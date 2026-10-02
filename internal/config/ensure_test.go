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
	"github.com/DeprecatedLuar/luxos/internal/templates"
	"github.com/DeprecatedLuar/luxos/internal/ui"
)

const fixtureUUID = "4c4c4544-0042-5110-8042-cac04f375433"

func skipIfNoNix(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("nix-instantiate"); err != nil {
		t.Skip("nix-instantiate not on PATH")
	}
}

// hardwareFixture builds a fake sysfs and a hardware root whose folder already
// holds a hardware-configuration.nix and a boot.nix.
func hardwareFixture(t *testing.T) paths.Paths {
	t.Helper()
	root := t.TempDir()
	sysDir := filepath.Join(root, "sys")
	writeFile(t, filepath.Join(sysDir, "class", "dmi", "id", "product_uuid"), fixtureUUID+"\n")
	hwDir, err := HardwareDir(sysDir, filepath.Join(root, "hardware"))
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(hwDir, HardwareConfigFile), "{ }\n")
	writeFile(t, filepath.Join(hwDir, BootFile), "{ ... }: { }\n")
	mounts := filepath.Join(root, "mounts")
	writeFile(t, mounts, "")
	return paths.Paths{Sys: sysDir, HardwareRoot: filepath.Join(root, "hardware"), Mounts: mounts}
}

func TestHardwareDirUsesKey(t *testing.T) {
	p := hardwareFixture(t)
	key, err := hardware.Key(p.Sys)
	if err != nil {
		t.Fatal(err)
	}
	got, err := HardwareDir(p.Sys, p.HardwareRoot)
	if err != nil || got != filepath.Join(p.HardwareRoot, key) {
		t.Errorf("HardwareDir = %q, %v", got, err)
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
	hostModules := filepath.Join(root, ".local", "machines", "box", "modules")
	for _, d := range []string{modules, hostModules} {
		if err := os.MkdirAll(d, 0755); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := ActiveHost(modules); err == nil {
		t.Fatal("want error with no modules/local link")
	}
	if err := os.Symlink(hostModules, filepath.Join(modules, localLinkName)); err != nil {
		t.Fatal(err)
	}
	if got, err := ActiveHost(modules); err != nil || got != "box" {
		t.Errorf("ActiveHost = %q, %v; want box", got, err)
	}
}

func TestEnsureMachineFile_CreatesFromTemplate(t *testing.T) {
	hostDir := t.TempDir()
	var out bytes.Buffer
	if err := ensureMachineFile(ui.NewProgress(&out), hostDir); err != nil {
		t.Fatal(err)
	}
	want, err := templates.File(machineTemplate)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(hostDir, "machine.nix")
	if got := readFile(t, path); got != string(want) {
		t.Errorf("machine.nix differs from template:\n%s", got)
	}
	if !strings.Contains(out.String(), path) {
		t.Errorf("output %q does not name %s", out.String(), path)
	}
}

func TestEnsureMachineFile_ExistingUntouched(t *testing.T) {
	hostDir := t.TempDir()
	path := filepath.Join(hostDir, "machine.nix")
	const own = "{ ... }: { }\n"
	writeFile(t, path, own)
	var out bytes.Buffer
	if err := ensureMachineFile(ui.NewProgress(&out), hostDir); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, path); got != own {
		t.Errorf("machine.nix changed: %q", got)
	}
	if out.Len() != 0 {
		t.Errorf("unexpected output %q", out.String())
	}
}

func TestMachineTemplate_Parses(t *testing.T) {
	skipIfNoNix(t)
	tmpl, err := templates.File(machineTemplate)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "machine.nix")
	writeFile(t, path, string(tmpl))
	if out, err := exec.Command("nix-instantiate", "--parse", path).CombinedOutput(); err != nil {
		t.Fatalf("template does not parse: %v\n%s", err, out)
	}
}

func TestMachineTemplate_NoGC(t *testing.T) {
	tmpl, err := templates.File(machineTemplate)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(tmpl), "nix.gc") {
		t.Error("machine template must not define nix.gc")
	}
}

func TestHardwareTemplate_Parses(t *testing.T) {
	skipIfNoNix(t)
	tmpl, err := templates.File(hardwareDefaultTemplate)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "default.nix")
	writeFile(t, path, string(tmpl))
	if out, err := exec.Command("nix-instantiate", "--parse", path).CombinedOutput(); err != nil {
		t.Fatalf("template does not parse: %v\n%s", err, out)
	}
}

func TestCheckMachineFile(t *testing.T) {
	skipIfNoNix(t)
	tmpl, err := templates.File(machineTemplate)
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
			path := filepath.Join(t.TempDir(), "machine.nix")
			writeFile(t, path, c.content)
			err := checkMachineFile(path)
			if (err != nil) != c.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, c.wantErr)
			}
			if err != nil && !strings.Contains(err.Error(), path) {
				t.Errorf("error %q does not name the file", err)
			}
		})
	}
}

func TestEnsureHardware_DefaultCreatedOnce(t *testing.T) {
	skipIfNoNix(t)
	p := hardwareFixture(t)
	var out bytes.Buffer
	hw, err := ensureHardware(ui.NewProgress(&out), p)
	if err != nil {
		t.Fatal(err)
	}
	def := filepath.Join(hw, DefaultFile)
	tmpl, err := templates.File(hardwareDefaultTemplate)
	if err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, def); got != string(tmpl) {
		t.Errorf("default.nix differs from the template:\n%s", got)
	}
	if !strings.Contains(out.String(), "created: "+def) {
		t.Errorf("output does not report creating %s:\n%s", def, out.String())
	}

	writeFile(t, def, "{ }\n")
	out.Reset()
	if _, err := ensureHardware(ui.NewProgress(&out), p); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, def); got != "{ }\n" {
		t.Errorf("edited default.nix was rewritten: %q", got)
	}
	if strings.Contains(out.String(), "created: "+def) {
		t.Errorf("second run reported creating default.nix:\n%s", out.String())
	}
}
