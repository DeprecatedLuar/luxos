package templates

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const usersEvalExpr = `
let
  sys = import <nixpkgs/nixos/lib/eval-config.nix> {
    system = "x86_64-linux";
    modules = [
      ./framework/users.nix
      ./luar
      ./bob
      ./luar/modules/packages.nix
      {
        users.mutableUsers = false;
        users.users.root.hashedPassword = "!";
        fileSystems."/".device = "none";
        boot.loader.grub.device = "nodev";
        system.stateVersion = "25.11";
        luar.groups = %s;
      }
      (%s)
    ];
  };
  c = sys.config;
in {
  luarGroups = c.users.users.luar.extraGroups;
  bobGroups = c.users.users.bob.extraGroups;
  luarFile = c.users.users.luar.hashedPasswordFile;
  luarKeyFiles = map toString c.users.users.luar.openssh.authorizedKeys.keyFiles;
  bobKeyFiles = map toString c.users.users.bob.openssh.authorizedKeys.keyFiles;
  hasLuar = c.users.users ? luar;
  hasModulesUser = c.users.users ? modules;
  deps = c.system.activationScripts.users.deps;
  script = c.system.activationScripts.luxosPasswords.text;
  failed = map (a: a.message) (builtins.filter (a: !a.assertion) c.assertions);
}
`

const (
	groupsWheel   = `[ "wheel" "networkmanager" ]`
	groupsNone    = `[ ]`
	noWheelMarker = "no user is in wheel"
	strayMarker   = "not declared by a user unit: stray"
	mutableMarker = "users.mutableUsers must stay false"
)

type usersEval struct {
	LuarGroups     []string `json:"luarGroups"`
	BobGroups      []string `json:"bobGroups"`
	LuarFile       string   `json:"luarFile"`
	LuarKeyFiles   []string `json:"luarKeyFiles"`
	BobKeyFiles    []string `json:"bobKeyFiles"`
	HasLuar        bool     `json:"hasLuar"`
	HasModulesUser bool     `json:"hasModulesUser"`
	Deps           []string `json:"deps"`
	Script         string   `json:"script"`
	Failed         []string `json:"failed"`
}

// evalUsers evaluates the users framework with two vendored user units and
// luar.groups set to groups, a Nix list, plus extra, a Nix module.
func evalUsers(t *testing.T, groups, extra string) usersEval {
	t.Helper()
	if _, err := exec.LookPath("nix-instantiate"); err != nil {
		t.Skip("nix-instantiate not on PATH")
	}
	dir := t.TempDir()
	write := func(rel string, data []byte) {
		path := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	read := func(name string) []byte {
		data, err := File(name)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	write("framework/users.nix", read("framework/users.nix"))
	for _, u := range []string{"luar", "bob"} {
		write(u+"/default.nix", read("user/default.nix"))
		write(u+"/modules/packages.nix", read("starters/user/modules/packages.nix"))
		write(u+"/account.nix", read("user/account.nix"))
		write(u+"/options.nix", read("user/options.nix"))
		write(u+"/.password.age", []byte("x"))
	}
	write("luar/authorized_keys", []byte("ssh-ed25519 AAAA luar@test\n"))

	cmd := exec.Command("nix-instantiate", "--eval", "--strict", "--json", "-E", fmt.Sprintf(usersEvalExpr, groups, extra))
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("eval failed: %v\n%s", err, stderr.String())
	}
	var got usersEval
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("parse %q: %v", stdout.String(), err)
	}
	return got
}

func TestUsersFramework(t *testing.T) {
	got := evalUsers(t, groupsWheel, "{ }")
	if !reflect.DeepEqual(got.LuarGroups, []string{"wheel", "networkmanager"}) || len(got.BobGroups) != 0 {
		t.Errorf("groups: luar %v, bob %v", got.LuarGroups, got.BobGroups)
	}
	if !got.HasLuar || got.HasModulesUser {
		t.Errorf("packages submodule named the wrong user: hasLuar %v, hasModulesUser %v", got.HasLuar, got.HasModulesUser)
	}
	if len(got.LuarKeyFiles) != 1 || !strings.HasSuffix(got.LuarKeyFiles[0], "/luar/authorized_keys") || len(got.BobKeyFiles) != 0 {
		t.Errorf("authorized key files: luar %v, bob %v", got.LuarKeyFiles, got.BobKeyFiles)
	}
	if got.LuarFile != "/run/luxos/passwords/luar" {
		t.Errorf("hashedPasswordFile = %q", got.LuarFile)
	}
	found := false
	for _, d := range got.Deps {
		found = found || d == "luxosPasswords"
	}
	if !found {
		t.Errorf("users deps = %v, want luxosPasswords", got.Deps)
	}
	for _, want := range []string{"/var/lib/luxos/keys/luar", "/var/lib/luxos/keys/bob", "/bin/age -d", "-.password.age"} {
		if !strings.Contains(got.Script, want) {
			t.Errorf("activation script lacks %q:\n%s", want, got.Script)
		}
	}
	for _, u := range []string{"luar", "bob"} {
		want := "echo 'luxos: cannot decrypt the password of " + u + "; its account stays locked (luxos user key add " + u + ")' >&2"
		if !strings.Contains(got.Script, want) {
			t.Errorf("activation script lacks the quoted message %q:\n%s", want, got.Script)
		}
	}
	if len(got.Failed) != 0 {
		t.Errorf("failed assertions: %v", got.Failed)
	}
}

func TestUsersFramework_NoWheelUserFails(t *testing.T) {
	got := evalUsers(t, groupsNone, "{ }")
	if !failedWith(got, noWheelMarker) {
		t.Errorf("failed assertions %v lack %q", got.Failed, noWheelMarker)
	}
}

func failedWith(got usersEval, marker string) bool {
	for _, m := range got.Failed {
		if strings.Contains(m, marker) {
			return true
		}
	}
	return false
}

func TestUsersFramework_StrayUserFails(t *testing.T) {
	got := evalUsers(t, groupsWheel, `{ users.users.stray.isNormalUser = true; }`)
	if !failedWith(got, strayMarker) {
		t.Errorf("failed assertions %v lack %q", got.Failed, strayMarker)
	}
}

func TestUsersFramework_MutableUsersFails(t *testing.T) {
	got := evalUsers(t, groupsWheel, `{ lib, ... }: { users.mutableUsers = lib.mkForce true; }`)
	if !failedWith(got, mutableMarker) {
		t.Errorf("failed assertions %v lack %q", got.Failed, mutableMarker)
	}
}

func TestUsersFramework_SystemUserPasses(t *testing.T) {
	got := evalUsers(t, groupsWheel, `{ users.users.svc = { isSystemUser = true; group = "svc"; }; users.groups.svc = { }; }`)
	if len(got.Failed) != 0 {
		t.Errorf("failed assertions: %v", got.Failed)
	}
}

func TestSystemNix_MutableUsersOffByDefault(t *testing.T) {
	data, err := File("framework/system.nix")
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	if !strings.Contains(s, "users.mutableUsers = lib.mkDefault false;") {
		t.Error("system.nix does not default mutableUsers to false")
	}
	if strings.Contains(s, "lockout guard") {
		t.Error("system.nix still carries the luxos lockout guard")
	}
}
