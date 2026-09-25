package computer

import (
	"errors"
	"fmt"
	"io/fs"
	"os"

	"github.com/DeprecatedLuar/luxos/internal/nix"
	"github.com/DeprecatedLuar/luxos/internal/userfile"
)

// EnsureHardwareConfig writes hardwareFile from nixos-generate-config when
// nothing exists at that path, and reports whether it did. An existing entry
// is never read or rewritten.
func EnsureHardwareConfig(hardwareFile string) (created bool, err error) {
	if _, err := os.Lstat(hardwareFile); err == nil {
		return false, nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return false, err
	}
	content, err := nix.ShowHardwareConfig()
	if err != nil {
		return false, fmt.Errorf("cannot generate %s: %w\nrun by hand: nixos-generate-config --show-hardware-config > %s", hardwareFile, err, hardwareFile)
	}
	if err := userfile.Write(hardwareFile, content); err != nil {
		return false, fmt.Errorf("computer.EnsureHardwareConfig: write %s: %w", hardwareFile, err)
	}
	return true, nil
}
