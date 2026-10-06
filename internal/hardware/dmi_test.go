package hardware

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeDMI writes the given files under <sysDir>/class/dmi/id.
func writeDMI(t *testing.T, sysDir string, files map[string]string) {
	t.Helper()
	dir := filepath.Join(sysDir, "class", "dmi", "id")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestDetectDMI_Reads(t *testing.T) {
	sysDir := t.TempDir()
	writeDMI(t, sysDir, map[string]string{
		"sys_vendor":   "Example Corp.\n",
		"product_name": "  Example Laptop 14  \n",
		"chassis_type": "10\n",
	})
	got, err := DetectDMI(sysDir)
	if err != nil {
		t.Fatalf("DetectDMI: %v", err)
	}
	want := DMI{Vendor: "Example Corp.", Product: "Example Laptop 14", ChassisType: 10}
	if got != want {
		t.Errorf("DetectDMI = %+v, want %+v", got, want)
	}
}

func TestDetectDMI_MissingFileNamesIt(t *testing.T) {
	sysDir := t.TempDir()
	writeDMI(t, sysDir, map[string]string{"sys_vendor": "x\n", "product_name": "y\n"})
	_, err := DetectDMI(sysDir)
	if err == nil || !strings.Contains(err.Error(), "chassis_type") {
		t.Fatalf("err = %v, want one naming chassis_type", err)
	}
}

func TestDetectDMI_NonIntegerChassis(t *testing.T) {
	sysDir := t.TempDir()
	writeDMI(t, sysDir, map[string]string{"sys_vendor": "x\n", "product_name": "y\n", "chassis_type": "laptop\n"})
	_, err := DetectDMI(sysDir)
	if err == nil || !strings.Contains(err.Error(), "chassis_type") {
		t.Fatalf("err = %v, want one naming chassis_type", err)
	}
}
