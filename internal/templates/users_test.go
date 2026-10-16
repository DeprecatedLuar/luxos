package templates

import (
	"bytes"
	"encoding/json"
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
      {
        users.mutableUsers = false;
        users.users.root.hashedPassword = "!";
        fileSystems."/".device = "none";
        boot.loader.grub.device = "nodev";
        system.stateVersion = "25.11";
        luar.groups = [ "wheel" "networkmanager" ];
      }
    ];
  };
  c = sys.config;
in {
  luarGroups = c.users.users.luar.extraGroups;
  bobGroups = c.users.users.bob.extraGroups;
  luarFile = c.users.users.luar.hashedPasswordFile;
  deps = c.system.activationScripts.users.deps;
  script = c.system.activationScripts.luxosPasswords.text;
  failed = map (a: a.message) (builtins.filter (a: !a.assertion) c.assertions);
}
`

func TestUsersFramework(t *testing.T) {
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
		write(u+"/default.nix", read("starters/user/default.nix"))
		write(u+"/packages.nix", read("starters/user/packages.nix"))
		write(u+"/account.nix", read("user/account.nix"))
		write(u+"/options.nix", read("user/options.nix"))
		write(u+"/.password.age", []byte("x"))
	}

	cmd := exec.Command("nix-instantiate", "--eval", "--strict", "--json", "-E", usersEvalExpr)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("eval failed: %v\n%s", err, stderr.String())
	}
	var got struct {
		LuarGroups []string `json:"luarGroups"`
		BobGroups  []string `json:"bobGroups"`
		LuarFile   string   `json:"luarFile"`
		Deps       []string `json:"deps"`
		Script     string   `json:"script"`
		Failed     []string `json:"failed"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("parse %q: %v", stdout.String(), err)
	}
	if !reflect.DeepEqual(got.LuarGroups, []string{"wheel", "networkmanager"}) || len(got.BobGroups) != 0 {
		t.Errorf("groups: luar %v, bob %v", got.LuarGroups, got.BobGroups)
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
		want := "echo 'luxos: cannot decrypt the password of " + u + "; its account stays locked (luxos key add " + u + ")' >&2"
		if !strings.Contains(got.Script, want) {
			t.Errorf("activation script lacks the quoted message %q:\n%s", want, got.Script)
		}
	}
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
