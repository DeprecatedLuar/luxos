package staging

import (
	"os"
	"path/filepath"
	"slices"
)

func Owned() []string {
	return slices.Clone(owned)
}

// Replace removes every owned entry from stagingDir, then copies each entry
// of srcDir in, symlinks dereferenced. Nothing is backed up.
func Replace(srcDir, stagingDir string) error {
	if err := Prune(stagingDir); err != nil {
		return err
	}
	entries, err := os.ReadDir(srcDir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if err := copyDeref(filepath.Join(srcDir, e.Name()), filepath.Join(stagingDir, e.Name())); err != nil {
			return err
		}
	}
	return nil
}
