package hardware

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func writeProfileChoices(t *testing.T, sysDir, content string) {
	t.Helper()
	path := filepath.Join(sysDir, platformProfileChoicesFile)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestDetectPlatformProfiles_Reads(t *testing.T) {
	sysDir := t.TempDir()
	writeProfileChoices(t, sysDir, "cool quiet balanced performance\n")
	got, err := DetectPlatformProfiles(sysDir)
	if err != nil {
		t.Fatalf("DetectPlatformProfiles: %v", err)
	}
	want := []string{"cool", "quiet", "balanced", "performance"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("DetectPlatformProfiles = %v, want %v", got, want)
	}
}

func TestDetectPlatformProfiles_MissingFileIsEmpty(t *testing.T) {
	got, err := DetectPlatformProfiles(t.TempDir())
	if err != nil || len(got) != 0 {
		t.Fatalf("got %v, %v; want no profiles and no error", got, err)
	}
}
