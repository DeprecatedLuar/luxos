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
