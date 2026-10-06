package hardware

import (
	"strings"
	"testing"
)

func TestRender_NoGPUs(t *testing.T) {
	out, err := Render(Facts{})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !strings.Contains(string(out), "luxos.hardware.gpus = [ ];") {
		t.Errorf("Render(Facts{}) = %q, want empty-list form", out)
	}
}

func TestRender_GPUsSortedByBusID(t *testing.T) {
	gpus := []GPU{
		{BusID: "PCI:1@0:0:0", Vendor: "nvidia", VendorID: "0x10de", Class: "3d", BootVGA: false},
		{BusID: "PCI:0@0:2:0", Vendor: "intel", VendorID: "0x8086", Class: "vga", BootVGA: true},
	}
	out, err := Render(Facts{GPUs: gpus})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	first := strings.Index(string(out), "PCI:0@0:2:0")
	second := strings.Index(string(out), "PCI:1@0:0:0")
	if first < 0 || second < 0 || first > second {
		t.Errorf("Render output not sorted by busId:\n%s", out)
	}
}

func TestRender_DMI(t *testing.T) {
	out, err := Render(Facts{DMI: DMI{Vendor: "Example Corp.", Product: "Example Laptop 14", ChassisType: 10}})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	want := "{ ... }:\n{\n" +
		"  luxos.hardware.vendor = \"Example Corp.\";\n" +
		"  luxos.hardware.product = \"Example Laptop 14\";\n" +
		"  luxos.hardware.chassisType = 10;\n" +
		"  luxos.hardware.gpus = [ ];\n" +
		"}\n"
	if string(out) != want {
		t.Errorf("Render =\n%s\nwant\n%s", out, want)
	}
}

func TestRender_EscapesStrings(t *testing.T) {
	out, err := Render(Facts{DMI: DMI{Vendor: `a"b\c${d}`, Product: "p", ChassisType: 3}})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !strings.Contains(string(out), `luxos.hardware.vendor = "a\"b\\c\${d}";`) {
		t.Errorf("vendor not escaped as a Nix string:\n%s", out)
	}
}
