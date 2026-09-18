// Package links manages every symlink luxos maintains: the per-host modules
// mirror, the CONFIG_DIR/local link, /etc/nixos healing, and the lux
// self-link (G8). Every directory is a parameter; nothing here resolves
// paths or prints (implementation-plan.md G10, G11).
package links

import (
	"fmt"
	"os"
	"path/filepath"
)

const (
	dirMode = 0755

	etcNixosEnvFile = "env"
	etcNixosEnvMode = 0600

	configurationNix = "configuration.nix"

	mirrorEntrypoint = "default.nix"
	selectionFile    = "modules.nix"
	modulesRel       = "modules"
	localLinkName    = "local"
)

// EnsureMirror ensures <modulesDir>/default.nix is a symlink to
// <localDir>/<host>/modules.nix (L4), creating the host's local modules
// directory if needed. Errors if a real file already occupies that path.
func EnsureMirror(localDir, modulesDir, host string) error {
	hostDir := filepath.Join(localDir, host)
	if err := os.MkdirAll(filepath.Join(hostDir, modulesRel), dirMode); err != nil {
		return err
	}

	sharedDefault := filepath.Join(modulesDir, mirrorEntrypoint)
	target := filepath.Join(hostDir, selectionFile)

	if err := refuseRealFile(sharedDefault, "reserved for the generated mirror link to "+target); err != nil {
		return err
	}

	rel, err := filepath.Rel(filepath.Dir(sharedDefault), target)
	if err != nil {
		return err
	}

	return relink(sharedDefault, rel)
}

// EnsureLocalLink ensures <configDir>/local is a symlink to
// <localDir>/<host>: the active host's whole private tree, visible at root
// alongside the shared kind folders. Errors if a real file/dir already
// occupies that name.
func EnsureLocalLink(configDir, localDir, host string) error {
	link := filepath.Join(configDir, localLinkName)
	target := filepath.Join(localDir, host)

	if err := refuseRealFile(link, "reserved for the link to the active host's "+target+"; nothing else may claim it"); err != nil {
		return err
	}

	return relink(link, target)
}

// EnsureEtcNixos ensures etcNixos is a real directory (self-healing a stale
// symlink from the old single-file scheme), removes a stale
// configuration.nix symlink left from before the flake migration, and
// ensures an env file exists for service environmentFiles that expect it
// even when empty. Returns the actions it took, in order.
func EnsureEtcNixos(etcNixos string) ([]string, error) {
	var actions []string

	if info, err := os.Lstat(etcNixos); err == nil && info.Mode()&os.ModeSymlink != 0 {
		if err := os.Remove(etcNixos); err != nil {
			return actions, err
		}
		actions = append(actions, fmt.Sprintf("converted %s from a symlink to a real directory", etcNixos))
	} else if err != nil && !os.IsNotExist(err) {
		return actions, err
	}

	if _, err := os.Stat(etcNixos); os.IsNotExist(err) {
		if err := os.MkdirAll(etcNixos, dirMode); err != nil {
			return actions, err
		}
		actions = append(actions, fmt.Sprintf("created %s", etcNixos))
	} else if err != nil {
		return actions, err
	}

	configPath := filepath.Join(etcNixos, configurationNix)
	if info, err := os.Lstat(configPath); err == nil && info.Mode()&os.ModeSymlink != 0 {
		if err := os.Remove(configPath); err != nil {
			return actions, err
		}
		actions = append(actions, fmt.Sprintf("removed stale %s symlink", configPath))
	} else if err != nil && !os.IsNotExist(err) {
		return actions, err
	}

	envPath := filepath.Join(etcNixos, etcNixosEnvFile)
	if _, err := os.Stat(envPath); os.IsNotExist(err) {
		if err := os.WriteFile(envPath, nil, etcNixosEnvMode); err != nil {
			return actions, err
		}
		if err := os.Chmod(envPath, etcNixosEnvMode); err != nil {
			return actions, err
		}
		actions = append(actions, fmt.Sprintf("created %s", envPath))
	} else if err != nil {
		return actions, err
	}

	return actions, nil
}

// LuxLinkState reports what EnsureLux found or did.
type LuxLinkState int

const (
	LuxLinkOK LuxLinkState = iota
	LuxLinkCreated
	LuxLinkConflict
)

// EnsureLux ensures link is a symlink to target (G8). Missing -> created.
// Already a symlink to target -> ok. A symlink pointing elsewhere, or a
// real file/dir, is left untouched and reported as a conflict.
func EnsureLux(link, target string) (LuxLinkState, error) {
	if err := os.MkdirAll(filepath.Dir(link), dirMode); err != nil {
		return LuxLinkConflict, err
	}

	info, err := os.Lstat(link)
	if os.IsNotExist(err) {
		if err := os.Symlink(target, link); err != nil {
			return LuxLinkConflict, err
		}
		return LuxLinkCreated, nil
	}
	if err != nil {
		return LuxLinkConflict, err
	}

	if info.Mode()&os.ModeSymlink == 0 {
		return LuxLinkConflict, nil
	}

	current, err := os.Readlink(link)
	if err != nil {
		return LuxLinkConflict, err
	}
	if current != target {
		return LuxLinkConflict, nil
	}

	return LuxLinkOK, nil
}

// refuseRealFile errors if path exists and is not a symlink.
func refuseRealFile(path, reason string) error {
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if info.Mode()&os.ModeSymlink == 0 {
		return fmt.Errorf("'%s' is a real file/dir. %s", path, reason)
	}
	return nil
}

// relink replaces link with a symlink to target, removing any existing
// symlink first so self-heal stays idempotent.
func relink(link, target string) error {
	if info, err := os.Lstat(link); err == nil {
		if info.Mode()&os.ModeSymlink == 0 {
			return fmt.Errorf("'%s' is a real file/dir, refusing to replace it", link)
		}
		if err := os.Remove(link); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}

	return os.Symlink(target, link)
}
