// Package staging materializes the active host's flake root: a real-file
// copy of the shared modules tree and the active host's own tree (symlinks
// dereferenced), plus the whitelisted slice of the framework a flake needs
// to evaluate itself. Every directory is a parameter (implementation-plan.md
// G10, G11); nothing here calls sudo (it runs as root already).
package staging

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/DeprecatedLuar/luxos/internal/framework"
	"github.com/DeprecatedLuar/luxos/internal/userfile"
)

const (
	dirMode        = 0755
	fileMode       = 0644
	sealedFileMode = 0444

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

	lockFileName = "flake.lock"
)

// Materialize regenerates the luxos-owned entries of stagingDir: it prunes
// them (see Prune), then writes framework/system.nix, framework/shadow.sh,
// framework/units.nix, framework/overlay.nix, framework/outputs.nix,
// framework/environment.nix, framework/luxos-hardware.nix,
// framework/luxos-hardware-defaults.nix, flake.nix, config/modules (from
// modulesDir), config/local (from hostDir), config/environment (from
// environmentFile, required), and flake.lock if lockFile exists. lockFile is
// the active host's own flake.lock (hostDir/flake.lock), not a config-root
// one. Entries outside the owned list, such as hardware-configuration.nix
// and boot.nix, are left alone; Adopt is what deals with strangers.
// Every symlink under modulesDir and hostDir is dereferenced. Refuses a
// dangling symlink under modulesDir or hostDir, naming it, before touching
// stagingDir.
func Materialize(stagingDir, modulesDir, hostDir, lockFile, environmentFile string) error {
	if err := checkNoDanglingLinks(modulesDir); err != nil {
		return err
	}
	if err := checkNoDanglingLinks(hostDir); err != nil {
		return err
	}

	if err := Prune(stagingDir); err != nil {
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

// LockInput is one input's locked identity, read from a flake.lock node.
type LockInput struct {
	Type  string
	Owner string
	Repo  string
	Rev   string
}

// ReadLockInput returns the locked identity of the node called name in
// lockFile. A missing file or an absent node reports false with no error;
// malformed JSON is an error. Type is not validated: that is the caller's call.
func ReadLockInput(lockFile, name string) (LockInput, bool, error) {
	data, err := os.ReadFile(lockFile)
	if err != nil {
		if os.IsNotExist(err) {
			return LockInput{}, false, nil
		}
		return LockInput{}, false, err
	}
	var lock struct {
		Nodes map[string]struct{ Locked LockInput }
	}
	if err := json.Unmarshal(data, &lock); err != nil {
		return LockInput{}, false, fmt.Errorf("parse %s: %w", lockFile, err)
	}
	node, ok := lock.Nodes[name]
	if !ok {
		return LockInput{}, false, nil
	}
	return node.Locked, true, nil
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

// Seal makes every file luxos generated read-only, so an accidental edit
// under /etc/nixos fails visibly instead of vanishing on the next rebuild.
// Directories stay 0755: unlinking is governed by the parent's mode, so
// Prune still works. This does not stop root, which is the common case
// under sudo - it is a signal, not a lock.
func Seal(stagingDir string) error {
	for _, name := range owned {
		root := filepath.Join(stagingDir, name)
		if _, err := os.Lstat(root); os.IsNotExist(err) {
			continue
		}
		err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			switch {
			case d.IsDir():
				return os.Chmod(p, dirMode)
			case d.Type().IsRegular():
				return os.Chmod(p, sealedFileMode)
			}
			return nil
		})
		if err != nil {
			return err
		}
	}
	return nil
}
