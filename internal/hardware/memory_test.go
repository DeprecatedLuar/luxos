package hardware

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeMeminfo(t *testing.T, content string) string {
	t.Helper()
	procDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(procDir, "meminfo"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return procDir
}

func TestDetectMemory_Reads(t *testing.T) {
	procDir := writeMeminfo(t, "MemTotal:        8000000 kB\nMemFree:         1000 kB\n")
	got, err := DetectMemory(procDir)
	if err != nil {
		t.Fatal(err)
	}
	if want := int64(8000000 * 1024); got != want {
		t.Errorf("got %d, want %d", got, want)
	}
}

func TestDetectMemory_NoTotal(t *testing.T) {
	procDir := writeMeminfo(t, "MemFree:         1000 kB\n")
	if _, err := DetectMemory(procDir); err == nil || !strings.Contains(err.Error(), "MemTotal") {
		t.Errorf("err = %v, want one naming MemTotal", err)
	}
}

func TestDetectMemory_NotInteger(t *testing.T) {
	procDir := writeMeminfo(t, "MemTotal:        lots kB\n")
	if _, err := DetectMemory(procDir); err == nil {
		t.Error("want an error")
	}
}

func TestDetectMemory_Missing(t *testing.T) {
	if _, err := DetectMemory(t.TempDir()); err == nil {
		t.Error("want an error")
	}
}
