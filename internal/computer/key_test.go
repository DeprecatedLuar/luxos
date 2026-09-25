package computer

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const fixtureUUID = "4c4c4544-0042-5110-8042-cac04f375433"

func writeUUID(t *testing.T, content string) string {
	t.Helper()
	sys := t.TempDir()
	dir := filepath.Join(sys, "class", "dmi", "id")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "product_uuid"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return sys
}

func TestHardwareKey(t *testing.T) {
	sum := sha256.Sum256([]byte("luxos-hardware:" + fixtureUUID))
	want := hex.EncodeToString(sum[:])[:16]

	got, err := HardwareKey(writeUUID(t, "  "+strings.ToUpper(fixtureUUID)+"\n"))
	if err != nil {
		t.Fatal(err)
	}
	if got != want || len(got) != 16 {
		t.Fatalf("key = %q, want %q", got, want)
	}
	if strings.Contains(got, "4c4c") && strings.Contains(fixtureUUID, got) {
		t.Fatal("key contains the raw UUID")
	}
}

func TestHardwareKey_Missing(t *testing.T) {
	if _, err := HardwareKey(t.TempDir()); err == nil || !strings.Contains(err.Error(), "cannot read") {
		t.Fatalf("err = %v", err)
	}
}

func TestHardwareKey_Empty(t *testing.T) {
	if _, err := HardwareKey(writeUUID(t, "\n")); err == nil {
		t.Fatal("expected error")
	}
}
