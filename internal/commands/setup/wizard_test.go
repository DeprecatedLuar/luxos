package setup

import (
	"bytes"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/DeprecatedLuar/luxos/internal/commands/shared"
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
	root := t.TempDir()
	cfg := filepath.Join(root, "luxos")
	mounts := filepath.Join(root, "mounts")
	if err := os.WriteFile(mounts, nil, 0644); err != nil {
		t.Fatal(err)
	}
	return paths.Paths{
		User:    "ana",
		Config:  cfg,
		Hosts:   filepath.Join(cfg, ".local/hosts"),
		Modules: filepath.Join(cfg, "modules"),
		Mounts:  mounts,
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
		Latest:   func() (string, error) { return "nixos-26.05", nil },
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
	w, out := newWizard(t, p, "\n-bad\ncathode\n3\n\n3\nus\ny\n", noClone)

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
	if sel, err := os.ReadFile(filepath.Join(p.Modules, "users", "ana", "modules", "default.nix")); err != nil || !strings.Contains(string(sel), "./packages.nix") {
		t.Errorf("ana's selection = %q, %v", sel, err)
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
	w, out := newWizard(t, p, "n\ncathode\n4\n\ny\n", noClone)
	if _, err := w.Fresh(); err != nil {
		t.Fatalf("Fresh: %v\n%s", err, out)
	}
	if got := selection(t, p, "cathode"); got != "host/packages.nix unstable.nix users/ana" {
		t.Errorf("selection = %q", got)
	}
	if sel, err := os.ReadFile(filepath.Join(p.Modules, "users", "ana", "modules", "default.nix")); err != nil || !strings.Contains(string(sel), "./packages.nix") {
		t.Errorf("ana's selection = %q, %v", sel, err)
	}
}

func TestFresh_EndOfInputWritesNothing(t *testing.T) {
	p := testPaths(t)
	w, _ := newWizard(t, p, "\ncathode\n1\n\n", noClone) // input ends at the review screen
	if _, err := w.Fresh(); !errors.Is(err, io.EOF) {
		t.Fatalf("err = %v, want io.EOF", err)
	}
	if _, err := os.Stat(p.Config); !os.IsNotExist(err) {
		t.Errorf("config folder written before the final y: %v", err)
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
	if sel, err := os.ReadFile(filepath.Join(p.Modules, "users", "ana", "modules", "default.nix")); err != nil || !strings.Contains(string(sel), "./packages.nix") {
		t.Errorf("ana's selection = %q, %v", sel, err)
	}
	account, err := os.ReadFile(filepath.Join(p.Modules, "users/ana/account.nix"))
	if err != nil || !strings.Contains(string(account), "extraGroups = config.${name}.groups") {
		t.Errorf("created user unit must hold the vendored account.nix: %v\n%s", err, account)
	}
}

func TestHostScreen_ExistingUserUnitReused(t *testing.T) {
	skipIfNoNix(t)
	skipIfNoPasswordTools(t)
	p := testPaths(t)
	if err := cloneConfig(t, true)("", p.Config); err != nil {
		t.Fatal(err)
	}
	packagesPath := filepath.Join(p.Modules, "users/ana/packages.nix")
	if err := os.WriteFile(packagesPath, []byte("{ }\n"), 0644); err != nil {
		t.Fatal(err)
	}

	for i := range 2 {
		w, out := newWizard(t, p, "1\n", noClone)
		if i == 1 {
			w.Secret = answers("AGE-SECRET-KEY-1X")
		}
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
	skipIfNoPasswordTools(t)
	p := testPaths(t)
	if err := cloneConfig(t, true)("", p.Config); err != nil {
		t.Fatal(err)
	}
	// new, taken name, new name, write
	w, out := newWizard(t, p, "n\nnuremberg\ncathode\n\ny\n", noClone)
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
	w, out := newWizard(t, p, "n\ncathode\n4\n\n1\nEurope/\"Lisbon\ny\n", noClone)
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
	w, out := newWizard(t, p, "n\ncathode\n\n1\nEurope/\"Lisbon\ny\n", noClone)
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
	w, out := newWizard(t, p, "n\ncathode\n4\n\ny\n", noClone)
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

func TestFresh_PasswordMismatchWritesNoPassword(t *testing.T) {
	skipIfNoNix(t)
	p := testPaths(t)
	w, out := newWizard(t, p, "n\ncathode\n4\n\ny\n", noClone)
	w.Secret = answers("a", "b")
	w.StoreKey = func(string, string) error { t.Error("StoreKey called"); return nil }
	if _, err := w.Fresh(); !errors.Is(err, shared.ErrPasswordMismatch) {
		t.Fatalf("err = %v, want mismatch\n%s", err, out)
	}
	if _, err := os.Stat(p.Config); !os.IsNotExist(err) {
		t.Errorf("config written despite mismatch: %v", err)
	}
}

func TestFresh_EmptyPasswordWritesNothing(t *testing.T) {
	skipIfNoNix(t)
	p := testPaths(t)
	w, out := newWizard(t, p, "n\ncathode\n4\n\ny\n", noClone)
	w.Secret = answers("")
	w.StoreKey = func(string, string) error { t.Error("StoreKey called"); return nil }
	_, err := w.Fresh()
	if !errors.Is(err, password.ErrEmpty) || !strings.HasSuffix(err.Error(), "; nothing changed") {
		t.Fatalf("err = %v, want the empty-password error ending in '; nothing changed'\n%s", err, out)
	}
	if _, err := os.Stat(p.Config); !os.IsNotExist(err) {
		t.Errorf("config written despite an empty password: %v", err)
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
	if _, err := w.HostScreen(); !errors.Is(err, shared.ErrPasswordMismatch) {
		t.Fatalf("err = %v", err)
	}
	if _, err := os.Stat(filepath.Join(p.Modules, "users/ana")); !os.IsNotExist(err) {
		t.Errorf("user unit created: %v", err)
	}
}

func TestHostScreen_NonUserUnitNamedLikeUserFails(t *testing.T) {
	p := testPaths(t)
	if err := cloneConfig(t, false)("", p.Config); err != nil {
		t.Fatal(err)
	}
	clash := filepath.Join(p.Modules, "services", "ana.nix")
	if err := config.MkdirAll(filepath.Dir(clash)); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(clash, []byte("{ }\n"), 0644); err != nil {
		t.Fatal(err)
	}
	// new host, name, write
	w, out := newWizard(t, p, "n\ncathode\n\ny\n", noClone)
	_, err := w.HostScreen()
	if err == nil || !strings.Contains(err.Error(), "modules/services/ana.nix") {
		t.Fatalf("HostScreen err = %v, want one naming modules/services/ana.nix\n%s", err, out)
	}
	if _, err := os.Stat(filepath.Join(p.Modules, "users", "ana")); !os.IsNotExist(err) {
		t.Errorf("a user unit was written despite the clash: %v", err)
	}
}

func TestAskChannel_EnterPicksLatest(t *testing.T) {
	w, out := newWizard(t, testPaths(t), "\n", noClone)
	if got, err := w.askChannel(); err != nil || got != "nixos-26.05" {
		t.Fatalf("got %q, %v\n%s", got, err, out)
	}
}

func TestAskChannel_CurrentRow(t *testing.T) {
	for release, shown := range map[string]bool{"25.11": true, "26.05": false, "25.05": false} {
		w, out := newWizard(t, testPaths(t), "\n", noClone)
		w.System.Release = release
		if _, err := w.askChannel(); err != nil {
			t.Fatal(err)
		}
		if got := strings.Contains(out.String(), "(current)"); got != shown {
			t.Errorf("release %s: current row shown = %v, want %v\n%s", release, got, shown, out)
		}
	}
}

func TestAskChannel_FetchFails(t *testing.T) {
	const warning = "could not fetch the latest NixOS channel: offline"
	w, out := newWizard(t, testPaths(t), "\n\n", noClone)
	w.Latest = func() (string, error) { return "", errors.New("offline") }
	w.System.Release = "26.05"
	for range 2 {
		if got, err := w.askChannel(); err != nil || got != "nixos-26.05" {
			t.Fatalf("got %q, %v\n%s", got, err, out)
		}
	}
	if n := strings.Count(out.String(), warning); n != 1 {
		t.Errorf("warning shown %d times, want once\n%s", n, out)
	}

	w, out = newWizard(t, testPaths(t), "\n1\n", noClone)
	w.Latest = func() (string, error) { return "", errors.New("offline") }
	w.System.Release = "25.05"
	if got, err := w.askChannel(); err != nil || got != "nixos-unstable" {
		t.Errorf("old release, offline: got %q, %v\n%s", got, err, out)
	}
}

func TestAskChannel_Custom(t *testing.T) {
	w, out := newWizard(t, testPaths(t), "4\n25.05\n26.11\n", noClone)
	if got, err := w.askChannel(); err != nil || got != "nixos-26.11" {
		t.Fatalf("got %q, %v\n%s", got, err, out)
	}
	if !strings.Contains(out.String(), "a channel is YY.MM or nixos-YY.MM, nixos-25.11 or newer") {
		t.Errorf("floor message missing:\n%s", out)
	}
}

func TestFresh_ChannelWritten(t *testing.T) {
	skipIfNoPasswordTools(t)
	p := testPaths(t)
	// no repo, name, console, latest channel, write
	w, out := newWizard(t, p, "\ncathode\n4\n\ny\n", noClone)
	if _, err := w.Fresh(); err != nil {
		t.Fatalf("Fresh: %v\n%s", err, out)
	}
	host, err := os.ReadFile(filepath.Join(p.Hosts, "cathode/host.nix"))
	if err != nil || !strings.Contains(string(host), "github:NixOS/nixpkgs/nixos-26.05") {
		t.Errorf("host.nix: %v\n%s", err, host)
	}
}

func TestReview_ChannelRowOpensMenu(t *testing.T) {
	skipIfNoPasswordTools(t)
	p := testPaths(t)
	// no repo, name, console, latest, review row 4, unstable, write
	w, out := newWizard(t, p, "\ncathode\n4\n\n4\n3\ny\n", noClone)
	if _, err := w.Fresh(); err != nil {
		t.Fatalf("Fresh: %v\n%s", err, out)
	}
	host, err := os.ReadFile(filepath.Join(p.Hosts, "cathode/host.nix"))
	if err != nil || !strings.Contains(string(host), "github:NixOS/nixpkgs/nixos-unstable") {
		t.Errorf("host.nix: %v\n%s", err, host)
	}
}

func TestIsRemoteURL(t *testing.T) {
	cases := map[string]bool{
		"https://github.com/ana/config.git":    true,
		"ssh://git@example.com/ana/config.git": true,
		"git@github.com:ana/config.git":        true,
		"ana/config":                           false,
		"http://github.com/ana/config":         false,
		"file:///tmp/config":                   false,
		"/tmp/config":                          false,
		"":                                     false,
	}
	for s, want := range cases {
		if got := isRemoteURL(s); got != want {
			t.Errorf("isRemoteURL(%q) = %v, want %v", s, got, want)
		}
	}
}

func cloneLeftovers(t *testing.T, p paths.Paths) []string {
	t.Helper()
	m, err := filepath.Glob(filepath.Join(filepath.Dir(p.Config), ".luxos-clone-*"))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestFresh_ShorthandURLReasked(t *testing.T) {
	p := testPaths(t)
	w, out := newWizard(t, p, "y\ndeprecatedluar/luxos-dots\n", noClone)
	if _, err := w.Fresh(); !errors.Is(err, io.EOF) {
		t.Fatalf("err = %v, want io.EOF", err)
	}
	if !strings.Contains(out.String(), "use a full URL: https://…, ssh://… or git@host:owner/repo") {
		t.Errorf("full-URL warning missing:\n%s", out)
	}
}

func TestFresh_CloneFailureReasks(t *testing.T) {
	p := testPaths(t)
	failing := func(string, string) error { return errors.New("exit status 128") }
	w, out := newWizard(t, p, "y\nhttps://x/y.git\n", failing)
	if _, err := w.Fresh(); !errors.Is(err, io.EOF) {
		t.Fatalf("err = %v, want io.EOF", err)
	}
	if !strings.Contains(out.String(), "use an https URL, or add an SSH key") {
		t.Errorf("clone hint missing:\n%s", out)
	}
	if left := cloneLeftovers(t, p); len(left) != 0 {
		t.Errorf("temporary clone left: %v", left)
	}
	if _, err := os.Stat(p.Config); !os.IsNotExist(err) {
		t.Errorf("config created after a failed clone: %v", err)
	}
}

func TestFresh_NotLuxosConfigReasks(t *testing.T) {
	p := testPaths(t)
	readmeOnly := func(_, dir string) error { return os.WriteFile(filepath.Join(dir, "README.md"), []byte("hi\n"), 0644) }
	w, out := newWizard(t, p, "y\nhttps://x/y.git\n", readmeOnly)
	if _, err := w.Fresh(); !errors.Is(err, io.EOF) {
		t.Fatalf("err = %v, want io.EOF", err)
	}
	for _, s := range []string{"https://x/y.git is not a luxos config: it has no host", "or pass --config <dir>"} {
		if !strings.Contains(out.String(), s) {
			t.Errorf("output missing %q:\n%s", s, out)
		}
	}
	if left := cloneLeftovers(t, p); len(left) != 0 {
		t.Errorf("temporary clone left: %v", left)
	}
	if _, err := os.Stat(p.Config); !os.IsNotExist(err) {
		t.Errorf("config created from a non-luxos repo: %v", err)
	}
}

func TestFresh_CloneMovedIntoPlace(t *testing.T) {
	skipIfNoNix(t)
	skipIfNoPasswordTools(t)
	for _, preexisting := range []bool{false, true} {
		p := testPaths(t)
		if preexisting {
			if err := os.MkdirAll(p.Config, 0755); err != nil {
				t.Fatal(err)
			}
		}
		w, out := newWizard(t, p, "y\nhttps://x/y.git\n1\n", cloneConfig(t, false))
		if name, err := w.Fresh(); err != nil || name != "nuremberg" {
			t.Fatalf("empty dir %v: Fresh = %q, %v\n%s", preexisting, name, err, out)
		}
		if _, err := os.Stat(filepath.Join(p.Hosts, "nuremberg")); err != nil {
			t.Errorf("clone not moved into place: %v", err)
		}
		if left := cloneLeftovers(t, p); len(left) != 0 {
			t.Errorf("temporary clone left: %v", left)
		}
	}
}

func TestHostScreen_PickByName(t *testing.T) {
	skipIfNoNix(t)
	skipIfNoPasswordTools(t)
	p := testPaths(t)
	if err := cloneConfig(t, false)("", p.Config); err != nil {
		t.Fatal(err)
	}
	w, out := newWizard(t, p, "nuremberg\n", noClone)
	if name, err := w.HostScreen(); err != nil || name != "nuremberg" {
		t.Fatalf("HostScreen = %q, %v\n%s", name, err, out)
	}
}

func TestHostScreen_InvalidWarns(t *testing.T) {
	skipIfNoNix(t)
	skipIfNoPasswordTools(t)
	p := testPaths(t)
	if err := cloneConfig(t, false)("", p.Config); err != nil {
		t.Fatal(err)
	}
	w, out := newWizard(t, p, "9\nfoo\n1\n", noClone)
	if name, err := w.HostScreen(); err != nil || name != "nuremberg" {
		t.Fatalf("HostScreen = %q, %v\n%s", name, err, out)
	}
	if n := strings.Count(out.String(), "pick a number or a host name from the list"); n != 2 {
		t.Errorf("warnings = %d, want 2\n%s", n, out)
	}
}

// existingUser clones a config holding ana's unit, with a password file when withPassword.
func existingUser(t *testing.T, withPassword bool) (paths.Paths, string) {
	t.Helper()
	p := testPaths(t)
	if err := cloneConfig(t, true)("", p.Config); err != nil {
		t.Fatal(err)
	}
	pwFile := filepath.Join(p.Modules, "users/ana", password.File)
	if withPassword {
		if err := os.WriteFile(pwFile, []byte("enc\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	return p, pwFile
}

func TestHostScreen_ExistingUserKeyStored(t *testing.T) {
	skipIfNoNix(t)
	p, pwFile := existingUser(t, true)
	w, out := newWizard(t, p, "1\n", noClone)
	w.Secret = answers("AGE-SECRET-KEY-1X")
	var stored []string
	w.StoreKey = func(user, secret string) error { stored = append(stored, user+"="+secret); return nil }
	if _, err := w.HostScreen(); err != nil {
		t.Fatalf("HostScreen: %v\n%s", err, out)
	}
	if !slices.Equal(stored, []string{"ana=AGE-SECRET-KEY-1X"}) {
		t.Errorf("stored = %q", stored)
	}
	if got, _ := os.ReadFile(pwFile); string(got) != "enc\n" {
		t.Errorf("password file rewritten: %q", got)
	}
	if !strings.Contains(out.String(), "Found user ana in your config.") {
		t.Errorf("found message missing:\n%s", out)
	}
	groupsPath := filepath.Join(p.Hosts, "nuremberg/settings/ana.nix")
	if got, _ := os.ReadFile(groupsPath); !bytes.Equal(got, config.UserGroups("ana")) {
		t.Errorf("groups file not created: %q", got)
	}
	if err := os.WriteFile(groupsPath, []byte("{ }\n"), 0644); err != nil {
		t.Fatal(err)
	}
	w, _ = newWizard(t, p, "1\n", noClone)
	w.Secret = answers("AGE-SECRET-KEY-1X")
	if _, err := w.HostScreen(); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(groupsPath); string(got) != "{ }\n" {
		t.Errorf("existing groups file overwritten: %q", got)
	}
}

func TestHostScreen_ExistingUserBadKeyReasked(t *testing.T) {
	skipIfNoNix(t)
	p, _ := existingUser(t, true)
	w, out := newWizard(t, p, "1\n", noClone)
	w.Secret = answers("AGE-SECRET-KEY-1A", "AGE-SECRET-KEY-1B")
	var stored []string
	w.StoreKey = func(user, secret string) error {
		if len(stored) == 0 {
			stored = append(stored, "")
			return errors.New("the key on this computer does not decrypt")
		}
		stored = append(stored, secret)
		return nil
	}
	if _, err := w.HostScreen(); err != nil {
		t.Fatalf("HostScreen: %v\n%s", err, out)
	}
	if stored[len(stored)-1] != "AGE-SECRET-KEY-1B" || !strings.Contains(out.String(), "does not decrypt") {
		t.Errorf("stored = %q\n%s", stored, out)
	}
}

func TestHostScreen_ExistingUserEmptyKeyNewPassword(t *testing.T) {
	skipIfNoNix(t)
	skipIfNoPasswordTools(t)
	p, pwFile := existingUser(t, true)
	w, out := newWizard(t, p, "1\n", noClone)
	w.Secret = answers("", "pw", "pw")
	var stored []string
	w.StoreKey = func(_, secret string) error { stored = append(stored, secret); return nil }
	if _, err := w.HostScreen(); err != nil {
		t.Fatalf("HostScreen: %v\n%s", err, out)
	}
	if !strings.Contains(out.String(), "every other computer needs `luxos key add ana` with the new key after pulling") {
		t.Errorf("re-key warning missing:\n%s", out)
	}
	if got, _ := os.ReadFile(pwFile); !strings.HasPrefix(string(got), "-----BEGIN AGE ENCRYPTED FILE-----") {
		t.Errorf("password file not re-encrypted: %q", got)
	}
	if len(stored) != 1 || !strings.HasPrefix(stored[0], "AGE-SECRET-KEY-1") {
		t.Errorf("stored = %q", stored)
	}
}

func TestHostScreen_ExistingUserNoPasswordFile(t *testing.T) {
	skipIfNoNix(t)
	skipIfNoPasswordTools(t)
	p, pwFile := existingUser(t, false)
	w, out := newWizard(t, p, "1\n", noClone)
	var prompts []string
	w.Secret = func(prompt string) (string, error) { prompts = append(prompts, prompt); return "pw", nil }
	if _, err := w.HostScreen(); err != nil {
		t.Fatalf("HostScreen: %v\n%s", err, out)
	}
	for _, pr := range prompts {
		if strings.HasPrefix(pr, "Key for") {
			t.Errorf("key asked for a user without a password file: %q", pr)
		}
	}
	if _, err := os.Stat(pwFile); err != nil {
		t.Errorf("password file not created: %v", err)
	}
}
