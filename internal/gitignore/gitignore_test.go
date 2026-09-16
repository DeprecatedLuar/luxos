package gitignore

import (
	"os"
	"path/filepath"
	"testing"
)

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", path, err)
	}
	return string(data)
}

func TestEnsure_MissingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".gitignore")

	added, err := Ensure(path, []string{"a", "b"})
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	if len(added) != 2 || added[0] != "a" || added[1] != "b" {
		t.Fatalf("added = %v, want [a b]", added)
	}

	got := readFile(t, path)
	want := "a\nb\n"
	if got != want {
		t.Fatalf("content = %q, want %q", got, want)
	}
}

func TestEnsure_AllPresent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".gitignore")
	if err := os.WriteFile(path, []byte("a\nb\n"), 0644); err != nil {
		t.Fatal(err)
	}

	before := readFile(t, path)
	beforeInfo, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}

	added, err := Ensure(path, []string{"a", "b"})
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	if len(added) != 0 {
		t.Fatalf("added = %v, want none", added)
	}

	after := readFile(t, path)
	if after != before {
		t.Fatalf("content changed: %q -> %q", before, after)
	}
	afterInfo, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !afterInfo.ModTime().Equal(beforeInfo.ModTime()) {
		t.Fatalf("file was rewritten though nothing needed to change")
	}
}

func TestEnsure_SomeMissing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".gitignore")
	if err := os.WriteFile(path, []byte("a\n"), 0644); err != nil {
		t.Fatal(err)
	}

	added, err := Ensure(path, []string{"a", "b", "c"})
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	if len(added) != 2 || added[0] != "b" || added[1] != "c" {
		t.Fatalf("added = %v, want [b c]", added)
	}

	got := readFile(t, path)
	want := "a\nb\nc\n"
	if got != want {
		t.Fatalf("content = %q, want %q", got, want)
	}
}

func TestEnsure_NoTrailingNewline(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".gitignore")
	if err := os.WriteFile(path, []byte("a"), 0644); err != nil {
		t.Fatal(err)
	}

	added, err := Ensure(path, []string{"b"})
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	if len(added) != 1 || added[0] != "b" {
		t.Fatalf("added = %v, want [b]", added)
	}

	got := readFile(t, path)
	want := "a\nb\n"
	if got != want {
		t.Fatalf("content = %q, want %q", got, want)
	}
}

func TestEnsure_DuplicateRequestedLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".gitignore")

	added, err := Ensure(path, []string{"a", "a", "b", "a"})
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	if len(added) != 2 || added[0] != "a" || added[1] != "b" {
		t.Fatalf("added = %v, want [a b]", added)
	}

	got := readFile(t, path)
	want := "a\nb\n"
	if got != want {
		t.Fatalf("content = %q, want %q", got, want)
	}
}
