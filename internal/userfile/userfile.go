// Package userfile writes files into a user-owned tree while running as root:
// every file gets its parent directory's owner.
package userfile

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
)

const fileMode = 0644

// Write writes data to path (truncating in place, never renaming), sets its
// mode to 0644 and gives it the uid/gid of its parent directory.
func Write(path string, data []byte) error {
	dir := filepath.Dir(path)
	info, err := os.Stat(dir)
	if err != nil {
		return err
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return fmt.Errorf("cannot read the owner of %s", dir)
	}
	if err := os.WriteFile(path, data, fileMode); err != nil {
		return err
	}
	if err := os.Chmod(path, fileMode); err != nil {
		return err
	}
	return os.Chown(path, int(st.Uid), int(st.Gid))
}

// Create writes data to path only when nothing exists there (checked with
// Lstat, so a symlink or directory counts as existing). It reports whether
// the file was created.
func Create(path string, data []byte) (created bool, err error) {
	_, err = os.Lstat(path)
	if err == nil {
		return false, nil
	}
	if !os.IsNotExist(err) {
		return false, err
	}
	err = Write(path, data)
	return err == nil, err
}

const dirMode = 0755

// Mkdir creates the single directory path (0755) and gives it the uid/gid of
// its parent. An existing path is an error, never a silent merge.
func Mkdir(path string) error {
	uid, gid, err := ownerOf(filepath.Dir(path))
	if err != nil {
		return err
	}
	if err := os.Mkdir(path, dirMode); err != nil {
		return err
	}
	if err := os.Chmod(path, dirMode); err != nil {
		return err
	}
	return os.Lchown(path, uid, gid)
}

// MkdirAll creates path and every missing ancestor (0755), each given the
// uid/gid of its own parent. An existing path is not an error.
func MkdirAll(path string) error {
	info, err := os.Stat(path)
	if err == nil {
		if !info.IsDir() {
			return fmt.Errorf("%s exists and is not a directory", path)
		}
		return nil
	}
	if !os.IsNotExist(err) {
		return err
	}
	if err := MkdirAll(filepath.Dir(path)); err != nil {
		return err
	}
	return Mkdir(path)
}

// ChownTree gives root and everything under it the uid/gid of root's parent.
// It uses Lchown throughout, so a symlink is re-owned and never followed.
func ChownTree(root string) error {
	uid, gid, err := ownerOf(filepath.Dir(root))
	if err != nil {
		return err
	}
	return filepath.WalkDir(root, func(p string, _ fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		return os.Lchown(p, uid, gid)
	})
}

func ownerOf(dir string) (uid, gid int, err error) {
	info, err := os.Stat(dir)
	if err != nil {
		return 0, 0, err
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, 0, fmt.Errorf("cannot read the owner of %s", dir)
	}
	return int(st.Uid), int(st.Gid), nil
}
