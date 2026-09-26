package heal

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DeprecatedLuar/luxos/internal/computer"
	config_ "github.com/DeprecatedLuar/luxos/internal/config"
	"github.com/DeprecatedLuar/luxos/internal/framework"
	"github.com/DeprecatedLuar/luxos/internal/paths"
)

// hostFlakeLock is the content written to .local/machines/<host>/flake.lock in
// fixtures, distinct from the bogus one written at the config root so a
// test can tell which one staging.Materialize actually copied.
const hostFlakeLock = `{"nodes":{"root":{"inputs":{"nixpkgs":"nixpkgs_2","unstable":"unstable_2"}}}}` + "\n"

// fixtureUUID is the product_uuid every fixture exposes under its fake sysfs.
const fixtureUUID = "4c4c4544-0042-5110-8042-cac04f375433"

// hardwareDir returns the hardware folder Run uses for p.
func hardwareDir(t *testing.T, p paths.Paths) string {
	t.Helper()
	key, err := computer.HardwareKey(p.Sys)
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(p.HardwareRoot, key)
}

// hardwareConfigContent is the pre-written hardware-configuration.nix.
const hardwareConfigContent = "{ }\n"

func skipIfNoNix(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("nix-instantiate"); err != nil {
		t.Skip("nix-instantiate not on PATH")
	}
}

// fakeNix replaces the nix invocations for the test and returns the calls
// made, in order. The fake lock step writes a staged flake.lock when
// staging.Materialize had none to copy, as `nix flake lock` would.
func fakeNix(t *testing.T) *[]string {
	t.Helper()
	calls := &[]string{}
	origWrite, origLock := writeFlake, flakeLock
	t.Cleanup(func() { writeFlake, flakeLock = origWrite, origLock })

	writeFlake = func(stagingDir string) error {
		*calls = append(*calls, "write-flake")
		if _, err := os.Stat(filepath.Join(stagingDir, "framework", "flake-file.nix")); err != nil {
			t.Errorf("write-flake ran before flake-file.nix was installed: %v", err)
		}
		return nil
	}
	flakeLock = func(stagingDir string) error {
		*calls = append(*calls, "flake-lock")
		lock := filepath.Join(stagingDir, "flake.lock")
		if _, err := os.Stat(lock); os.IsNotExist(err) {
			return os.WriteFile(lock, []byte("{\"fake\":true}\n"), 0644)
		}
		return nil
	}
	return calls
}

func mustMkdirAll(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
}

func write(t *testing.T, path, content string) {
	t.Helper()
	mustMkdirAll(t, filepath.Dir(path))
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func mustReadFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

// fixture builds a config tree at root with two hosts: host1 (active),
// whose entrypoint references a module by a stale path (the module has
// since moved to modules/misc/foo.nix), and host2, whose entrypoint
// already references the module correctly. Returns Paths with every field
// under root and the active host's name.
func fixture(t *testing.T) (paths.Paths, string) {
	t.Helper()
	root := t.TempDir()

	config := filepath.Join(root, "config")
	local := filepath.Join(config, ".local", "machines")
	modules := filepath.Join(config, "modules")
	staging := filepath.Join(root, "etc-nixos")
	sysDir := filepath.Join(root, "sys")
	mountsFile := filepath.Join(root, "mounts")

	// The module that host1's entrypoint refers to by a now-stale path;
	// units.Resolve finds it by name ("foo") and imports.Heal rewrites the
	// broken line to point here.
	write(t, filepath.Join(modules, "misc", "foo.nix"), "{ }\n")

	// host1: active host, stale import to heal.
	write(t, filepath.Join(local, "host1", "modules.nix"),
		"{ ... }:\n{\n  imports = [\n    ./old/foo.nix\n  ];\n}\n")
	write(t, filepath.Join(local, "host1", ".plsdonttouch.nix"),
		"{ system.stateVersion = \"25.11\"; }\n")
	write(t, filepath.Join(local, "host1", "machine.nix"),
		"{ time.timeZone = \"UTC\"; i18n.defaultLocale = \"en_US.UTF-8\"; }\n")
	write(t, filepath.Join(local, "host1", "flake.lock"), hostFlakeLock)

	// A flake.lock at the config root, which must be ignored: only the
	// active host's own .local/machines/<host>/flake.lock is ever staged.
	write(t, filepath.Join(config, "flake.lock"), "{ \"root-lock\": true }\n")

	// host2: other host, already correct - imports.Heal must leave it be.
	write(t, filepath.Join(local, "host2", "modules.nix"),
		"{ ... }:\n{\n  imports = [\n    ./misc/foo.nix\n  ];\n}\n")
	write(t, filepath.Join(local, "host2", ".plsdonttouch.nix"),
		"{ system.stateVersion = \"25.11\"; }\n")
	write(t, filepath.Join(local, "host2", "machine.nix"),
		"{ time.timeZone = \"UTC\"; i18n.defaultLocale = \"en_US.UTF-8\"; }\n")

	hardwareRoot := filepath.Join(config, ".local", "hardware")
	write(t, filepath.Join(sysDir, "class", "dmi", "id", "product_uuid"), fixtureUUID+"\n")
	key, err := computer.HardwareKey(sysDir)
	if err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(hardwareRoot, key, config_.HardwareConfigFile), hardwareConfigContent)
	write(t, filepath.Join(hardwareRoot, key, config_.BootFile), "{ ... }: { }\n")
	write(t, mountsFile, "")

	p := paths.Paths{
		Home:           root,
		User:           "test",
		Config:         config,
		Machines:       local,
		Modules:        modules,
		Staging:        staging,
		Backup:         filepath.Join(root, "backup"),
		HardwareRoot:   hardwareRoot,
		Sys:            sysDir,
		Mounts:         mountsFile,
		RunningModules: filepath.Join(root, "run-modules.nix"),
	}
	return p, "host1"
}

func TestRun_EndToEnd(t *testing.T) {
	skipIfNoNix(t)

	p, host := fixture(t)
	calls := fakeNix(t)

	var out bytes.Buffer
	if err := Run(&out, p, host, false); err != nil {
		t.Fatalf("Run: %v\noutput:\n%s", err, out.String())
	}

	if got := strings.Join(*calls, ","); got != "write-flake,flake-lock" {
		t.Errorf("nix calls = %q, want write-flake then flake-lock", got)
	}

	mustExist := []string{
		filepath.Join(p.Staging, "flake.nix"),
		filepath.Join(p.Staging, "framework", "flake-file.nix"),
		filepath.Join(p.Staging, "framework", "configuration.nix"),
		filepath.Join(p.Staging, "framework", "system.nix"),
		filepath.Join(p.Staging, "config", "modules", "system", "desktop.nix"),
	}
	for _, f := range mustExist {
		if _, err := os.Stat(f); err != nil {
			t.Errorf("expected %s to exist: %v", f, err)
		}
	}
	for _, name := range []string{"flake-file.nix", "configuration.nix"} {
		if _, err := os.Stat(filepath.Join(p.Staging, name)); err == nil {
			t.Errorf("%s must not exist at the staging root", name)
		}
	}

	hw := hardwareDir(t, p)

	// The pre-written boot.nix was left alone.
	if got := mustReadFile(t, filepath.Join(hw, "boot.nix")); got != "{ ... }: { }\n" {
		t.Errorf("boot.nix = %q, want it untouched", got)
	}

	// The hardware folder is ensured before anything is staged.
	ensuringHW := strings.Index(out.String(), "Ensuring "+hw)
	materializing := strings.Index(out.String(), "Materializing "+p.Staging)
	if ensuringHW < 0 || ensuringHW > materializing {
		t.Errorf("want Ensuring hardware before Materializing, got:\n%s", out.String())
	}

	// The pre-written hardware-configuration.nix was left alone; hardware.nix
	// was created from the template.
	hwConfig := filepath.Join(hw, "hardware-configuration.nix")
	if got := mustReadFile(t, hwConfig); got != hardwareConfigContent {
		t.Errorf("hardware-configuration.nix = %q, want %q", got, hardwareConfigContent)
	}
	if strings.Contains(out.String(), "created: "+hwConfig) {
		t.Errorf("output reported creating %s, got:\n%s", hwConfig, out.String())
	}
	hwTmpl, err := framework.File("templates/hardware.nix")
	if err != nil {
		t.Fatal(err)
	}
	if got := mustReadFile(t, filepath.Join(hw, "hardware.nix")); got != string(hwTmpl) {
		t.Errorf("hardware.nix differs from the template:\n%s", got)
	}

	// The host's modules/ links to it, and it is staged under config/modules/local/hardware-support.
	target, err := os.Readlink(filepath.Join(p.Machines, host, "modules", "hardware-support"))
	if err != nil || target != "../../../hardware/"+filepath.Base(hw) {
		t.Errorf("hardware link = %q (%v)", target, err)
	}
	for _, name := range []string{"default.nix", "hardware-configuration.nix", "boot.nix", "hardware.nix"} {
		staged := filepath.Join(p.Staging, "config", "modules", "local", "hardware-support", name)
		if _, err := os.Stat(staged); err != nil {
			t.Errorf("expected %s staged: %v", staged, err)
		}
	}

	// The staged flake.lock came from .local/machines/host1/flake.lock, not the
	// config root - the root copy (ignored) must not be what got staged.
	stagedLock := mustReadFile(t, filepath.Join(p.Staging, "flake.lock"))
	if stagedLock != hostFlakeLock {
		t.Errorf("staged flake.lock = %q, want the host's flake.lock %q", stagedLock, hostFlakeLock)
	}

	// The moved import was rewritten in host1's real entrypoint.
	entrypoint := filepath.Join(p.Machines, host, "modules.nix")
	content := mustReadFile(t, entrypoint)
	if strings.Contains(content, "old/foo.nix") {
		t.Errorf("entrypoint %s still references old/foo.nix:\n%s", entrypoint, content)
	}
	if !strings.Contains(content, "misc/foo.nix") {
		t.Errorf("entrypoint %s was not rewritten to misc/foo.nix:\n%s", entrypoint, content)
	}
	if !strings.Contains(out.String(), "./old/foo.nix -> ./misc/foo.nix") {
		t.Errorf("output did not report the heal, got:\n%s", out.String())
	}

	// .gitignore contains every required line.
	gitignoreContent := mustReadFile(t, filepath.Join(p.Config, ".gitignore"))
	for _, line := range gitignoreLines {
		if !strings.Contains(gitignoreContent, line) {
			t.Errorf(".gitignore missing line %q, got:\n%s", line, gitignoreContent)
		}
	}

	// The environment file was created from the template and staged as is.
	tmpl, err := framework.File("templates/environment")
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
	if err := Run(&out2, p, host, false); err != nil {
		t.Fatalf("second Run: %v\noutput:\n%s", err, out2.String())
	}
	for _, marker := range []string{"-> ./", "Warning:", "scaffolded:", "added:", "created:", "removed ./"} {
		if strings.Contains(out2.String(), marker) {
			t.Errorf("second run reported a heal change (found %q), got:\n%s", marker, out2.String())
		}
	}
}

func TestRun_ExistingEnvironmentUntouched(t *testing.T) {
	skipIfNoNix(t)

	p, host := fixture(t)
	fakeNix(t)
	envPath := filepath.Join(p.Config, "environment")
	write(t, envPath, "")

	var out bytes.Buffer
	if err := Run(&out, p, host, false); err != nil {
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
	fakeNix(t)

	// No pre-written boot.nix this time: fixture wrote one, so remove it and
	// fake an EFI sysfs with vfat mounted at /boot.
	bootConfig := filepath.Join(hardwareDir(t, p), "boot.nix")
	if err := os.Remove(bootConfig); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(p.Sys, "firmware", "efi", "efivars"), 0755); err != nil {
		t.Fatal(err)
	}
	write(t, p.Mounts, "/dev/sda1 /boot vfat rw 0 0\n")

	var out bytes.Buffer
	if err := Run(&out, p, host, false); err != nil {
		t.Fatalf("Run: %v\noutput:\n%s", err, out.String())
	}

	want, err := computer.RenderBoot(computer.Loader{EFI: true, Target: "/boot"})
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
// matching internal/computer's own test fixture shape.
func writeGPUDevice(t *testing.T, sysDir, addr, class, vendor string) {
	t.Helper()
	dir := filepath.Join(sysDir, "bus", "pci", "devices", addr)
	mustMkdirAll(t, dir)
	write(t, filepath.Join(dir, "class"), class+"\n")
	write(t, filepath.Join(dir, "vendor"), vendor+"\n")
	write(t, filepath.Join(dir, "device"), "0x1234\n")
}

func TestRun_GPUsDetectedAndStaged(t *testing.T) {
	skipIfNoNix(t)

	p, host := fixture(t)
	fakeNix(t)

	// A single NVIDIA GPU in the fake sysfs.
	writeGPUDevice(t, p.Sys, "0000:01:00.0", "0x030200", "0x10de")

	var out bytes.Buffer
	if err := Run(&out, p, host, false); err != nil {
		t.Fatalf("Run: %v\noutput:\n%s", err, out.String())
	}

	gpuFile := filepath.Join(p.Staging, "framework", "gpu.nix")
	got := mustReadFile(t, gpuFile)

	want, err := computer.RenderGPUs([]computer.GPU{
		{BusID: "PCI:1@0:0:0", Vendor: "nvidia", VendorID: "0x10de", Class: "3d", BootVGA: false},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Errorf("staged framework/gpu.nix = %q, want %q", got, want)
	}

	if !strings.Contains(out.String(), "Detecting GPUs...") {
		t.Errorf("output did not report GPU detection, got:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "nvidia 3d: PCI:1@0:0:0") {
		t.Errorf("output did not report the detected GPU, got:\n%s", out.String())
	}
}

func TestRun_LocalModuleSelected(t *testing.T) {
	skipIfNoNix(t)

	root := t.TempDir()
	config := filepath.Join(root, "config")
	local := filepath.Join(config, ".local", "machines")
	modules := filepath.Join(config, "modules")
	staging := filepath.Join(root, "etc-nixos")
	sysDir := filepath.Join(root, "sys")
	mountsFile := filepath.Join(root, "mounts")

	// A module private to host1, selected through the "local/" prefix.
	write(t, filepath.Join(local, "host1", "modules", "foo.nix"), "{ }\n")
	write(t, filepath.Join(local, "host1", "modules.nix"),
		"{ ... }:\n{\n  imports = [\n    ./local/foo.nix\n  ];\n}\n")
	write(t, filepath.Join(local, "host1", ".plsdonttouch.nix"),
		"{ system.stateVersion = \"25.11\"; }\n")
	write(t, filepath.Join(local, "host1", "machine.nix"),
		"{ time.timeZone = \"UTC\"; i18n.defaultLocale = \"en_US.UTF-8\"; }\n")

	hardwareRoot := filepath.Join(config, ".local", "hardware")
	write(t, filepath.Join(sysDir, "class", "dmi", "id", "product_uuid"), fixtureUUID+"\n")
	key, err := computer.HardwareKey(sysDir)
	if err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(hardwareRoot, key, config_.HardwareConfigFile), hardwareConfigContent)
	write(t, filepath.Join(hardwareRoot, key, config_.BootFile), "{ ... }: { }\n")
	write(t, mountsFile, "")

	p := paths.Paths{
		Home:           root,
		User:           "test",
		Config:         config,
		Machines:       local,
		Modules:        modules,
		Staging:        staging,
		Backup:         filepath.Join(root, "backup"),
		HardwareRoot:   hardwareRoot,
		Sys:            sysDir,
		Mounts:         mountsFile,
		RunningModules: filepath.Join(root, "run-modules.nix"),
	}

	fakeNix(t)

	var out bytes.Buffer
	if err := Run(&out, p, "host1", false); err != nil {
		t.Fatalf("Run: %v\noutput:\n%s", err, out.String())
	}

	link := filepath.Join(modules, "local")
	info, err := os.Lstat(link)
	if err != nil {
		t.Fatalf("Lstat %s: %v", link, err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("%s is not a symlink", link)
	}

	stagedFoo := filepath.Join(staging, "config", "modules", "local", "foo.nix")
	if _, err := os.Stat(stagedFoo); err != nil {
		t.Errorf("expected %s to exist: %v", stagedFoo, err)
	}

	flakeFile := mustReadFile(t, filepath.Join(staging, "framework", "flake-file.nix"))
	for _, want := range []string{"import ./overlay.nix", "nixosConfigurations.host1"} {
		if !strings.Contains(flakeFile, want) {
			t.Errorf("flake-file.nix missing %q, got:\n%s", want, flakeFile)
		}
	}

	// No lock existed for the host: the fake lock step created one, so the
	// rebuild copies it back and says so.
	if !strings.Contains(out.String(), "locked new inputs:") {
		t.Errorf("output did not report the new lock, got:\n%s", out.String())
	}
	if _, err := os.Stat(filepath.Join(local, "host1", "flake.lock")); err != nil {
		t.Errorf("lock was not copied back to the host folder: %v", err)
	}
}

func TestRun_StrayHostFileFails(t *testing.T) {
	skipIfNoNix(t)

	p, host := fixture(t)
	fakeNix(t)

	stray := filepath.Join(p.Machines, host, "hardware.nix")
	write(t, stray, "{ }\n")

	var out bytes.Buffer
	err := Run(&out, p, host, false)
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
	fakeNix(t)

	// Broken, but no host imports it: it is never built, so never checked.
	write(t, filepath.Join(p.Modules, "unused.nix"), "{ imports = [ ../outside.nix ]; }\n")

	var out bytes.Buffer
	if err := Run(&out, p, host, false); err != nil {
		t.Fatalf("Run: %v\noutput:\n%s", err, out.String())
	}
}

func TestRun_BoundaryViolation(t *testing.T) {
	skipIfNoNix(t)

	p, host := fixture(t)
	fakeNix(t)

	// A module referencing a path outside its own module - a boundary
	// violation refs.Validate must catch before anything is staged.
	write(t, filepath.Join(p.Modules, "bad.nix"), "{ imports = [ ../outside.nix ]; }\n")
	write(t, filepath.Join(p.Machines, host, "modules.nix"),
		"{ ... }:\n{\n  imports = [\n    ./misc/foo.nix\n    ./bad.nix\n  ];\n}\n")

	var out bytes.Buffer
	err := Run(&out, p, host, false)
	if err == nil {
		t.Fatal("expected an error for the boundary violation, got nil")
	}
	if !strings.Contains(err.Error(), "module boundary violations") {
		t.Errorf("error = %q, want mention of module boundary violations", err.Error())
	}
	if !strings.Contains(out.String(), "Error: ") {
		t.Errorf("output did not report the violation, got:\n%s", out.String())
	}

	if _, statErr := os.Stat(filepath.Join(p.Staging, "framework")); !os.IsNotExist(statErr) {
		t.Errorf("nothing should be staged after a boundary violation, err=%v", statErr)
	}
}

func TestRun_StrangerMovedToBackup(t *testing.T) {
	skipIfNoNix(t)

	p, host := fixture(t)
	fakeNix(t)
	write(t, filepath.Join(p.Staging, "old-config.nix"), "{ }\n")

	var out bytes.Buffer
	if err := Run(&out, p, host, false); err != nil {
		t.Fatalf("Run: %v\noutput:\n%s", err, out.String())
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

	// The hardware folder is not in /etc/nixos, so a second run leaves it be.
	if err := Run(&out, p, host, false); err != nil {
		t.Fatalf("second Run: %v", err)
	}
	if got := mustReadFile(t, filepath.Join(hardwareDir(t, p), "hardware-configuration.nix")); got != hardwareConfigContent {
		t.Errorf("hardware-configuration.nix = %q after second run, want %q", got, hardwareConfigContent)
	}
}

func TestRun_NoBackupDirNamesFlag(t *testing.T) {
	skipIfNoNix(t)

	p, host := fixture(t)
	fakeNix(t)
	p.Backup = ""
	write(t, filepath.Join(p.Staging, "old-config.nix"), "{ }\n")

	var out bytes.Buffer
	err := Run(&out, p, host, false)
	if err == nil || !strings.Contains(err.Error(), "luxos rebuild --backup-dir <path>") {
		t.Fatalf("err = %v, want the --backup-dir recovery line", err)
	}
}

func TestEnsureMachineFile_CreatesFromTemplate(t *testing.T) {
	hostDir := t.TempDir()
	var out bytes.Buffer
	if err := ensureMachineFile(&out, hostDir); err != nil {
		t.Fatal(err)
	}
	want, err := framework.File(machineTemplate)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(hostDir, "machine.nix")
	if got := mustReadFile(t, path); got != string(want) {
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
	write(t, path, own)
	var out bytes.Buffer
	if err := ensureMachineFile(&out, hostDir); err != nil {
		t.Fatal(err)
	}
	if got := mustReadFile(t, path); got != own {
		t.Errorf("machine.nix changed: %q", got)
	}
	if out.Len() != 0 {
		t.Errorf("unexpected output %q", out.String())
	}
}

func TestMachineTemplate_Parses(t *testing.T) {
	skipIfNoNix(t)
	tmpl, err := framework.File(machineTemplate)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "machine.nix")
	write(t, path, string(tmpl))
	if out, err := exec.Command("nix-instantiate", "--parse", path).CombinedOutput(); err != nil {
		t.Fatalf("template does not parse: %v\n%s", err, out)
	}
}

func TestSystemNix_RetentionInMachineTemplate(t *testing.T) {
	sys, err := framework.File("system.nix")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(sys), "nix.gc") {
		t.Error("system.nix must not define nix.gc")
	}
	for _, want := range []string{
		"nix.optimise.automatic = lib.mkDefault true;",
		"nix.settings.auto-optimise-store = lib.mkDefault true;",
	} {
		if !strings.Contains(string(sys), want) {
			t.Errorf("system.nix missing %q", want)
		}
	}
}

func TestSystemNix_ImportsNoHardwareFiles(t *testing.T) {
	sys, err := framework.File("system.nix")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(sys), "config/local/hardware-support") {
		t.Error("system.nix must not import hardware files")
	}
	if strings.Contains(string(sys), "RuntimeWatchdogSec") {
		t.Error("system.nix must not set RuntimeWatchdogSec")
	}
}

func TestMachineTemplate_NoGC(t *testing.T) {
	tmpl, err := framework.File(machineTemplate)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(tmpl), "nix.gc") {
		t.Error("machine template must not define nix.gc")
	}
}

func TestEnsureHardware_DefaultCreatedOnce(t *testing.T) {
	skipIfNoNix(t)
	p, _ := fixture(t)
	fakeNix(t)
	var out bytes.Buffer
	hw, err := ensureHardware(&out, p)
	if err != nil {
		t.Fatal(err)
	}
	def := filepath.Join(hw, config_.DefaultFile)
	tmpl, err := framework.File(hardwareDefaultTemplate)
	if err != nil {
		t.Fatal(err)
	}
	if got := mustReadFile(t, def); got != string(tmpl) {
		t.Errorf("default.nix differs from the template:\n%s", got)
	}
	if !strings.Contains(out.String(), "created: "+def) {
		t.Errorf("output does not report creating %s:\n%s", def, out.String())
	}

	write(t, def, "{ }\n")
	out.Reset()
	if _, err := ensureHardware(&out, p); err != nil {
		t.Fatal(err)
	}
	if got := mustReadFile(t, def); got != "{ }\n" {
		t.Errorf("edited default.nix was rewritten: %q", got)
	}
	if strings.Contains(out.String(), "created: "+def) {
		t.Errorf("second run reported creating default.nix:\n%s", out.String())
	}
}

func TestHardwareTemplate_Parses(t *testing.T) {
	skipIfNoNix(t)
	tmpl, err := framework.File(hardwareDefaultTemplate)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "default.nix")
	write(t, path, string(tmpl))
	if out, err := exec.Command("nix-instantiate", "--parse", path).CombinedOutput(); err != nil {
		t.Fatalf("template does not parse: %v\n%s", err, out)
	}
}

func TestRun_MissingProductUUIDFails(t *testing.T) {
	skipIfNoNix(t)
	p, host := fixture(t)
	fakeNix(t)
	if err := os.Remove(filepath.Join(p.Sys, "class", "dmi", "id", "product_uuid")); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := Run(&out, p, host, false); err == nil || !strings.Contains(err.Error(), "product_uuid") {
		t.Fatalf("err = %v, want the product_uuid error", err)
	}
}

func TestCheckMachineFile(t *testing.T) {
	skipIfNoNix(t)
	tmpl, err := framework.File(machineTemplate)
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
			write(t, path, c.content)
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

func TestRun_MachineFilePathStopsBeforeStaging(t *testing.T) {
	skipIfNoNix(t)
	p, host := fixture(t)
	fakeNix(t)
	write(t, filepath.Join(p.Machines, host, "machine.nix"), "{ imports = [ ./x.nix ]; }\n")

	var out bytes.Buffer
	if err := Run(&out, p, host, false); err == nil || !strings.Contains(err.Error(), "machine.nix") {
		t.Fatalf("err = %v, want machine.nix error", err)
	}
	if _, statErr := os.Stat(filepath.Join(p.Staging, "framework")); !os.IsNotExist(statErr) {
		t.Errorf("nothing should be staged, err=%v", statErr)
	}
}
