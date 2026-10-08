package staging

import (
	"os"
	"path/filepath"
	"testing"
)

// promoteDirs returns staging, new and old paths, siblings under one temp dir.
func promoteDirs(t *testing.T) (stagingDir, newDir, oldDir string) {
	t.Helper()
	root := t.TempDir()
	return filepath.Join(root, "nixos"), filepath.Join(root, ".nixos-luxos-new"), filepath.Join(root, ".nixos-luxos-old")
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func mustRead(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func assertGone(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Errorf("%s still exists (err %v)", path, err)
	}
}

func TestRecover_RestoresInterruptedPromotion(t *testing.T) {
	stagingDir, newDir, oldDir := promoteDirs(t)
	mustWrite(t, filepath.Join(oldDir, "flake.nix"), "old")
	mustWrite(t, filepath.Join(newDir, "flake.nix"), "new")

	if err := Recover(stagingDir, newDir, oldDir); err != nil {
		t.Fatal(err)
	}
	if got := mustRead(t, filepath.Join(stagingDir, "flake.nix")); got != "old" {
		t.Errorf("flake.nix = %q, want the restored %q", got, "old")
	}
	assertGone(t, oldDir)
	assertGone(t, newDir)
}

func TestRecover_RemovesLeftovers(t *testing.T) {
	stagingDir, newDir, oldDir := promoteDirs(t)
	mustWrite(t, filepath.Join(stagingDir, "flake.nix"), "current")
	mustWrite(t, filepath.Join(oldDir, "flake.nix"), "old")
	mustWrite(t, filepath.Join(newDir, "flake.nix"), "new")
	// A sealed file, as Seal leaves them.
	if err := os.Chmod(filepath.Join(newDir, "flake.nix"), 0444); err != nil {
		t.Fatal(err)
	}

	if err := Recover(stagingDir, newDir, oldDir); err != nil {
		t.Fatal(err)
	}
	if got := mustRead(t, filepath.Join(stagingDir, "flake.nix")); got != "current" {
		t.Errorf("flake.nix = %q, want %q untouched", got, "current")
	}
	assertGone(t, oldDir)
	assertGone(t, newDir)
}

func TestRecover_NothingToDo(t *testing.T) {
	stagingDir, newDir, oldDir := promoteDirs(t)
	if err := Recover(stagingDir, newDir, oldDir); err != nil {
		t.Fatalf("all missing: %v", err)
	}
	assertGone(t, stagingDir)

	mustWrite(t, filepath.Join(stagingDir, "flake.nix"), "current")
	if err := Recover(stagingDir, newDir, oldDir); err != nil {
		t.Fatal(err)
	}
	if got := mustRead(t, filepath.Join(stagingDir, "flake.nix")); got != "current" {
		t.Errorf("flake.nix = %q, want %q", got, "current")
	}
}

func TestPromote(t *testing.T) {
	stagingDir, newDir, oldDir := promoteDirs(t)
	mustWrite(t, filepath.Join(stagingDir, "flake.nix"), "old")
	mustWrite(t, filepath.Join(stagingDir, "config", "gone.nix"), "x")
	mustWrite(t, filepath.Join(newDir, "flake.nix"), "new")

	if err := Promote(newDir, stagingDir, oldDir); err != nil {
		t.Fatal(err)
	}
	if got := mustRead(t, filepath.Join(stagingDir, "flake.nix")); got != "new" {
		t.Errorf("flake.nix = %q, want %q", got, "new")
	}
	assertGone(t, filepath.Join(stagingDir, "config", "gone.nix"))
	assertGone(t, newDir)
	assertGone(t, oldDir)
}

func TestPromote_MissingStaging(t *testing.T) {
	stagingDir, newDir, oldDir := promoteDirs(t)
	mustWrite(t, filepath.Join(newDir, "flake.nix"), "new")

	if err := Promote(newDir, stagingDir, oldDir); err != nil {
		t.Fatal(err)
	}
	if got := mustRead(t, filepath.Join(stagingDir, "flake.nix")); got != "new" {
		t.Errorf("flake.nix = %q, want %q", got, "new")
	}
	assertGone(t, newDir)
	assertGone(t, oldDir)
}
