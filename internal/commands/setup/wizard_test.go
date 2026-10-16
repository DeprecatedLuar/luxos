package setup

import (
	"bytes"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DeprecatedLuar/luxos/internal/config"
	"github.com/DeprecatedLuar/luxos/internal/modules"
	"github.com/DeprecatedLuar/luxos/internal/password"
	"github.com/DeprecatedLuar/luxos/internal/paths"
	"github.com/DeprecatedLuar/luxos/internal/ui"
)

func skipIfNoNix(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("nix-instantiate"); err != nil {
		t.Skip("nix-instantiate not on PATH")
	}
}

func skipIfNoPasswordTools(t *testing.T) {
	t.Helper()
	for _, tool := range []string{"age", "age-keygen", "mkpasswd"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s not on PATH", tool)
		}
	}
}

// answers returns a Secret that replies with each value in turn.
func answers(values ...string) func(string) (string, error) {
	return func(string) (string, error) {
		if len(values) == 0 {
			return "", io.EOF
		}
		v := values[0]
		values = values[1:]
		return v, nil
	}
}

var testSystem = System{
	Values:  config.HostValues{Channel: "nixos-25.11", Timezone: "Europe/Lisbon", Locale: "pt_PT.UTF-8", Keyboard: "pt"},
	Release: "25.11",
}

func testPaths(t *testing.T) paths.Paths {
	cfg := filepath.Join(t.TempDir(), "luxos")
	return paths.Paths{
		User:    "ana",
		Config:  cfg,
		Hosts:   filepath.Join(cfg, ".local/hosts"),
		Modules: filepath.Join(cfg, "modules"),
	}
}

func newWizard(t *testing.T, p paths.Paths, input string, clone func(url, dir string) error) (*Wizard, *bytes.Buffer) {
	var out bytes.Buffer
	return &Wizard{
		Ask:      ui.NewPrompter(strings.NewReader(input), &out),
		Out:      &out,
		Paths:    p,
		System:   testSystem,
		Clone:    clone,
		Secret:   answers("pw", "pw", "pw", "pw"),
		StoreKey: func(string, string) error { return nil },
	}, &out
}

func noClone(string, string) error { return errors.New("clone must not run") }

func selection(t *testing.T, p paths.Paths, host string) string {
	t.Helper()
	sel, err := modules.ReadSelection(config.SelectionPath(filepath.Join(p.Hosts, host)), config.ScopeHost)
	if err != nil {
		t.Fatal(err)
	}
	return strings.Join(sel, " ")
}

func TestFresh_NewConfig(t *testing.T) {
	skipIfNoPasswordTools(t)
	p := testPaths(t)
	// no repo, bad name, good name, hyprland, change keyboard, keep, write
	w, out := newWizard(t, p, "\n-bad\ncathode\n3\n3\nus\ny\n", noClone)

	name, err := w.Fresh()
	if err != nil {
		t.Fatalf("Fresh: %v\n%s", err, out)
	}
	if name != "cathode" {
		t.Errorf("name = %q", name)
	}
	want := "host/packages.nix unstable.nix users/ana desktop/compositors/hyprland.nix desktop/greeters/greetd.nix"
	if got := selection(t, p, "cathode"); got != want {
		t.Errorf("selection = %q\nwant %q", got, want)
	}
	host, err := os.ReadFile(filepath.Join(p.Hosts, "cathode/host.nix"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(host), `services.xserver.xkb.layout = "us";`) {
		t.Errorf("edited keyboard not written:\n%s", host)
	}
	if _, err := os.Stat(filepath.Join(p.Modules, "users/ana/account.nix")); err != nil {
		t.Errorf("user unit: %v", err)
	}
	if _, err := os.Stat(filepath.Join(p.Modules, "desktop/compositors/hyprland.nix")); err != nil {
		t.Errorf("default modules: %v", err)
	}
	for _, s := range []string{"letters, digits and -", "timezone   Europe/Lisbon", "desktop    hyprland", p.Config} {
		if !strings.Contains(out.String(), s) {
			t.Errorf("output missing %q:\n%s", s, out)
		}
	}
}

func TestFresh_ConsoleSelectsNoDesktop(t *testing.T) {
	skipIfNoPasswordTools(t)
	p := testPaths(t)
	w, out := newWizard(t, p, "n\ncathode\n4\ny\n", noClone)
	if _, err := w.Fresh(); err != nil {
		t.Fatalf("Fresh: %v\n%s", err, out)
	}
	if got := selection(t, p, "cathode"); got != "host/packages.nix unstable.nix users/ana" {
		t.Errorf("selection = %q", got)
	}
}

func TestFresh_EndOfInputWritesNothing(t *testing.T) {
	p := testPaths(t)
	w, _ := newWizard(t, p, "\ncathode\n1\n", noClone) // input ends at the review screen
	if _, err := w.Fresh(); !errors.Is(err, io.EOF) {
		t.Fatalf("err = %v, want io.EOF", err)
	}
	if _, err := os.Stat(p.Config); !os.IsNotExist(err) {
		t.Errorf("config folder written before the final y: %v", err)
	}
}

func TestFresh_CloneFailureNamesFix(t *testing.T) {
	p := testPaths(t)
	failing := func(string, string) error { return errors.New("exit status 128") }
	w, _ := newWizard(t, p, "y\ngit@example.com:ana/config.git\n", failing)
	_, err := w.Fresh()
	if err == nil || !strings.Contains(err.Error(), "use an https URL, or add an SSH key and run luxos setup again") {
		t.Fatalf("err = %v", err)
	}
}

// cloneConfig fakes a clone holding one host and, when withUser, ana's unit.
func cloneConfig(t *testing.T, withUser bool) func(url, dir string) error {
	return func(url, dir string) error {
		hosts := filepath.Join(dir, ".local/hosts")
		m := config.HostSpec{Name: "nuremberg", StateVersion: "25.05", Values: config.HostDefaults, Selection: []string{"host/packages.nix"}}
		if err := config.CreateHost(hosts, m); err != nil {
			return err
		}
		if !withUser {
			return config.MkdirAll(filepath.Join(dir, "modules"))
		}
		return config.WriteUser(filepath.Join(dir, "modules/users/ana"))
	}
}

func TestHostScreen_ExistingHostGetsUser(t *testing.T) {
	skipIfNoNix(t)
	skipIfNoPasswordTools(t)
	p := testPaths(t)
	w, out := newWizard(t, p, "y\nhttps://example.com/c.git\n1\n", cloneConfig(t, false))
	name, err := w.Fresh()
	if err != nil {
		t.Fatalf("Fresh: %v\n%s", err, out)
	}
	if name != "nuremberg" {
		t.Errorf("name = %q", name)
	}
	if got := selection(t, p, "nuremberg"); got != "host/packages.nix users/ana" {
		t.Errorf("selection = %q", got)
	}
	account, err := os.ReadFile(filepath.Join(p.Modules, "users/ana/account.nix"))
	if err != nil || !strings.Contains(string(account), "extraGroups = config.${name}.groups") {
		t.Errorf("created user unit must hold the vendored account.nix: %v\n%s", err, account)
	}
}

func TestHostScreen_ExistingUserUnitReused(t *testing.T) {
	skipIfNoNix(t)
	p := testPaths(t)
	if err := cloneConfig(t, true)("", p.Config); err != nil {
		t.Fatal(err)
	}
	packagesPath := filepath.Join(p.Modules, "users/ana/packages.nix")
	if err := os.WriteFile(packagesPath, []byte("{ }\n"), 0644); err != nil {
		t.Fatal(err)
	}

	for range 2 {
		w, out := newWizard(t, p, "1\n", noClone)
		if _, err := w.HostScreen(); err != nil {
			t.Fatalf("HostScreen: %v\n%s", err, out)
		}
	}
	if got := selection(t, p, "nuremberg"); got != "host/packages.nix users/ana" {
		t.Errorf("selection = %q", got)
	}
	if got, _ := os.ReadFile(packagesPath); string(got) != "{ }\n" {
		t.Errorf("existing packages.nix rewritten: %q", got)
	}
}

func TestHostScreen_NewHost(t *testing.T) {
	p := testPaths(t)
	if err := cloneConfig(t, true)("", p.Config); err != nil {
		t.Fatal(err)
	}
	// new, taken name, new name, write
	w, out := newWizard(t, p, "n\nnuremberg\ncathode\ny\n", noClone)
	name, err := w.HostScreen()
	if err != nil {
		t.Fatalf("HostScreen: %v\n%s", err, out)
	}
	if name != "cathode" {
		t.Errorf("name = %q", name)
	}
	if !strings.Contains(out.String(), "nuremberg already exists, pick it from the list or choose another name") {
		t.Errorf("taken-name message missing:\n%s", out)
	}
	if strings.Contains(out.String(), "desktop") {
		t.Errorf("host screen must not ask for a desktop:\n%s", out)
	}
	if got := selection(t, p, "cathode"); got != "host/packages.nix users/ana" {
		t.Errorf("selection = %q", got)
	}
}

func TestFresh_UnparsableValueWritesNothing(t *testing.T) {
	skipIfNoNix(t)
	p := testPaths(t)
	// no repo, name, console, change timezone to a bad value, write
	w, out := newWizard(t, p, "n\ncathode\n4\n1\nEurope/\"Lisbon\ny\n", noClone)
	if _, err := w.Fresh(); err == nil || !strings.Contains(err.Error(), "host.nix") {
		t.Fatalf("err = %v, want a parse error naming host.nix\n%s", err, out)
	}
	if _, err := os.Stat(p.Config); !os.IsNotExist(err) {
		t.Errorf("config folder written although the host was refused: %v", err)
	}
}

func TestHostScreen_UnparsableValueWritesNothing(t *testing.T) {
	skipIfNoNix(t)
	p := testPaths(t)
	if err := cloneConfig(t, false)("", p.Config); err != nil {
		t.Fatal(err)
	}
	w, out := newWizard(t, p, "n\ncathode\n1\nEurope/\"Lisbon\ny\n", noClone)
	if _, err := w.HostScreen(); err == nil {
		t.Fatalf("want a parse error\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(p.Modules, "users/ana")); !os.IsNotExist(err) {
		t.Errorf("user unit written although the host was refused: %v", err)
	}
}

func TestFresh_UserGetsPasswordKeyAndGroups(t *testing.T) {
	skipIfNoPasswordTools(t)
	p := testPaths(t)
	w, out := newWizard(t, p, "n\ncathode\n4\ny\n", noClone)
	stored := map[string]string{}
	w.StoreKey = func(user, secret string) error { stored[user] = secret; return nil }
	if _, err := w.Fresh(); err != nil {
		t.Fatalf("Fresh: %v\n%s", err, out)
	}
	enc, err := os.ReadFile(filepath.Join(p.Modules, "users/ana", password.File))
	if err != nil || !strings.HasPrefix(string(enc), "-----BEGIN AGE ENCRYPTED FILE-----") {
		t.Errorf("password file: %v\n%s", err, enc)
	}
	secret := stored["ana"]
	if !strings.HasPrefix(secret, "AGE-SECRET-KEY-1") {
		t.Fatalf("stored key = %q", secret)
	}
	if n := strings.Count(out.String(), secret); n != 1 {
		t.Errorf("key shown %d times, want once", n)
	}
	groups, err := os.ReadFile(filepath.Join(p.Hosts, "cathode/settings/ana.nix"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(groups, config.UserGroups("ana")) || !strings.Contains(string(groups), "wheel") {
		t.Errorf("groups file:\n%s", groups)
	}
}

func TestHostScreen_ExistingUserUnitKeepsPassword(t *testing.T) {
	skipIfNoNix(t)
	p := testPaths(t)
	if err := cloneConfig(t, true)("", p.Config); err != nil {
		t.Fatal(err)
	}
	pwFile := filepath.Join(p.Modules, "users/ana", password.File)
	if err := os.WriteFile(pwFile, []byte("enc\n"), 0644); err != nil {
		t.Fatal(err)
	}
	groupsPath := filepath.Join(p.Hosts, "nuremberg/settings/ana.nix")
	w, out := newWizard(t, p, "1\n", noClone)
	w.StoreKey = func(string, string) error { t.Error("StoreKey called"); return nil }
	w.Secret = func(string) (string, error) { t.Error("Secret asked"); return "", io.EOF }
	if _, err := w.HostScreen(); err != nil {
		t.Fatalf("HostScreen: %v\n%s", err, out)
	}
	if got, _ := os.ReadFile(pwFile); string(got) != "enc\n" {
		t.Errorf("password file rewritten: %q", got)
	}
	if got, _ := os.ReadFile(groupsPath); !bytes.Equal(got, config.UserGroups("ana")) {
		t.Errorf("groups file not created: %q", got)
	}
	if err := os.WriteFile(groupsPath, []byte("{ }\n"), 0644); err != nil {
		t.Fatal(err)
	}
	w, _ = newWizard(t, p, "1\n", noClone)
	if _, err := w.HostScreen(); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(groupsPath); string(got) != "{ }\n" {
		t.Errorf("existing groups file overwritten: %q", got)
	}
}

func TestFresh_PasswordMismatchWritesNoPassword(t *testing.T) {
	skipIfNoNix(t)
	p := testPaths(t)
	w, out := newWizard(t, p, "n\ncathode\n4\ny\n", noClone)
	w.Secret = answers("a", "b")
	w.StoreKey = func(string, string) error { t.Error("StoreKey called"); return nil }
	if _, err := w.Fresh(); !errors.Is(err, errSetupMismatch) {
		t.Fatalf("err = %v, want mismatch\n%s", err, out)
	}
	if _, err := os.Stat(p.Config); !os.IsNotExist(err) {
		t.Errorf("config written despite mismatch: %v", err)
	}
}

func TestHostScreen_MismatchLeavesNoUnit(t *testing.T) {
	skipIfNoNix(t)
	p := testPaths(t)
	if err := cloneConfig(t, false)("", p.Config); err != nil {
		t.Fatal(err)
	}
	w, _ := newWizard(t, p, "1\n", noClone)
	w.Secret = answers("a", "b")
	if _, err := w.HostScreen(); !errors.Is(err, errSetupMismatch) {
		t.Fatalf("err = %v", err)
	}
	if _, err := os.Stat(filepath.Join(p.Modules, "users/ana")); !os.IsNotExist(err) {
		t.Errorf("user unit created: %v", err)
	}
}
