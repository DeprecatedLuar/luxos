package boot

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DeprecatedLuar/luxos/internal/nix"
)

func skipIfNoNix(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("nix-instantiate"); err != nil {
		t.Skip("nix-instantiate not on PATH")
	}
}

// writeMounts writes lines (already whitespace-joined "source mountpoint
// fstype") to a mounts file under dir and returns its path.
func writeMounts(t *testing.T, dir string, lines ...string) string {
	t.Helper()
	path := filepath.Join(dir, "mounts")
	content := ""
	for _, l := range lines {
		content += l + "\n"
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write mounts file: %v", err)
	}
	return path
}

// efiSys creates <sys>/firmware/efi/efivars so Detect sees EFI firmware.
func efiSys(t *testing.T, sysDir string) {
	t.Helper()
	dir := filepath.Join(sysDir, "firmware", "efi")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("mkdir efi dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "efivars"), nil, 0644); err != nil {
		t.Fatalf("write efivars: %v", err)
	}
}

// wholeDisk creates <sys>/class/block/<name> as a plain directory (no
// partition file), optionally with a non-empty slaves/ subdirectory.
func wholeDisk(t *testing.T, sysDir, name string, withSlave bool) {
	t.Helper()
	dir := filepath.Join(sysDir, "class", "block", name)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("mkdir class/block/%s: %v", name, err)
	}
	if withSlave {
		slaves := filepath.Join(dir, "slaves")
		if err := os.MkdirAll(slaves, 0755); err != nil {
			t.Fatalf("mkdir slaves: %v", err)
		}
		if err := os.WriteFile(filepath.Join(slaves, "dm-1"), nil, 0644); err != nil {
			t.Fatalf("write slave entry: %v", err)
		}
	}
}

// partition creates <sys>/devices/x/<disk>/<part>/partition and symlinks
// <sys>/class/block/<part> to that directory, as the plan's test fixture
// instructions describe.
func partition(t *testing.T, sysDir, disk, part string) {
	t.Helper()
	realDir := filepath.Join(sysDir, "devices", "x", disk, part)
	if err := os.MkdirAll(realDir, 0755); err != nil {
		t.Fatalf("mkdir device dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(realDir, "partition"), nil, 0644); err != nil {
		t.Fatalf("write partition file: %v", err)
	}

	classBlockDir := filepath.Join(sysDir, "class", "block")
	if err := os.MkdirAll(classBlockDir, 0755); err != nil {
		t.Fatalf("mkdir class/block: %v", err)
	}
	link := filepath.Join(classBlockDir, part)
	if err := os.Symlink(realDir, link); err != nil {
		t.Fatalf("symlink class/block/%s: %v", part, err)
	}
}

func TestDetect_EFI_VfatAtBoot(t *testing.T) {
	sysDir := t.TempDir()
	efiSys(t, sysDir)
	mountsFile := writeMounts(t, t.TempDir(), "/dev/sda1 /boot vfat rw 0 0")

	got, err := Detect(sysDir, mountsFile)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	want := Loader{EFI: true, Target: "/boot"}
	if got != want {
		t.Errorf("Detect = %+v, want %+v", got, want)
	}
}

func TestDetect_EFI_PrefersEfiMountPoint(t *testing.T) {
	sysDir := t.TempDir()
	efiSys(t, sysDir)
	mountsFile := writeMounts(t, t.TempDir(),
		"/dev/sda2 /boot vfat rw 0 0",
		"/dev/sda1 /boot/efi vfat rw 0 0",
	)

	got, err := Detect(sysDir, mountsFile)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	want := Loader{EFI: true, Target: "/boot/efi"}
	if got != want {
		t.Errorf("Detect = %+v, want %+v", got, want)
	}
}

func TestDetect_EFI_NoVfatErrors(t *testing.T) {
	sysDir := t.TempDir()
	efiSys(t, sysDir)
	mountsFile := writeMounts(t, t.TempDir(), "/dev/sda1 /boot ext4 rw 0 0")

	if _, err := Detect(sysDir, mountsFile); err == nil {
		t.Fatalf("Detect: want error, got nil")
	}
}

func TestDetect_BIOS_PartitionOfRoot(t *testing.T) {
	sysDir := t.TempDir()
	partition(t, sysDir, "sda", "sda1")
	mountsFile := writeMounts(t, t.TempDir(), "/dev/sda1 / ext4 rw 0 0")

	got, err := Detect(sysDir, mountsFile)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	want := Loader{EFI: false, Target: "/dev/sda"}
	if got != want {
		t.Errorf("Detect = %+v, want %+v", got, want)
	}
}

func TestDetect_BIOS_NvmePartition(t *testing.T) {
	sysDir := t.TempDir()
	partition(t, sysDir, "nvme0n1", "nvme0n1p2")
	mountsFile := writeMounts(t, t.TempDir(), "/dev/nvme0n1p2 / ext4 rw 0 0")

	got, err := Detect(sysDir, mountsFile)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	want := Loader{EFI: false, Target: "/dev/nvme0n1"}
	if got != want {
		t.Errorf("Detect = %+v, want %+v", got, want)
	}
}

func TestDetect_BIOS_PrefersBootMount(t *testing.T) {
	sysDir := t.TempDir()
	partition(t, sysDir, "sdb", "sdb1")
	partition(t, sysDir, "sda", "sda1")
	mountsFile := writeMounts(t, t.TempDir(),
		"/dev/sda1 / ext4 rw 0 0",
		"/dev/sdb1 /boot ext4 rw 0 0",
	)

	got, err := Detect(sysDir, mountsFile)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	want := Loader{EFI: false, Target: "/dev/sdb"}
	if got != want {
		t.Errorf("Detect = %+v, want %+v", got, want)
	}
}

func TestDetect_BIOS_WholeDiskNoPartitionFile(t *testing.T) {
	sysDir := t.TempDir()
	wholeDisk(t, sysDir, "vda", false)
	mountsFile := writeMounts(t, t.TempDir(), "/dev/vda / ext4 rw 0 0")

	got, err := Detect(sysDir, mountsFile)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	want := Loader{EFI: false, Target: "/dev/vda"}
	if got != want {
		t.Errorf("Detect = %+v, want %+v", got, want)
	}
}

func TestDetect_BIOS_SlavesErrors(t *testing.T) {
	sysDir := t.TempDir()
	wholeDisk(t, sysDir, "dm-0", true)
	mountsFile := writeMounts(t, t.TempDir(), "/dev/dm-0 / ext4 rw 0 0")

	if _, err := Detect(sysDir, mountsFile); err == nil {
		t.Fatalf("Detect: want error, got nil")
	}
}

func TestDetect_BIOS_NonDevSourceErrors(t *testing.T) {
	sysDir := t.TempDir()
	mountsFile := writeMounts(t, t.TempDir(), "tmpfs / tmpfs rw 0 0")

	if _, err := Detect(sysDir, mountsFile); err == nil {
		t.Fatalf("Detect: want error, got nil")
	}
}

func TestDetect_BIOS_MissingClassBlockEntryErrors(t *testing.T) {
	sysDir := t.TempDir()
	mountsFile := writeMounts(t, t.TempDir(), "/dev/sda1 / ext4 rw 0 0")

	if _, err := Detect(sysDir, mountsFile); err == nil {
		t.Fatalf("Detect: want error, got nil")
	}
}

func TestDetect_MountsFile_LastLineForMountPointWins(t *testing.T) {
	sysDir := t.TempDir()
	partition(t, sysDir, "sdb", "sdb1")
	mountsFile := writeMounts(t, t.TempDir(),
		"/dev/sda1 / ext4 rw 0 0",
		"/dev/sdb1 / ext4 rw 0 0",
	)

	got, err := Detect(sysDir, mountsFile)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	want := Loader{EFI: false, Target: "/dev/sdb"}
	if got != want {
		t.Errorf("Detect = %+v, want %+v", got, want)
	}
}

func TestRender_Golden(t *testing.T) {
	efiGolden, err := os.ReadFile(filepath.Join("testdata", "efi.golden"))
	if err != nil {
		t.Fatalf("read efi.golden: %v", err)
	}
	got, err := Render(Loader{EFI: true, Target: "/boot"})
	if err != nil {
		t.Fatalf("Render EFI: %v", err)
	}
	if string(got) != string(efiGolden) {
		t.Errorf("Render EFI mismatch\n--- got ---\n%s\n--- want ---\n%s", got, efiGolden)
	}

	biosGolden, err := os.ReadFile(filepath.Join("testdata", "bios.golden"))
	if err != nil {
		t.Fatalf("read bios.golden: %v", err)
	}
	got, err = Render(Loader{EFI: false, Target: "/dev/sda"})
	if err != nil {
		t.Fatalf("Render BIOS: %v", err)
	}
	if string(got) != string(biosGolden) {
		t.Errorf("Render BIOS mismatch\n--- got ---\n%s\n--- want ---\n%s", got, biosGolden)
	}
}

func TestRender_ParsesAsNix(t *testing.T) {
	skipIfNoNix(t)

	dir := t.TempDir()

	efiContent, err := Render(Loader{EFI: true, Target: "/boot"})
	if err != nil {
		t.Fatalf("Render EFI: %v", err)
	}
	efiFile := filepath.Join(dir, "efi.nix")
	if err := os.WriteFile(efiFile, efiContent, 0644); err != nil {
		t.Fatalf("write efi.nix: %v", err)
	}
	if _, err := nix.Parse(efiFile); err != nil {
		t.Errorf("nix-instantiate --parse efi.nix: %v", err)
	}

	biosContent, err := Render(Loader{EFI: false, Target: "/dev/sda"})
	if err != nil {
		t.Fatalf("Render BIOS: %v", err)
	}
	biosFile := filepath.Join(dir, "bios.nix")
	if err := os.WriteFile(biosFile, biosContent, 0644); err != nil {
		t.Fatalf("write bios.nix: %v", err)
	}
	if _, err := nix.Parse(biosFile); err != nil {
		t.Errorf("nix-instantiate --parse bios.nix: %v", err)
	}
}

func TestEnsure_ExistingFileUntouched(t *testing.T) {
	dir := t.TempDir()
	bootFile := filepath.Join(dir, "boot.nix")
	original := []byte("{ ... }: { }\n")
	if err := os.WriteFile(bootFile, original, 0600); err != nil {
		t.Fatalf("write existing boot.nix: %v", err)
	}

	created, err := Ensure(bootFile, filepath.Join(dir, "sys"), filepath.Join(dir, "mounts"))
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	if created {
		t.Errorf("Ensure: created = true, want false")
	}

	got, err := os.ReadFile(bootFile)
	if err != nil {
		t.Fatalf("read boot.nix: %v", err)
	}
	if string(got) != string(original) {
		t.Errorf("Ensure changed content: got %q, want %q", got, original)
	}
	info, err := os.Stat(bootFile)
	if err != nil {
		t.Fatalf("stat boot.nix: %v", err)
	}
	if info.Mode().Perm() != 0600 {
		t.Errorf("Ensure changed mode: got %v, want 0600", info.Mode().Perm())
	}
}

func TestEnsure_MissingFileWritesDetected(t *testing.T) {
	dir := t.TempDir()
	sysDir := filepath.Join(dir, "sys")
	efiSys(t, sysDir)
	mountsFile := writeMounts(t, dir, "/dev/sda1 /boot vfat rw 0 0")
	bootFile := filepath.Join(dir, "boot.nix")

	created, err := Ensure(bootFile, sysDir, mountsFile)
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	if !created {
		t.Errorf("Ensure: created = false, want true")
	}

	want, err := Render(Loader{EFI: true, Target: "/boot"})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	got, err := os.ReadFile(bootFile)
	if err != nil {
		t.Fatalf("read boot.nix: %v", err)
	}
	if string(got) != string(want) {
		t.Errorf("Ensure content mismatch\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}

	info, err := os.Stat(bootFile)
	if err != nil {
		t.Fatalf("stat boot.nix: %v", err)
	}
	if info.Mode().Perm() != fileMode {
		t.Errorf("Ensure mode = %v, want %v", info.Mode().Perm(), fileMode)
	}
}

func TestEnsure_UndetectableErrorsMentionsFile(t *testing.T) {
	dir := t.TempDir()
	sysDir := filepath.Join(dir, "sys")
	// No EFI firmware and no mounts at all: BIOS detection also fails.
	mountsFile := writeMounts(t, dir)
	bootFile := filepath.Join(dir, "boot.nix")

	created, err := Ensure(bootFile, sysDir, mountsFile)
	if err == nil {
		t.Fatalf("Ensure: want error, got nil")
	}
	if created {
		t.Errorf("Ensure: created = true, want false")
	}
	if !strings.Contains(err.Error(), bootFile) {
		t.Errorf("Ensure error %q does not mention %q", err.Error(), bootFile)
	}
	if _, statErr := os.Lstat(bootFile); !os.IsNotExist(statErr) {
		t.Errorf("Ensure: boot.nix was written despite the error")
	}
}
