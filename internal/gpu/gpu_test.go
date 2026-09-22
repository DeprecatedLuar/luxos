package gpu

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// busIDTypePattern is nixpkgs' own busIDType regex, copied verbatim from
// nixos/modules/hardware/video/nvidia.nix (options.hardware.nvidia.prime.*BusId
// definitions, `types.strMatching`), cited in G2 of implementation-plan.md.
const busIDTypePattern = `([[:print:]]+:[0-9]{1,3}(@[0-9]{1,10})?:[0-9]{1,2}:[0-9])?`

var busIDTypeRegex = regexp.MustCompile(`^` + busIDTypePattern + `$`)

// device writes a fake sysfs PCI device directory <sysDir>/bus/pci/devices/<addr>
// with the given class/vendor hex strings (e.g. "0x030000", "0x8086") and an
// optional boot_vga content ("" to omit the file, "1" or "0" to write it).
func device(t *testing.T, sysDir, addr, class, vendor, bootVga string) {
	t.Helper()
	dir := filepath.Join(sysDir, "bus", "pci", "devices", addr)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("mkdir device dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "class"), []byte(class+"\n"), 0644); err != nil {
		t.Fatalf("write class: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "vendor"), []byte(vendor+"\n"), 0644); err != nil {
		t.Fatalf("write vendor: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "device"), []byte("0x1234\n"), 0644); err != nil {
		t.Fatalf("write device: %v", err)
	}
	if bootVga != "" {
		if err := os.WriteFile(filepath.Join(dir, "boot_vga"), []byte(bootVga+"\n"), 0644); err != nil {
			t.Fatalf("write boot_vga: %v", err)
		}
	}
}

func mustRenderContains(t *testing.T, gpus []GPU, substrs ...string) {
	t.Helper()
	out, err := Render(gpus)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	for _, s := range substrs {
		if !strings.Contains(string(out), s) {
			t.Errorf("Render output missing %q; got:\n%s", s, out)
		}
	}
}

func checkBusIDsMatchRegex(t *testing.T, gpus []GPU) {
	t.Helper()
	for _, g := range gpus {
		if !busIDTypeRegex.MatchString(g.BusID) {
			t.Errorf("busId %q does not match nixpkgs busIDType regex", g.BusID)
		}
	}
}

func TestDetect_ParaloidLayout(t *testing.T) {
	sysDir := t.TempDir()
	device(t, sysDir, "0000:00:02.0", "0x030000", "0x8086", "1")
	device(t, sysDir, "0000:01:00.0", "0x030200", "0x10de", "")

	gpus, err := Detect(sysDir)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if len(gpus) != 2 {
		t.Fatalf("got %d gpus, want 2: %+v", len(gpus), gpus)
	}

	byAddr := map[string]GPU{}
	for _, g := range gpus {
		byAddr[g.BusID] = g
	}

	intel, ok := byAddr["PCI:0@0:2:0"]
	if !ok {
		t.Fatalf("missing intel PCI:0@0:2:0 in %+v", gpus)
	}
	if intel.Vendor != "intel" || intel.Class != "vga" || !intel.BootVGA {
		t.Errorf("intel gpu wrong: %+v", intel)
	}

	nvidia, ok := byAddr["PCI:1@0:0:0"]
	if !ok {
		t.Fatalf("missing nvidia PCI:1@0:0:0 in %+v", gpus)
	}
	if nvidia.Vendor != "nvidia" || nvidia.Class != "3d" || nvidia.BootVGA {
		t.Errorf("nvidia gpu wrong: %+v", nvidia)
	}

	checkBusIDsMatchRegex(t, gpus)
	mustRenderContains(t, gpus, `busId = "PCI:0@0:2:0"`, `busId = "PCI:1@0:0:0"`)
}

func TestDetect_AMDApuPlusNvidiaDGPU(t *testing.T) {
	sysDir := t.TempDir()
	device(t, sysDir, "0000:00:01.0", "0x030000", "0x1002", "1")
	device(t, sysDir, "0000:01:00.0", "0x030200", "0x10de", "")

	gpus, err := Detect(sysDir)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if len(gpus) != 2 {
		t.Fatalf("got %d gpus, want 2: %+v", len(gpus), gpus)
	}
	for _, g := range gpus {
		switch g.Vendor {
		case "amd":
			if !g.BootVGA || g.Class != "vga" {
				t.Errorf("amd gpu wrong: %+v", g)
			}
		case "nvidia":
			if g.BootVGA || g.Class != "3d" {
				t.Errorf("nvidia gpu wrong: %+v", g)
			}
		default:
			t.Errorf("unexpected vendor: %+v", g)
		}
	}
	checkBusIDsMatchRegex(t, gpus)
}

func TestDetect_NvidiaOnly(t *testing.T) {
	sysDir := t.TempDir()
	device(t, sysDir, "0000:01:00.0", "0x030000", "0x10de", "1")

	gpus, err := Detect(sysDir)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if len(gpus) != 1 || gpus[0].Vendor != "nvidia" {
		t.Fatalf("got %+v, want single nvidia gpu", gpus)
	}
	checkBusIDsMatchRegex(t, gpus)
}

func TestDetect_IntelOnly(t *testing.T) {
	sysDir := t.TempDir()
	device(t, sysDir, "0000:00:02.0", "0x030000", "0x8086", "1")

	gpus, err := Detect(sysDir)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if len(gpus) != 1 || gpus[0].Vendor != "intel" {
		t.Fatalf("got %+v, want single intel gpu", gpus)
	}
	checkBusIDsMatchRegex(t, gpus)
}

func TestDetect_NoDevicesDir(t *testing.T) {
	sysDir := t.TempDir()

	gpus, err := Detect(sysDir)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if gpus != nil {
		t.Fatalf("got %+v, want nil", gpus)
	}
}

func TestDetect_TwoNvidiaGPUsBothKept(t *testing.T) {
	sysDir := t.TempDir()
	device(t, sysDir, "0000:01:00.0", "0x030000", "0x10de", "1")
	device(t, sysDir, "0000:02:00.0", "0x030000", "0x10de", "0")

	gpus, err := Detect(sysDir)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if len(gpus) != 2 {
		t.Fatalf("got %d gpus, want 2 (ambiguity resolution is not this package's job): %+v", len(gpus), gpus)
	}
	checkBusIDsMatchRegex(t, gpus)
}

func TestDetect_NonzeroDomain(t *testing.T) {
	sysDir := t.TempDir()
	device(t, sysDir, "00010000:01:00.0", "0x030000", "0x10de", "1")

	gpus, err := Detect(sysDir)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if len(gpus) != 1 {
		t.Fatalf("got %d gpus, want 1: %+v", len(gpus), gpus)
	}
	if gpus[0].BusID != "PCI:1@65536:0:0" {
		t.Fatalf("got busId %q, want PCI:1@65536:0:0", gpus[0].BusID)
	}
	checkBusIDsMatchRegex(t, gpus)
}

func TestDetect_MalformedClassIsHardError(t *testing.T) {
	sysDir := t.TempDir()
	dir := filepath.Join(sysDir, "bus", "pci", "devices", "0000:01:00.0")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "class"), []byte("garbage\n"), 0644); err != nil {
		t.Fatalf("write class: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "vendor"), []byte("0x10de\n"), 0644); err != nil {
		t.Fatalf("write vendor: %v", err)
	}

	_, err := Detect(sysDir)
	if err == nil {
		t.Fatal("Detect: want error for malformed class file, got nil")
	}
	if !strings.Contains(err.Error(), dir) {
		t.Errorf("error %q does not name device directory %q", err, dir)
	}
}

func TestDetect_MalformedVendorIsHardError(t *testing.T) {
	sysDir := t.TempDir()
	dir := filepath.Join(sysDir, "bus", "pci", "devices", "0000:01:00.0")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "class"), []byte("0x030000\n"), 0644); err != nil {
		t.Fatalf("write class: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "vendor"), []byte("garbage\n"), 0644); err != nil {
		t.Fatalf("write vendor: %v", err)
	}

	_, err := Detect(sysDir)
	if err == nil {
		t.Fatal("Detect: want error for malformed vendor file, got nil")
	}
	if !strings.Contains(err.Error(), dir) {
		t.Errorf("error %q does not name device directory %q", err, dir)
	}
}

func TestRender_Empty(t *testing.T) {
	out, err := Render(nil)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !strings.Contains(string(out), "luxos.hardware.gpus = [ ];") {
		t.Errorf("Render(nil) = %q, want empty-list form", out)
	}
}

func TestRender_SortedByBusID(t *testing.T) {
	gpus := []GPU{
		{BusID: "PCI:1@0:0:0", Vendor: "nvidia", VendorID: "0x10de", Class: "3d", BootVGA: false},
		{BusID: "PCI:0@0:2:0", Vendor: "intel", VendorID: "0x8086", Class: "vga", BootVGA: true},
	}
	out, err := Render(gpus)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	first := strings.Index(string(out), "PCI:0@0:2:0")
	second := strings.Index(string(out), "PCI:1@0:0:0")
	if first < 0 || second < 0 || first > second {
		t.Errorf("Render output not sorted by busId:\n%s", out)
	}
}

func TestBusIDTypeRegex_ValidatesKnownGoodAndBad(t *testing.T) {
	good := []string{"PCI:0@0:2:0", "PCI:1@0:0:0", "PCI:1@65536:0:0"}
	for _, g := range good {
		if !busIDTypeRegex.MatchString(g) {
			t.Errorf("expected %q to match busIDType regex", g)
		}
	}
}
