package config

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DeprecatedLuar/luxos/internal/hardware"
	"github.com/DeprecatedLuar/luxos/internal/paths"
	"github.com/DeprecatedLuar/luxos/internal/ui"
)

const fixtureUUID = "4c4c4544-0042-5110-8042-cac04f375433"

func skipIfNoNix(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("nix-instantiate"); err != nil {
		t.Skip("nix-instantiate not on PATH")
	}
}

// machineFixture builds a fake sysfs and a machines root whose folder already
// holds a hardware-configuration.nix and a modules/boot.nix.
func machineFixture(t *testing.T) paths.Paths {
	t.Helper()
	root := t.TempDir()
	sysDir := filepath.Join(root, "sys")
	writeFile(t, filepath.Join(sysDir, "class", "dmi", "id", "product_uuid"), fixtureUUID+"\n")
	dir, err := MachineDir(sysDir, filepath.Join(root, "machines"))
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, HardwareConfigFile), "{ }\n")
	writeFile(t, filepath.Join(dir, ScopeModulesDir, BootFile), "{ ... }: { }\n")
	mounts := filepath.Join(root, "mounts")
	writeFile(t, mounts, "")
	return paths.Paths{Sys: sysDir, Machines: filepath.Join(root, "machines"), Mounts: mounts}
}

func TestMachineDirUsesKey(t *testing.T) {
	p := machineFixture(t)
	key, err := hardware.Key(p.Sys)
	if err != nil {
		t.Fatal(err)
	}
	got, err := MachineDir(p.Sys, p.Machines)
	if err != nil || got != filepath.Join(p.Machines, key) {
		t.Errorf("MachineDir = %q, %v", got, err)
	}
}

func TestCopyLockBack(t *testing.T) {
	staged := filepath.Join(t.TempDir(), "flake.lock")
	hostLock := filepath.Join(t.TempDir(), "flake.lock")
	writeFile(t, staged, "a")

	if changed, err := CopyLockBack(staged, hostLock); err != nil || !changed {
		t.Fatalf("missing host lock: changed=%v err=%v, want true", changed, err)
	}
	if got := readFile(t, hostLock); got != "a" {
		t.Errorf("host lock = %q, want %q", got, "a")
	}
	info, err := os.Stat(hostLock)
	if err != nil || info.Mode().Perm() != fileMode {
		t.Errorf("host lock mode = %v, err=%v, want %v", info.Mode().Perm(), err, fileMode)
	}

	if changed, err := CopyLockBack(staged, hostLock); err != nil || changed {
		t.Errorf("same content: changed=%v err=%v, want false", changed, err)
	}

	writeFile(t, staged, "b")
	if changed, err := CopyLockBack(staged, hostLock); err != nil || !changed {
		t.Errorf("different content: changed=%v err=%v, want true", changed, err)
	}
	if got := readFile(t, hostLock); got != "b" {
		t.Errorf("host lock = %q, want %q", got, "b")
	}
}

func TestCopyLockBack_MissingStagedLockErrors(t *testing.T) {
	if _, err := CopyLockBack(filepath.Join(t.TempDir(), "flake.lock"), filepath.Join(t.TempDir(), "flake.lock")); err == nil {
		t.Fatal("want error when the staged lock is missing")
	}
}

func TestActiveHost(t *testing.T) {
	root := t.TempDir()
	modules := filepath.Join(root, "modules")
	hostModules := filepath.Join(root, ".local", "hosts", "box", "modules")
	for _, d := range []string{modules, hostModules} {
		if err := os.MkdirAll(d, 0755); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := ActiveHost(modules); err == nil {
		t.Fatal("want error with no modules/host link")
	}
	if err := os.Symlink(hostModules, filepath.Join(modules, HostLinkName)); err != nil {
		t.Fatal(err)
	}
	if got, err := ActiveHost(modules); err != nil || got != "box" {
		t.Errorf("ActiveHost = %q, %v; want box", got, err)
	}
}

func TestEnsureHostFile_CreatesFromTemplate(t *testing.T) {
	hostDir := t.TempDir()
	var out bytes.Buffer
	if err := ensureHostFile(ui.NewProgress(&out), hostDir); err != nil {
		t.Fatal(err)
	}
	want, err := RenderHost(HostDefaults)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(hostDir, "host.nix")
	if got := readFile(t, path); got != string(want) {
		t.Errorf("host.nix differs from template:\n%s", got)
	}
	if !strings.Contains(out.String(), path) {
		t.Errorf("output %q does not name %s", out.String(), path)
	}
}

func TestEnsureHostFile_ExistingUntouched(t *testing.T) {
	hostDir := t.TempDir()
	path := filepath.Join(hostDir, "host.nix")
	const own = "{ ... }: { }\n"
	writeFile(t, path, own)
	var out bytes.Buffer
	if err := ensureHostFile(ui.NewProgress(&out), hostDir); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, path); got != own {
		t.Errorf("host.nix changed: %q", got)
	}
	if out.Len() != 0 {
		t.Errorf("unexpected output %q", out.String())
	}
}

func TestHostTemplate_Parses(t *testing.T) {
	skipIfNoNix(t)
	tmpl, err := RenderHost(HostDefaults)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "host.nix")
	writeFile(t, path, string(tmpl))
	if out, err := exec.Command("nix-instantiate", "--parse", path).CombinedOutput(); err != nil {
		t.Fatalf("template does not parse: %v\n%s", err, out)
	}
}

func TestHostTemplate_NoGC(t *testing.T) {
	tmpl, err := RenderHost(HostDefaults)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(tmpl), "nix.gc") {
		t.Error("host template must not define nix.gc")
	}
}

func TestCheckHostFile(t *testing.T) {
	skipIfNoNix(t)
	tmpl, err := RenderHost(HostDefaults)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name    string
		content string
		wantErr bool
	}{
		{"assignments only", "{ time.timeZone = \"UTC\"; }\n", false},
		{"embedded template", string(tmpl), false},
		{"static import", "{ imports = [ ./extra.nix ]; }\n", true},
		{"dynamic path", "{ imports = [ (./. + \"/x.nix\") ]; }\n", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "host.nix")
			writeFile(t, path, c.content)
			err := checkHostFile(path)
			if (err != nil) != c.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, c.wantErr)
			}
			if err != nil && !strings.Contains(err.Error(), path) {
				t.Errorf("error %q does not name the file", err)
			}
		})
	}
}

func TestEnsureMachine_CreatesSelectionOnce(t *testing.T) {
	p := machineFixture(t)
	var out bytes.Buffer
	dir, err := EnsureMachine(ui.NewProgress(&out), p)
	if err != nil {
		t.Fatal(err)
	}
	sel := SelectionPath(dir)
	if got := readFile(t, sel); !strings.Contains(got, "./boot.nix") {
		t.Errorf("seed selection does not select ./boot.nix:\n%s", got)
	}
	if !strings.Contains(out.String(), "created: "+sel) {
		t.Errorf("output does not report creating %s:\n%s", sel, out.String())
	}

	writeFile(t, sel, "{ ... }:\n{\n  imports = [ ];\n}\n")
	out.Reset()
	if _, err := EnsureMachine(ui.NewProgress(&out), p); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, sel); !strings.Contains(got, "imports = [ ];") {
		t.Errorf("edited selection was rewritten: %q", got)
	}
	if strings.Contains(out.String(), "created: "+sel) {
		t.Errorf("second run reported creating the selection:\n%s", out.String())
	}
}

func TestEnsureCreatesScopeLinksAndEntry(t *testing.T) {
	skipIfNoNix(t)
	p := machineFixture(t)
	p.Config = t.TempDir()
	p.Machines = filepath.Join(p.Config, ".local", "machines")
	machine, err := MachineDir(p.Sys, p.Machines)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(machine, HardwareConfigFile), "{ }\n")
	writeFile(t, filepath.Join(machine, ScopeModulesDir, BootFile), "{ ... }: { }\n")
	p.Hosts = filepath.Join(p.Config, ".local", "hosts")
	p.Modules = filepath.Join(p.Config, "modules")
	hostDir := filepath.Join(p.Hosts, "h1")
	writeFile(t, filepath.Join(hostDir, ".plsdonttouch.nix"), "{ system.stateVersion = \"25.11\"; }\n")
	writeFile(t, SelectionPath(hostDir), "{ ... }:\n{\n  imports = [ ];\n}\n")

	var out bytes.Buffer
	machineDir, err := Ensure(ui.NewProgress(&out), p, "h1")
	if err != nil {
		t.Fatalf("Ensure: %v\n%s", err, out.String())
	}
	if filepath.Base(machineDir) != filepath.Base(machine) {
		t.Errorf("Ensure machine folder = %q, want the folder %q", machineDir, filepath.Base(machine))
	}

	for link, target := range map[string]string{
		filepath.Join(p.Modules, "host"):    "../.local/hosts/h1/modules",
		filepath.Join(p.Modules, "machine"): "../.local/machines/" + filepath.Base(machine) + "/modules",
		filepath.Join(p.Config, "host"):     ".local/hosts/h1",
		filepath.Join(p.Config, "machine"):  ".local/machines/" + filepath.Base(machine),
	} {
		if got, err := os.Readlink(link); err != nil || got != target {
			t.Errorf("%s -> %q (%v), want %q", link, got, err, target)
		}
	}
	entry := filepath.Join(p.Modules, DefaultFile)
	if info, err := os.Lstat(entry); err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0444 {
		t.Errorf("modules/default.nix: %v, %v", info, err)
	}
	ignore := readFile(t, filepath.Join(p.Config, ".gitignore"))
	for _, line := range []string{"/modules/system", "/modules/default.nix", "/modules/host", "/modules/machine", "/host", "/machine"} {
		if !strings.Contains(ignore, line+"\n") {
			t.Errorf(".gitignore lacks %q:\n%s", line, ignore)
		}
	}
}

func TestRenderHost_FillsValues(t *testing.T) {
	out, err := RenderHost(HostValues{Channel: "nixos-25.11", Timezone: "Europe/Lisbon", Locale: "pt_PT.UTF-8", Keyboard: "pt"})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`luxos.inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-25.11";`,
		`time.timeZone = "Europe/Lisbon";`,
		`i18n.defaultLocale = "pt_PT.UTF-8";`,
		`services.xserver.xkb.layout = "pt";`,
		`console.useXkbConfig = true;`,
	} {
		if !strings.Contains(string(out), want) {
			t.Errorf("host.nix missing %q:\n%s", want, out)
		}
	}
}
