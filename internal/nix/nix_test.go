package nix

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func skipIfNoNix(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath(instantiateBin); err != nil {
		t.Skip("nix-instantiate not on PATH")
	}
}

func TestParse_RelativePathErrors(t *testing.T) {
	if _, err := Parse("relative/path.nix"); err == nil {
		t.Fatalf("Parse: want error for non-absolute path")
	}
}

func TestParse_ValidFile(t *testing.T) {
	skipIfNoNix(t)
	dir := t.TempDir()
	file := filepath.Join(dir, "ok.nix")
	if err := os.WriteFile(file, []byte("{ a = 1; }\n"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	out, err := Parse(file)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if out == "" {
		t.Errorf("Parse output empty")
	}
}

func TestParse_InvalidFileErrorsWithStderr(t *testing.T) {
	skipIfNoNix(t)
	dir := t.TempDir()
	file := filepath.Join(dir, "bad.nix")
	if err := os.WriteFile(file, []byte("{ a = ;\n"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	_, err := Parse(file)
	if err == nil {
		t.Fatalf("Parse: want error for invalid syntax")
	}
}

func TestFlakeEnv_AddsNixConfig(t *testing.T) {
	got := flakeEnv([]string{"HOME=/root"})
	want := []string{"HOME=/root", "NIX_CONFIG=" + flakeFeatures}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("flakeEnv = %q, want %q", got, want)
	}
}

func TestFlakeEnv_KeepsExistingNixConfig(t *testing.T) {
	got := flakeEnv([]string{"NIX_CONFIG=warn-dirty = false", "HOME=/root"})
	want := []string{"HOME=/root", "NIX_CONFIG=warn-dirty = false\n" + flakeFeatures}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("flakeEnv = %q, want %q", got, want)
	}
}
