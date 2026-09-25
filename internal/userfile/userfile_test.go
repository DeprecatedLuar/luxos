package userfile

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestWriteCreates(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "f")
	if err := Write(p, []byte("hi")); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(p)
	if string(got) != "hi" {
		t.Fatalf("content %q", got)
	}
	info, _ := os.Stat(p)
	if info.Mode().Perm() != 0644 {
		t.Fatalf("mode %v", info.Mode().Perm())
	}
	dinfo, _ := os.Stat(dir)
	fs, ds := info.Sys().(*syscall.Stat_t), dinfo.Sys().(*syscall.Stat_t)
	if fs.Uid != ds.Uid || fs.Gid != ds.Gid {
		t.Fatalf("owner %d:%d, want %d:%d", fs.Uid, fs.Gid, ds.Uid, ds.Gid)
	}
}

func TestWriteKeepsInode(t *testing.T) {
	p := filepath.Join(t.TempDir(), "f")
	if err := os.WriteFile(p, []byte("old content"), 0600); err != nil {
		t.Fatal(err)
	}
	before, _ := os.Stat(p)
	if err := Write(p, []byte("new")); err != nil {
		t.Fatal(err)
	}
	after, _ := os.Stat(p)
	got, _ := os.ReadFile(p)
	if string(got) != "new" {
		t.Fatalf("content %q", got)
	}
	if !os.SameFile(before, after) {
		t.Fatal("inode changed")
	}
}

func TestWriteMissingParent(t *testing.T) {
	if err := Write(filepath.Join(t.TempDir(), "no", "f"), []byte("x")); err == nil {
		t.Fatal("expected error")
	}
}

func TestCreateMissing(t *testing.T) {
	p := filepath.Join(t.TempDir(), "f")
	created, err := Create(p, []byte("x"))
	if err != nil || !created {
		t.Fatalf("created=%v err=%v", created, err)
	}
	got, _ := os.ReadFile(p)
	if string(got) != "x" {
		t.Fatalf("content %q", got)
	}
}

func TestCreateExistingFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "f")
	if err := os.WriteFile(p, nil, 0644); err != nil {
		t.Fatal(err)
	}
	created, err := Create(p, []byte("x"))
	if err != nil || created {
		t.Fatalf("created=%v err=%v", created, err)
	}
	got, _ := os.ReadFile(p)
	if len(got) != 0 {
		t.Fatalf("content %q", got)
	}
}

func TestCreateExistingDir(t *testing.T) {
	created, err := Create(t.TempDir(), []byte("x"))
	if err != nil || created {
		t.Fatalf("created=%v err=%v", created, err)
	}
}

func TestMkdir_ErrorsWhenExists(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "d")
	if err := Mkdir(dir); err != nil {
		t.Fatal(err)
	}
	if err := Mkdir(dir); err == nil {
		t.Fatal("Mkdir on an existing path succeeded")
	}
}

func TestMkdir_NeedsParent(t *testing.T) {
	if err := Mkdir(filepath.Join(t.TempDir(), "a", "b")); err == nil {
		t.Fatal("Mkdir created a missing parent")
	}
}

func TestMkdirAll_CreatesParentsAndIsIdempotent(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "a", "b", "c")
	if err := MkdirAll(dir); err != nil {
		t.Fatal(err)
	}
	if err := MkdirAll(dir); err != nil {
		t.Fatalf("second MkdirAll: %v", err)
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		t.Fatalf("dir not created: %v", err)
	}
}

func TestChownTree_DoesNotFollowSymlink(t *testing.T) {
	base := t.TempDir()
	outside := filepath.Join(base, "outside")
	if err := os.MkdirAll(outside, 0755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(outside, "target.txt")
	if err := os.WriteFile(target, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(base, "tree")
	if err := os.MkdirAll(root, 0755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	if err := ChownTree(root); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Lstat(link); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("link is no longer a symlink: %v", err)
	}
	if b, err := os.ReadFile(target); err != nil || string(b) != "x" {
		t.Fatalf("target changed: %q %v", b, err)
	}
}
