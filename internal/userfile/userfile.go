// Package userfile writes files into a user-owned tree while running as root:
// every file gets its parent directory's owner.
package userfile

import (
	"fmt"
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
