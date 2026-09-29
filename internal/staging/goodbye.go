package staging

import (
	"os"
	"path/filepath"
	"slices"
)

func Owned() []string {
	return slices.Clone(owned)
}

// Replace makes stagingDir's contents exactly goodbyeDir's: it removes every
// owned entry from stagingDir, then copies each entry of goodbyeDir in.
// Nothing is preserved and nothing is backed up.
func Replace(goodbyeDir, stagingDir string) error {
	if err := Prune(stagingDir); err != nil {
		return err
	}
	entries, err := os.ReadDir(goodbyeDir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if err := copyDeref(filepath.Join(goodbyeDir, e.Name()), filepath.Join(stagingDir, e.Name())); err != nil {
			return err
		}
	}
	return nil
}
