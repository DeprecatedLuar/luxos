package shared

import (
	"os"
	"path/filepath"
)

// Executable returns the absolute, symlink-resolved path to the running
// luxos binary.
func Executable() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(exe)
}
