package staging

import "os"

// Recover undoes a promotion interrupted between its two renames, then
// removes whatever an earlier run left in newDir and oldDir.
func Recover(stagingDir, newDir, oldDir string) error {
	if _, err := os.Lstat(stagingDir); os.IsNotExist(err) {
		if _, err := os.Lstat(oldDir); err == nil {
			if err := os.Rename(oldDir, stagingDir); err != nil {
				return err
			}
		} else if !os.IsNotExist(err) {
			return err
		}
	} else if err != nil {
		return err
	}
	if err := os.RemoveAll(newDir); err != nil {
		return err
	}
	return os.RemoveAll(oldDir)
}

// Promote replaces stagingDir with newDir: stagingDir is renamed to oldDir,
// newDir to stagingDir, then oldDir is removed. The three must share a
// filesystem and oldDir must not exist. A missing stagingDir is skipped.
func Promote(newDir, stagingDir, oldDir string) error {
	if err := os.Rename(stagingDir, oldDir); err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := os.Rename(newDir, stagingDir); err != nil {
		return err
	}
	return os.RemoveAll(oldDir)
}
