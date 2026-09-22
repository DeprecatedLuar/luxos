// Package staging materializes the active host's flake root: a real-file
// copy of the shared modules tree and the active host's own tree (symlinks
// dereferenced), plus the whitelisted slice of the framework a flake needs
// to evaluate itself. Every directory is a parameter (implementation-plan.md
// G10, G11); nothing here calls sudo (it runs as root already).
package staging

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/DeprecatedLuar/luxos/internal/framework"
	"github.com/DeprecatedLuar/luxos/internal/nix"
	"github.com/DeprecatedLuar/luxos/internal/userfile"
)

// Marker marks a directory as a staging tree this package created and may
// wipe.
const Marker = ".luxos-staging"

const (
	dirMode  = 0755
	fileMode = 0644

	frameworkDir = "framework"
	configDir    = "config"

	flakeNix   = "flake.nix"
	systemNix  = "system.nix"
	shadowSh   = "shadow.sh"
	unitsNix   = "units.nix"
	overlayNix = "overlay.nix"
	outputsNix = "outputs.nix"

	environmentNix        = "environment.nix"
	luxosHardwareNix      = "luxos-hardware.nix"
	luxosHardwareDefaults = "luxos-hardware-defaults.nix"
	stagedEnvironment     = "environment"

	stagedModulesDir = "modules"
	stagedLocalDir   = "local"

	hardwareConfigName = "hardware-configuration.nix"
	bootConfigName     = "boot.nix"
	lockFileName       = "flake.lock"
)

// Materialize wipes and rebuilds stagingDir: framework/system.nix,
// framework/shadow.sh, framework/units.nix, framework/overlay.nix, framework/outputs.nix, framework/environment.nix,
// framework/luxos-hardware.nix, framework/luxos-hardware-defaults.nix, flake.nix,
// config/modules (from modulesDir), config/local (from hostDir), config/environment (from environmentFile, required),
// hardware-configuration.nix, boot.nix (from bootConfig, required), and
// flake.lock if lockFile exists. lockFile is the active host's own
// flake.lock (hostDir/flake.lock), not a config-root one.
// Every symlink under modulesDir and hostDir is dereferenced. Refuses to
// touch stagingDir unless it is empty or a tree this package created
// (marked with Marker), and refuses a dangling symlink under modulesDir or
// hostDir, naming it.
func Materialize(stagingDir, modulesDir, hostDir, hardwareConfig, bootConfig, lockFile, environmentFile string) error {
	if err := guard(stagingDir); err != nil {
		return err
	}
	if err := checkNoDanglingLinks(modulesDir); err != nil {
		return err
	}
	if err := checkNoDanglingLinks(hostDir); err != nil {
		return err
	}

	if err := os.RemoveAll(stagingDir); err != nil {
		return err
	}

	fwDir := filepath.Join(stagingDir, frameworkDir)
	cfgDir := filepath.Join(stagingDir, configDir)
	if err := os.MkdirAll(fwDir, dirMode); err != nil {
		return err
	}
	if err := os.MkdirAll(cfgDir, dirMode); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(stagingDir, Marker), nil, fileMode); err != nil {
		return err
	}

	if err := writeFrameworkFile(fwDir, systemNix); err != nil {
		return err
	}
	if err := writeFrameworkFile(fwDir, shadowSh); err != nil {
		return err
	}
	if err := writeFrameworkFile(fwDir, unitsNix); err != nil {
		return err
	}
	if err := writeFrameworkFile(fwDir, overlayNix); err != nil {
		return err
	}
	if err := writeFrameworkFile(fwDir, outputsNix); err != nil {
		return err
	}
	if err := writeFrameworkFile(fwDir, environmentNix); err != nil {
		return err
	}
	if err := writeFrameworkFile(fwDir, luxosHardwareNix); err != nil {
		return err
	}
	if err := writeFrameworkFile(fwDir, luxosHardwareDefaults); err != nil {
		return err
	}
	if err := writeFrameworkFile(stagingDir, flakeNix); err != nil {
		return err
	}

	if err := copyDeref(modulesDir, filepath.Join(cfgDir, stagedModulesDir)); err != nil {
		return err
	}
	if err := copyDeref(hostDir, filepath.Join(cfgDir, stagedLocalDir)); err != nil {
		return err
	}

	if err := copyFile(environmentFile, filepath.Join(cfgDir, stagedEnvironment)); err != nil {
		return err
	}

	if err := copyFile(hardwareConfig, filepath.Join(stagingDir, hardwareConfigName)); err != nil {
		return err
	}

	if err := copyFile(bootConfig, filepath.Join(stagingDir, bootConfigName)); err != nil {
		return err
	}

	if _, err := os.Stat(lockFile); err == nil {
		if err := copyFile(lockFile, filepath.Join(stagingDir, lockFileName)); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}

	return nil
}

// Install writes content as stagingDir/rel, creating parent directories as
// needed.
func Install(stagingDir, rel string, content []byte) error {
	dest := filepath.Join(stagingDir, rel)
	if err := os.MkdirAll(filepath.Dir(dest), dirMode); err != nil {
		return err
	}
	if err := os.WriteFile(dest, content, fileMode); err != nil {
		return err
	}
	return os.Chmod(dest, fileMode)
}

// UpdateLock re-locks the staged flake and copies the result back to
// hostLock (the active host's flake.lock) via CopyLockBack, to be committed.
// Not exercised by unit tests: it requires real network/nix flake access.
func UpdateLock(stagingDir, hostLock string) error {
	if err := nix.FlakeUpdate(stagingDir); err != nil {
		return err
	}
	return CopyLockBack(stagingDir, hostLock)
}

// LockChanged reports whether the staged flake.lock differs from hostLock.
// A missing hostLock counts as changed; a missing staged lock is an error.
func LockChanged(stagingDir, hostLock string) (bool, error) {
	staged, err := os.ReadFile(filepath.Join(stagingDir, lockFileName))
	if err != nil {
		return false, err
	}
	host, err := os.ReadFile(hostLock)
	if err != nil {
		if os.IsNotExist(err) {
			return true, nil
		}
		return false, err
	}
	return !bytes.Equal(staged, host), nil
}

// CopyLockBack copies the staged flake.lock to hostLock via userfile.Write,
// so it is owned by whoever owns hostLock's directory (the invoking user,
// since rebuild runs as root).
func CopyLockBack(stagingDir, hostLock string) error {
	data, err := os.ReadFile(filepath.Join(stagingDir, lockFileName))
	if err != nil {
		return err
	}
	return userfile.Write(hostLock, data)
}

func guard(stagingDir string) error {
	if _, err := os.Lstat(stagingDir); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if _, err := os.Stat(filepath.Join(stagingDir, Marker)); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("%s exists but isn't a luxos-managed staging tree; remove it manually first if that's intended", stagingDir)
		}
		return err
	}
	return nil
}

// checkNoDanglingLinks walks root and errors, naming the culprit, on the
// first symlink whose target does not exist.
func checkNoDanglingLinks(root string) error {
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type()&os.ModeSymlink == 0 {
			return nil
		}
		if _, err := os.Stat(path); err != nil {
			if os.IsNotExist(err) {
				return fmt.Errorf("dangling symlink, cannot stage: %s", path)
			}
			return err
		}
		return nil
	})
}

func writeFrameworkFile(dst, name string) error {
	data, err := framework.File(name)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dst, name), data, fileMode)
}

// copyDeref copies src into dst, dereferencing every symlink it walks
// through (a symlink to a directory is copied as that directory's
// contents; a symlink to a file is copied as that file). No symlink
// survives under dst.
func copyDeref(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)

		if d.Type()&os.ModeSymlink != 0 {
			resolved, err := filepath.EvalSymlinks(path)
			if err != nil {
				return err
			}
			info, err := os.Stat(resolved)
			if err != nil {
				return err
			}
			if info.IsDir() {
				return copyDeref(resolved, target)
			}
			return copyFile(resolved, target)
		}

		if d.IsDir() {
			return os.MkdirAll(target, dirMode)
		}

		return copyFile(path, target)
	})
}

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), dirMode); err != nil {
		return err
	}
	return os.WriteFile(dst, data, fileMode)
}
