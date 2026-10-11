package hardware

import (
	"os"
	"path/filepath"
	"testing"
)

func writeCPUModalias(t *testing.T, sysDir, content string) {
	t.Helper()
	path := filepath.Join(sysDir, cpuModaliasFile)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestDetectCPUVendor(t *testing.T) {
	cases := map[string]struct {
		modalias string
		want     string
	}{
		"intel": {"cpu:type:x86,ven0000fam0006mod008C:feature:,0000,0001\n", "intel"},
		"amd":   {"cpu:type:x86,ven0002fam0019mod0050:feature:,0000,0001\n", "amd"},
		"arm":   {"cpu:type:aarch64:feature:,0000,0001\n", "unknown"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			sysDir := t.TempDir()
			writeCPUModalias(t, sysDir, c.modalias)
			got, err := DetectCPUVendor(sysDir)
			if err != nil {
				t.Fatalf("DetectCPUVendor: %v", err)
			}
			if got != c.want {
				t.Errorf("DetectCPUVendor = %q, want %q", got, c.want)
			}
		})
	}
}

func TestDetectCPUVendor_MissingFileIsUnknown(t *testing.T) {
	got, err := DetectCPUVendor(t.TempDir())
	if err != nil || got != "unknown" {
		t.Fatalf("got %q, %v; want \"unknown\" and no error", got, err)
	}
}
