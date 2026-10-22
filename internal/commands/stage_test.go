package commands

import (
	"bytes"
	"errors"
	"io"
	"maps"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/DeprecatedLuar/luxos/internal/config"
	"github.com/DeprecatedLuar/luxos/internal/hardware"
	"github.com/DeprecatedLuar/luxos/internal/modules"
	"github.com/DeprecatedLuar/luxos/internal/nix"
	"github.com/DeprecatedLuar/luxos/internal/paths"
	"github.com/DeprecatedLuar/luxos/internal/staging"
	"github.com/DeprecatedLuar/luxos/internal/templates"
	"github.com/DeprecatedLuar/luxos/internal/ui"
)

// hostFlakeLock is the content written to .local/hosts/<host>/flake.lock in
// fixtures, distinct from the bogus one written at the config root so a
// test can tell which one staging.Materialize actually copied.
const hostFlakeLock = `{"nodes":{"root":{"inputs":{"nixpkgs":"nixpkgs_2","unstable":"unstable_2"}}}}` + "\n"

// fixtureUUID is the product_uuid every fixture exposes under its fake sysfs.
const fixtureUUID = "4c4c4544-0042-5110-8042-cac04f375433"

// machineDirOf returns the machine folder Run uses for p.
func machineDirOf(t *testing.T, p paths.Paths) string {
	t.Helper()
	key, err := hardware.Key(p.Sys)
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(p.Machines, key)
}

// hardwareConfigContent is the pre-written hardware-configuration.nix.
const hardwareConfigContent = "{ }\n"

// fakeNix returns flake steps that need no network, and
// the calls made, in order. The fake lock step writes a staged flake.lock when
// Materialize had none to copy, as `nix flake lock` would.
func fakeNix(t *testing.T) (flakeSteps, *[]string) {
	t.Helper()
	calls := &[]string{}
	return flakeSteps{
		lock: func(stagingDir string) error {
			*calls = append(*calls, "flake-lock")
			lock := filepath.Join(stagingDir, "flake.lock")
			if _, err := os.Stat(lock); os.IsNotExist(err) {
				return os.WriteFile(lock, []byte("{\"fake\":true}\n"), 0644)
			}
			return nil
		},
	}, calls
}

// runAll is a whole rebuild's preparation: prepare, stage, seal.
func runAll(w io.Writer, p paths.Paths, host string, prune bool, steps flakeSteps) error {
	out := ui.NewProgress(w)
	h, err := prepare(out, p, host, prune)
	if err != nil {
		return err
	}
	if err := stage(out, p, h, p.Staging, steps); err != nil {
		return err
	}
	return staging.Seal(p.Staging)
}

// fixture builds a config tree at root with two hosts: host1 (active),
// whose selection references a module by a stale path (the module has
// since moved to modules/misc/foo.nix), and host2, whose selection
// already references the module correctly. This computer's machine folder
// holds a hardware-configuration.nix and a boot.nix. Returns Paths with every
// field under root and the active host's name.
func fixture(t *testing.T) (paths.Paths, string) {
	t.Helper()
	root := t.TempDir()

	cfg := filepath.Join(root, "config")
	hosts := filepath.Join(cfg, ".local", "hosts")
	machines := filepath.Join(cfg, ".local", "machines")
	modulesDir := filepath.Join(cfg, "modules")
	staging := filepath.Join(root, "etc-nixos")
	sysDir := filepath.Join(root, "sys")
	mountsFile := filepath.Join(root, "mounts")

	// The module that host1's selection refers to by a now-stale path;
	// modules.Heal finds it by name ("foo") and rewrites the broken line to
	// point here.
	write(t, filepath.Join(modulesDir, "misc", "foo.nix"), "{ }\n")

	// host1: active host, stale import to heal.
	write(t, config.SelectionPath(filepath.Join(hosts, "host1")),
		"{ ... }:\n{\n  imports = [\n    ../old/foo.nix\n  ];\n}\n")
	write(t, filepath.Join(hosts, "host1", ".plsdonttouch.nix"),
		"{ system.stateVersion = \"25.11\"; }\n")
	write(t, filepath.Join(hosts, "host1", "host.nix"),
		"{\n  luxos.inputs.nixpkgs.url = \"github:NixOS/nixpkgs/nixos-25.11\";\n  time.timeZone = \"UTC\"; i18n.defaultLocale = \"en_US.UTF-8\"; }\n")
	write(t, filepath.Join(hosts, "host1", "flake.lock"), hostFlakeLock)

	// A flake.lock at the config root, which must be ignored: only the
	// active host's own .local/hosts/<host>/flake.lock is ever staged.
	write(t, filepath.Join(cfg, "flake.lock"), "{ \"root-lock\": true }\n")

	// host2: other host, already correct - modules.Heal must leave it be.
	write(t, config.SelectionPath(filepath.Join(hosts, "host2")),
		"{ ... }:\n{\n  imports = [\n    ../misc/foo.nix\n  ];\n}\n")
	write(t, filepath.Join(hosts, "host2", ".plsdonttouch.nix"),
		"{ system.stateVersion = \"25.11\"; }\n")
	write(t, filepath.Join(hosts, "host2", "host.nix"),
		"{\n  luxos.inputs.nixpkgs.url = \"github:NixOS/nixpkgs/nixos-25.11\";\n  time.timeZone = \"UTC\"; i18n.defaultLocale = \"en_US.UTF-8\"; }\n")

	write(t, filepath.Join(sysDir, "class", "dmi", "id", "product_uuid"), fixtureUUID+"\n")
	writeDMIFixture(t, sysDir)
	key, err := hardware.Key(sysDir)
	if err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(machines, key, config.HardwareConfigFile), hardwareConfigContent)
	write(t, filepath.Join(machines, key, config.ScopeModulesDir, config.BootFile), "{ ... }: { }\n")
	write(t, mountsFile, "")

	p := paths.Paths{
		Home:              root,
		User:              "test",
		Config:            cfg,
		Hosts:             hosts,
		Machines:          machines,
		Modules:           modulesDir,
		Staging:           staging,
		Backup:            filepath.Join(root, "backup"),
		Sys:               sysDir,
		Mounts:            mountsFile,
		RunningSelections: filepath.Join(root, "run-modules"),
	}
	return p, "host1"
}

func TestRun_EndToEnd(t *testing.T) {
	skipIfNoNix(t)

	p, host := fixture(t)
	steps, calls := fakeNix(t)

	var out bytes.Buffer
	if err := runAll(&out, p, host, false, steps); err != nil {
		t.Fatalf("Run: %v\noutput:\n%s", err, out.String())
	}

	if got := strings.Join(*calls, ","); got != "flake-lock" {
		t.Errorf("nix calls = %q, want flake-lock", got)
	}

	mustExist := []string{
		filepath.Join(p.Staging, "flake.nix"),
		filepath.Join(p.Staging, "framework", "configuration.nix"),
		filepath.Join(p.Staging, "framework", "system.nix"),
		filepath.Join(p.Staging, "config", "modules", "system", "desktop.nix"),
	}
	for _, f := range mustExist {
		if _, err := os.Stat(f); err != nil {
			t.Errorf("expected %s to exist: %v", f, err)
		}
	}
	for _, name := range []string{"configuration.nix"} {
		if _, err := os.Stat(filepath.Join(p.Staging, name)); err == nil {
			t.Errorf("%s must not exist at the staging root", name)
		}
	}

	machine := machineDirOf(t, p)

	// The pre-written boot.nix was left alone.
	if got := mustReadFile(t, filepath.Join(machine, "modules", "boot.nix")); got != "{ ... }: { }\n" {
		t.Errorf("boot.nix = %q, want it untouched", got)
	}

	// The machine folder is ensured before anything is staged.
	ensuring := strings.Index(out.String(), "Ensuring "+machine)
	materializing := strings.Index(out.String(), "Materializing "+p.Staging)
	if ensuring < 0 || ensuring > materializing {
		t.Errorf("want Ensuring the machine folder before Materializing, got:\n%s", out.String())
	}

	// The pre-written hardware-configuration.nix was left alone; the machine's
	// selection was seeded.
	hwConfig := filepath.Join(machine, "hardware-configuration.nix")
	if got := mustReadFile(t, hwConfig); got != hardwareConfigContent {
		t.Errorf("hardware-configuration.nix = %q, want %q", got, hardwareConfigContent)
	}
	if strings.Contains(out.String(), "created: "+hwConfig) {
		t.Errorf("output reported creating %s, got:\n%s", hwConfig, out.String())
	}
	if got := mustReadFile(t, config.SelectionPath(machine)); !strings.Contains(got, "./boot.nix") {
		t.Errorf("machine selection = %q, want the seed selecting ./boot.nix", got)
	}

	// The four links exist and modules/default.nix is a real read-only file.
	for link, target := range map[string]string{
		filepath.Join(p.Modules, "host"):    "../.local/hosts/" + host + "/modules",
		filepath.Join(p.Modules, "machine"): "../.local/machines/" + filepath.Base(machine) + "/modules",
		filepath.Join(p.Config, "host"):     ".local/hosts/" + host,
		filepath.Join(p.Config, "machine"):  ".local/machines/" + filepath.Base(machine),
	} {
		if got, err := os.Readlink(link); err != nil || got != target {
			t.Errorf("%s -> %q (%v), want %q", link, got, err, target)
		}
	}
	if info, err := os.Lstat(filepath.Join(p.Modules, "default.nix")); err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0444 {
		t.Errorf("modules/default.nix: %v, %v", info, err)
	}
	for _, staged := range []string{
		filepath.Join(p.Staging, "config", "modules", "machine", "boot.nix"),
		filepath.Join(p.Staging, "config", "hardware-configuration.nix"),
	} {
		if _, err := os.Stat(staged); err != nil {
			t.Errorf("expected %s staged: %v", staged, err)
		}
	}

	// The staged flake.lock came from .local/hosts/host1/flake.lock, not the
	// config root - the root copy (ignored) must not be what got staged.
	stagedLock := mustReadFile(t, filepath.Join(p.Staging, "flake.lock"))
	if stagedLock != hostFlakeLock {
		t.Errorf("staged flake.lock = %q, want the host's flake.lock %q", stagedLock, hostFlakeLock)
	}

	// The moved import was rewritten in host1's real selection.
	entrypoint := config.SelectionPath(filepath.Join(p.Hosts, host))
	content := mustReadFile(t, entrypoint)
	if strings.Contains(content, "old/foo.nix") {
		t.Errorf("entrypoint %s still references old/foo.nix:\n%s", entrypoint, content)
	}
	if !strings.Contains(content, "misc/foo.nix") {
		t.Errorf("entrypoint %s was not rewritten to misc/foo.nix:\n%s", entrypoint, content)
	}
	if !strings.Contains(out.String(), "old/foo.nix -> misc/foo.nix") {
		t.Errorf("output did not report the heal, got:\n%s", out.String())
	}

	// .gitignore contains every required line.
	gitignoreContent := mustReadFile(t, filepath.Join(p.Config, ".gitignore"))
	for _, line := range []string{"/modules/system", "/modules/default.nix", "/modules/host", "/modules/machine", "/host", "/machine"} {
		if !strings.Contains(gitignoreContent, line) {
			t.Errorf(".gitignore missing line %q, got:\n%s", line, gitignoreContent)
		}
	}

	// The environment file was created from the template and staged as is.
	tmpl, err := templates.File("starters/environment")
	if err != nil {
		t.Fatal(err)
	}
	if got := mustReadFile(t, filepath.Join(p.Config, "environment")); got != string(tmpl) {
		t.Errorf("environment = %q, want the template %q", got, tmpl)
	}
	if got := mustReadFile(t, filepath.Join(p.Staging, "config", "environment")); got != string(tmpl) {
		t.Errorf("staged environment = %q, want the template %q", got, tmpl)
	}

	// A second run finds nothing left to heal.
	var out2 bytes.Buffer
	if err := runAll(&out2, p, host, false, steps); err != nil {
		t.Fatalf("second Run: %v\noutput:\n%s", err, out2.String())
	}
	for _, marker := range []string{"-> ./", "Warning:", "scaffolded:", "added:", "created:", "removed "} {
		if strings.Contains(out2.String(), marker) {
			t.Errorf("second run reported a heal change (found %q), got:\n%s", marker, out2.String())
		}
	}
}

func TestRun_ExistingEnvironmentUntouched(t *testing.T) {
	skipIfNoNix(t)

	p, host := fixture(t)
	steps, _ := fakeNix(t)
	envPath := filepath.Join(p.Config, "environment")
	write(t, envPath, "")

	var out bytes.Buffer
	if err := runAll(&out, p, host, false, steps); err != nil {
		t.Fatalf("Run: %v\noutput:\n%s", err, out.String())
	}
	if got := mustReadFile(t, envPath); got != "" {
		t.Errorf("environment = %q, want it left empty", got)
	}
	if strings.Contains(out.String(), "created: "+envPath) {
		t.Errorf("output reported a creation, got:\n%s", out.String())
	}
}

func TestRun_BootConfigCreated(t *testing.T) {
	skipIfNoNix(t)

	p, host := fixture(t)
	steps, _ := fakeNix(t)

	// No pre-written boot.nix this time: fixture wrote one, so remove it and
	// fake an EFI sysfs with vfat mounted at /boot.
	bootConfig := filepath.Join(machineDirOf(t, p), "modules", "boot.nix")
	if err := os.Remove(bootConfig); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(p.Sys, "firmware", "efi", "efivars"), 0755); err != nil {
		t.Fatal(err)
	}
	write(t, p.Mounts, "/dev/sda1 /boot vfat rw 0 0\n")

	var out bytes.Buffer
	if err := runAll(&out, p, host, false, steps); err != nil {
		t.Fatalf("Run: %v\noutput:\n%s", err, out.String())
	}

	want, err := hardware.RenderBoot(hardware.Loader{EFI: true, Target: "/boot"})
	if err != nil {
		t.Fatal(err)
	}

	if got := mustReadFile(t, bootConfig); got != string(want) {
		t.Errorf("boot.nix = %q, want %q", got, want)
	}
	if !strings.Contains(out.String(), "created:") {
		t.Errorf("output did not report the creation, got:\n%s", out.String())
	}
}

// writeGPUDevice writes a fake sysfs PCI device directory
// <sysDir>/bus/pci/devices/<addr> with the given class/vendor hex strings,
// matching internal/hardware's own test fixture shape.
func writeGPUDevice(t *testing.T, sysDir, addr, class, vendor string) {
	t.Helper()
	dir := filepath.Join(sysDir, "bus", "pci", "devices", addr)
	mustMkdirAll(t, dir)
	write(t, filepath.Join(dir, "class"), class+"\n")
	write(t, filepath.Join(dir, "vendor"), vendor+"\n")
	write(t, filepath.Join(dir, "device"), "0x1234\n")
}

// writeDMIFixture writes the DMI files stage reads under the fake sysfs.
func writeDMIFixture(t *testing.T, sysDir string) {
	t.Helper()
	dir := filepath.Join(sysDir, "class", "dmi", "id")
	write(t, filepath.Join(dir, "sys_vendor"), "Vendor\n")
	write(t, filepath.Join(dir, "product_name"), "Product\n")
	write(t, filepath.Join(dir, "chassis_type"), "10\n")
}

func TestRun_HardwareFactsDetectedAndStaged(t *testing.T) {
	skipIfNoNix(t)

	p, host := fixture(t)
	steps, _ := fakeNix(t)

	// A single NVIDIA GPU in the fake sysfs.
	writeGPUDevice(t, p.Sys, "0000:01:00.0", "0x030200", "0x10de")

	var out bytes.Buffer
	if err := runAll(&out, p, host, false, steps); err != nil {
		t.Fatalf("Run: %v\noutput:\n%s", err, out.String())
	}

	factsFile := filepath.Join(p.Staging, "framework", "hardware-facts.nix")
	got := mustReadFile(t, factsFile)

	want, err := hardware.Render(hardware.Facts{
		GPUs: []hardware.GPU{
			{BusID: "PCI:1@0:0:0", Vendor: "nvidia", VendorID: "0x10de", DeviceID: "0x1234", Class: "3d", BootVGA: false},
		},
		DMI:       hardware.DMI{Vendor: "Vendor", Product: "Product", ChassisType: 10},
		CPUVendor: "unknown",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Errorf("staged framework/hardware-facts.nix = %q, want %q", got, want)
	}
	if _, err := os.Stat(filepath.Join(p.Staging, "framework", "gpu.nix")); !os.IsNotExist(err) {
		t.Errorf("framework/gpu.nix must not be staged, stat err = %v", err)
	}

	if !strings.Contains(out.String(), "Detecting hardware...") {
		t.Errorf("output did not report hardware detection, got:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "nvidia 3d: PCI:1@0:0:0") {
		t.Errorf("output did not report the detected GPU, got:\n%s", out.String())
	}
}

func TestRun_HostModuleSelected(t *testing.T) {
	skipIfNoNix(t)

	p, host := fixture(t)
	steps, _ := fakeNix(t)
	write(t, filepath.Join(p.Hosts, host, "modules", "mine.nix"), "{ }\n")
	write(t, config.SelectionPath(filepath.Join(p.Hosts, host)),
		"{ ... }:\n{\n  imports = [\n    ./mine.nix\n  ];\n}\n")

	var out bytes.Buffer
	if err := runAll(&out, p, host, false, steps); err != nil {
		t.Fatalf("Run: %v\noutput:\n%s", err, out.String())
	}

	link := filepath.Join(p.Modules, "host")
	if got, err := os.Readlink(link); err != nil || got != "../.local/hosts/"+host+"/modules" {
		t.Errorf("%s -> %q (%v), want the host's modules", link, got, err)
	}
	stagedMine := filepath.Join(p.Staging, "config", "modules", "host", "mine.nix")
	if _, err := os.Stat(stagedMine); err != nil {
		t.Errorf("expected %s to exist: %v", stagedMine, err)
	}

	flakeNix := mustReadFile(t, filepath.Join(p.Staging, "flake.nix"))
	if want := `import ./framework/outputs.nix inputs "host1"`; !strings.Contains(flakeNix, want) {
		t.Errorf("flake.nix missing %q, got:\n%s", want, flakeNix)
	}

	// The host had a lock: nothing new to copy back.
	if _, err := os.Stat(filepath.Join(p.Hosts, host, "flake.lock")); err != nil {
		t.Errorf("host lock missing: %v", err)
	}
}

func TestRun_CrossScopeLineMoved(t *testing.T) {
	skipIfNoNix(t)

	p, host := fixture(t)
	steps, _ := fakeNix(t)
	write(t, config.SelectionPath(filepath.Join(p.Hosts, host)),
		"{ ... }:\n{\n  imports = [\n    ../machine/boot.nix\n  ];\n}\n")

	var out bytes.Buffer
	if err := runAll(&out, p, host, false, steps); err != nil {
		t.Fatalf("Run: %v\noutput:\n%s", err, out.String())
	}

	hostSel := mustReadFile(t, config.SelectionPath(filepath.Join(p.Hosts, host)))
	if strings.Contains(hostSel, "boot") {
		t.Errorf("host selection still holds the machine line:\n%s", hostSel)
	}
	machineSel := mustReadFile(t, config.SelectionPath(machineDirOf(t, p)))
	if strings.Count(machineSel, "./boot.nix") != 1 {
		t.Errorf("machine selection should select ./boot.nix once:\n%s", machineSel)
	}
	if !strings.Contains(out.String(), "moved: boot → modules/machine/default.nix") {
		t.Errorf("output did not report the move, got:\n%s", out.String())
	}
}

func TestRun_SelectionWithSettingFails(t *testing.T) {
	skipIfNoNix(t)

	p, host := fixture(t)
	steps, _ := fakeNix(t)
	hostSel := config.SelectionPath(filepath.Join(p.Hosts, host))
	write(t, hostSel, "{ ... }:\n{\n  imports = [\n    ../misc/foo.nix\n  ];\n  services.x.enable = true;\n}\n")

	var out bytes.Buffer
	err := runAll(&out, p, host, false, steps)
	if err == nil || !strings.Contains(err.Error(), hostSel) {
		t.Fatalf("err = %v, want one naming %s", err, hostSel)
	}
	for _, dir := range []string{p.NewStage, p.Staging} {
		if dir == "" {
			continue
		}
		if _, statErr := os.Stat(filepath.Join(dir, "framework")); !os.IsNotExist(statErr) {
			t.Errorf("%s staged despite the selection error: %v", dir, statErr)
		}
	}
}

func TestRun_StrayHostFileFails(t *testing.T) {
	skipIfNoNix(t)

	p, host := fixture(t)
	steps, _ := fakeNix(t)

	stray := filepath.Join(p.Hosts, host, "hardware.nix")
	write(t, stray, "{ }\n")

	var out bytes.Buffer
	err := runAll(&out, p, host, false, steps)
	if err == nil {
		t.Fatal("expected an error for the stray host file, got nil")
	}
	if !strings.Contains(err.Error(), "hardware.nix does not belong here") {
		t.Errorf("error = %q, want mention of hardware.nix", err.Error())
	}
}

func TestRun_UnimportedViolationIgnored(t *testing.T) {
	skipIfNoNix(t)

	p, host := fixture(t)
	steps, _ := fakeNix(t)

	// Broken, but no host imports it: it is never built, so never checked.
	write(t, filepath.Join(p.Modules, "unused.nix"), "{ imports = [ ../outside.nix ]; }\n")

	var out bytes.Buffer
	if err := runAll(&out, p, host, false, steps); err != nil {
		t.Fatalf("Run: %v\noutput:\n%s", err, out.String())
	}
}

func TestRun_BoundaryViolation(t *testing.T) {
	skipIfNoNix(t)

	p, host := fixture(t)
	steps, _ := fakeNix(t)

	// A module referencing a path outside its own module - a boundary
	// violation refs.Validate must catch before anything is staged.
	write(t, filepath.Join(p.Modules, "bad.nix"), "{ imports = [ ../outside.nix ]; }\n")
	write(t, config.SelectionPath(filepath.Join(p.Hosts, host)),
		"{ ... }:\n{\n  imports = [\n    ../misc/foo.nix\n    ../bad.nix\n  ];\n}\n")

	var out bytes.Buffer
	err := runAll(&out, p, host, false, steps)
	if err == nil {
		t.Fatal("expected an error for the boundary violation, got nil")
	}
	if !strings.Contains(err.Error(), "module boundary violations") {
		t.Errorf("error = %q, want mention of module boundary violations", err.Error())
	}
	if !strings.Contains(out.String(), "[ERROR] ") {
		t.Errorf("output did not report the violation, got:\n%s", out.String())
	}

	if _, statErr := os.Stat(filepath.Join(p.Staging, "framework")); !os.IsNotExist(statErr) {
		t.Errorf("nothing should be staged after a boundary violation, err=%v", statErr)
	}
}

func TestAdoptStaging_StrangerMovedToBackup(t *testing.T) {
	p, _ := fixture(t)
	write(t, filepath.Join(p.Staging, "old-config.nix"), "{ }\n")

	var out bytes.Buffer
	if err := adoptStaging(ui.NewProgress(&out), p); err != nil {
		t.Fatalf("adoptStaging: %v\noutput:\n%s", err, out.String())
	}
	if _, err := os.Lstat(filepath.Join(p.Staging, "old-config.nix")); !os.IsNotExist(err) {
		t.Errorf("stranger should have left the staging root, err=%v", err)
	}
	moved, err := filepath.Glob(filepath.Join(p.Backup, "*", "old-config.nix"))
	if err != nil || len(moved) != 1 {
		t.Fatalf("backup holds %v (err %v), want exactly one old-config.nix", moved, err)
	}
	if !strings.Contains(out.String(), "moved: "+filepath.Join(p.Staging, "old-config.nix")+" -> "+moved[0]) {
		t.Errorf("output did not report the move, got:\n%s", out.String())
	}
}

func TestAdoptStaging_NoBackupDirNamesFlag(t *testing.T) {
	p, _ := fixture(t)
	p.Backup = ""
	write(t, filepath.Join(p.Staging, "old-config.nix"), "{ }\n")

	err := adoptStaging(ui.NewProgress(io.Discard), p)
	if err == nil || !strings.Contains(err.Error(), "luxos rebuild --backup-dir <path>") {
		t.Fatalf("err = %v, want the --backup-dir recovery line", err)
	}
}

func TestRun_SecondRunLeavesMachineFolder(t *testing.T) {
	skipIfNoNix(t)
	p, host := fixture(t)
	steps, _ := fakeNix(t)

	var out bytes.Buffer
	for i := range 2 {
		if err := runAll(&out, p, host, false, steps); err != nil {
			t.Fatalf("run %d: %v\noutput:\n%s", i+1, err, out.String())
		}
	}
	if got := mustReadFile(t, filepath.Join(machineDirOf(t, p), "hardware-configuration.nix")); got != hardwareConfigContent {
		t.Errorf("hardware-configuration.nix = %q after second run, want %q", got, hardwareConfigContent)
	}
}

func TestStageDoesNotAdopt(t *testing.T) {
	skipIfNoNix(t)
	p, host := fixture(t)
	steps, _ := fakeNix(t)
	discard := ui.NewProgress(io.Discard)
	machineDir, err := config.Ensure(discard, configDirs(p), host)
	if err != nil {
		t.Fatal(err)
	}
	h, err := modules.Load(p.Modules, filepath.Join(p.Hosts, host), machineDir)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	write(t, filepath.Join(dir, "old-config.nix"), "{ }\n")

	if err := stage(discard, p, h, dir, steps); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "old-config.nix")); err != nil {
		t.Errorf("stage moved an entry it does not own: %v", err)
	}
	if _, err := os.Stat(p.Backup); !os.IsNotExist(err) {
		t.Errorf("stage created the backup dir (err %v)", err)
	}
}

func TestRun_MissingProductUUIDFails(t *testing.T) {
	skipIfNoNix(t)
	p, host := fixture(t)
	steps, _ := fakeNix(t)
	if err := os.Remove(filepath.Join(p.Sys, "class", "dmi", "id", "product_uuid")); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := runAll(&out, p, host, false, steps); err == nil || !strings.Contains(err.Error(), "product_uuid") {
		t.Fatalf("err = %v, want the product_uuid error", err)
	}
}

func TestRun_MissingBaseChannelStopsBeforeStaging(t *testing.T) {
	skipIfNoNix(t)
	p, host := fixture(t)
	steps, _ := fakeNix(t)
	write(t, filepath.Join(p.Hosts, host, "host.nix"), "{ time.timeZone = \"UTC\"; }\n")

	var out bytes.Buffer
	err := runAll(&out, p, host, false, steps)
	if err == nil || !strings.Contains(err.Error(), "missing base channel") || !strings.Contains(err.Error(), "luxos.inputs.nixpkgs.url") {
		t.Fatalf("err = %v, want missing base channel error naming the line", err)
	}
	if _, statErr := os.Stat(filepath.Join(p.Staging, "framework")); !os.IsNotExist(statErr) {
		t.Errorf("nothing should be staged, err=%v", statErr)
	}
}

func TestRun_HostFilePathStopsBeforeStaging(t *testing.T) {
	skipIfNoNix(t)
	p, host := fixture(t)
	steps, _ := fakeNix(t)
	write(t, filepath.Join(p.Hosts, host, "host.nix"), "{ imports = [ ./x.nix ]; }\n")

	var out bytes.Buffer
	if err := runAll(&out, p, host, false, steps); err == nil || !strings.Contains(err.Error(), "host.nix") {
		t.Fatalf("err = %v, want host.nix error", err)
	}
	if _, statErr := os.Stat(filepath.Join(p.Staging, "framework")); !os.IsNotExist(statErr) {
		t.Errorf("nothing should be staged, err=%v", statErr)
	}
}

func TestStageLeavesConfigUntouched(t *testing.T) {
	skipIfNoNix(t)
	p, host := fixture(t)
	steps, _ := fakeNix(t)
	discard := ui.NewProgress(io.Discard)
	machineDir, err := config.Ensure(discard, configDirs(p), host)
	if err != nil {
		t.Fatal(err)
	}
	h, err := modules.Load(p.Modules, filepath.Join(p.Hosts, host), machineDir)
	if err != nil {
		t.Fatal(err)
	}
	before := snapshotTree(t, p.Config)

	out := t.TempDir()
	if err := stage(discard, p, h, out, steps); err != nil {
		t.Fatal(err)
	}

	after := snapshotTree(t, p.Config)
	lock := filepath.ToSlash(filepath.Join(".local", "hosts", host, "flake.lock"))
	delete(before, lock)
	delete(after, lock)
	if !maps.Equal(before, after) {
		t.Errorf("stage changed CONFIG_DIR:\n%s", diffKeys(before, after))
	}
}

// A temporary stage gets its lock rewritten by `nix flake update` as the
// invoking user, so stage must leave it writable.
func TestStageLeavesLockWritable(t *testing.T) {
	skipIfNoNix(t)
	p, host := fixture(t)
	steps, _ := fakeNix(t)
	discard := ui.NewProgress(io.Discard)
	machineDir, err := config.Ensure(discard, configDirs(p), host)
	if err != nil {
		t.Fatal(err)
	}
	h, err := modules.Load(p.Modules, filepath.Join(p.Hosts, host), machineDir)
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	if err := stage(discard, p, h, dir, steps); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(filepath.Join(dir, flakeLockName))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0200 == 0 {
		t.Errorf("flake.lock mode = %v, want owner-writable", info.Mode().Perm())
	}
}

// snapshotTree maps every path under root (links not followed) to its mode,
// link target or content.
func snapshotTree(t *testing.T, root string) map[string]string {
	t.Helper()
	snap := map[string]string{}
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		val := info.Mode().String()
		switch {
		case info.Mode()&os.ModeSymlink != 0:
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			val += " -> " + target
		case info.Mode().IsRegular():
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			val += " " + string(data)
		}
		snap[filepath.ToSlash(rel)] = val
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return snap
}

// diffKeys lists, sorted, the keys present in only one map or with different values.
func diffKeys(a, b map[string]string) string {
	var keys []string
	for k, v := range a {
		if w, ok := b[k]; !ok || w != v {
			keys = append(keys, k)
		}
	}
	for k := range b {
		if _, ok := a[k]; !ok {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	return strings.Join(keys, "\n")
}

func TestRun_BrokenSettingsFailBeforeStaging(t *testing.T) {
	skipIfNoNix(t)
	p, host := fixture(t)
	steps, _ := fakeNix(t)
	write(t, filepath.Join(p.Modules, "laptop", "default.nix"), "{ ... }: { imports = [ ./options.nix ]; }\n")
	write(t, filepath.Join(p.Modules, "laptop", "options.nix"), "{ lib, ... }: { options.laptop.a = lib.mkOption { type = lib.types.int; }; }\n")
	if err := nix.AddImport(config.SelectionPath(filepath.Join(p.Hosts, host)), "../laptop"); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	err := runAll(&out, p, host, false, steps)
	if err == nil || !strings.Contains(out.String(), "has no default") {
		t.Fatalf("err = %v, output:\n%s\nwant the rule named", err, out.String())
	}
	if _, statErr := os.Stat(filepath.Join(p.Staging, "framework")); !os.IsNotExist(statErr) {
		t.Errorf("staged despite broken settings: %v", statErr)
	}
}

func TestRun_SettingsCreatedAndStaged(t *testing.T) {
	skipIfNoNix(t)
	p, host := fixture(t)
	steps, _ := fakeNix(t)
	write(t, filepath.Join(p.Modules, "laptop", "default.nix"), "{ ... }: { imports = [ ./options.nix ]; }\n")
	write(t, filepath.Join(p.Modules, "laptop", "options.nix"), "{ lib, ... }: { options.laptop.a = lib.mkOption { type = lib.types.int; default = 1; }; }\n")
	if err := nix.AddImport(config.SelectionPath(filepath.Join(p.Hosts, host)), "../laptop"); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := runAll(&out, p, host, false, steps); err != nil {
		t.Fatalf("Run: %v\n%s", err, out.String())
	}
	if got := mustReadFile(t, filepath.Join(p.Hosts, host, "settings", "laptop.nix")); got != "{\n  laptop.a = 1;\n}\n" {
		t.Errorf("settings file = %q", got)
	}
	if got := mustReadFile(t, filepath.Join(p.Staging, "config", "settings", "laptop.nix")); !strings.Contains(got, "laptop.a = 1;") {
		t.Errorf("staged settings = %q", got)
	}
}

func TestStageWithoutMachineLinkFails(t *testing.T) {
	skipIfNoNix(t)
	p, host := fixture(t)
	err := withTempStage(p, host, "luxos-test-", func(string) error { return nil })
	if !errors.Is(err, config.ErrNoMachineLink) {
		t.Fatalf("err = %v, want the missing-link error", err)
	}
}

func TestRun_SelectedUserWithoutPasswordFailsBeforeStaging(t *testing.T) {
	skipIfNoNix(t)
	p, host := fixture(t)
	steps, _ := fakeNix(t)
	if err := config.WriteUser(filepath.Join(p.Modules, "users", "ana")); err != nil {
		t.Fatal(err)
	}
	if err := nix.AddImport(config.SelectionPath(filepath.Join(p.Hosts, host)), "../users/ana"); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	err := runAll(&out, p, host, false, steps)
	if err == nil || !strings.Contains(err.Error()+out.String(), "luxos user passwd ana") {
		t.Fatalf("err = %v, output:\n%s\nwant the missing password named", err, out.String())
	}
	if _, statErr := os.Stat(filepath.Join(p.Staging, "framework")); !os.IsNotExist(statErr) {
		t.Errorf("staged despite the missing password: %v", statErr)
	}
}
