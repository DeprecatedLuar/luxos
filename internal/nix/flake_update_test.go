package nix

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeNix puts a nix on PATH that runs script.
func fakeNix(t *testing.T, script string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, flakeBin), []byte("#!/bin/sh\n"+script+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
}

func TestFlakeUpdate_SuccessIsSilent(t *testing.T) {
	fakeNix(t, "echo 'warning: updating lock file' >&2")

	captured, err := os.CreateTemp(t.TempDir(), "stderr")
	if err != nil {
		t.Fatal(err)
	}
	defer captured.Close()
	real := os.Stderr
	os.Stderr = captured
	defer func() { os.Stderr = real }()

	if err := FlakeUpdate(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(captured.Name())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("nix output reached the terminal: %q", got)
	}
}

func TestFlakeUpdate_FailureCarriesStderr(t *testing.T) {
	fakeNix(t, "echo 'error: boom' >&2; exit 1")

	err := FlakeUpdate(t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "error: boom") {
		t.Errorf("error = %v, want nix's stderr in it", err)
	}
}
