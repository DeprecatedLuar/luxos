package staging

import (
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestReplace(t *testing.T) {
	goodbye, stg := t.TempDir(), t.TempDir()
	write(t, filepath.Join(goodbye, "configuration.nix"), "user config")
	write(t, filepath.Join(goodbye, "sub", "deep", "x.nix"), "nested")
	for _, n := range owned {
		write(t, filepath.Join(stg, n), "generated")
	}
	write(t, filepath.Join(stg, "stranger.txt"), "keep")

	if err := Replace(goodbye, stg); err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(filepath.Join(stg, "configuration.nix"))
	if err != nil || string(got) != "user config" {
		t.Fatalf("configuration.nix = %q, %v", got, err)
	}
	got, err = os.ReadFile(filepath.Join(stg, "sub", "deep", "x.nix"))
	if err != nil || string(got) != "nested" {
		t.Fatalf("nested = %q, %v", got, err)
	}
	for _, n := range owned {
		if n == "configuration.nix" {
			continue
		}
		if _, err := os.Lstat(filepath.Join(stg, n)); !os.IsNotExist(err) {
			t.Errorf("owned %s survived: %v", n, err)
		}
	}
	if _, err := os.Stat(filepath.Join(stg, "stranger.txt")); err != nil {
		t.Errorf("stranger removed: %v", err)
	}
}

func TestReplaceVerbatimWithoutBoot(t *testing.T) {
	goodbye, stg := t.TempDir(), t.TempDir()
	write(t, filepath.Join(goodbye, "configuration.nix"), "c")

	if err := Replace(goodbye, stg); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(stg, "boot.nix")); !os.IsNotExist(err) {
		t.Fatalf("boot.nix present: %v", err)
	}
}

func TestReplaceLeavesGoodbyeDirUnchanged(t *testing.T) {
	goodbye, stg := t.TempDir(), t.TempDir()
	write(t, filepath.Join(goodbye, "configuration.nix"), "c")
	write(t, filepath.Join(goodbye, "d", "e.nix"), "e")

	if err := Replace(goodbye, stg); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(goodbye)
	if err != nil || len(entries) != 2 {
		t.Fatalf("goodbye dir changed: %v, %v", entries, err)
	}
	got, _ := os.ReadFile(filepath.Join(goodbye, "d", "e.nix"))
	if string(got) != "e" {
		t.Fatalf("e.nix = %q", got)
	}
}
