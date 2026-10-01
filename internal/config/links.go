// Every directory is a parameter; nothing here resolves paths or prints.
package config

import (
	"fmt"
	"os"
	"path/filepath"
)

// localLinkName names the link at CONFIG_DIR and at the root of modules/ to the active host.
const localLinkName = "local"

// Errors if a real file already occupies that path.
func EnsureMirror(machinesDir, modulesDir, host string) error {
	hostDir := filepath.Join(machinesDir, host)
	if err := MkdirAll(filepath.Join(hostDir, localModulesDir)); err != nil {
		return err
	}

	sharedDefault := filepath.Join(modulesDir, DefaultFile)
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

// Errors if a real file/dir already occupies that name.
func ensureLocalLink(configDir, machinesDir, host string) error {
	link := filepath.Join(configDir, localLinkName)
	target := filepath.Join(machinesDir, host)

	if err := refuseRealFile(link, "reserved for the link to the active host's "+target+"; nothing else may claim it"); err != nil {
		return err
	}

	return relink(link, target)
}

// Errors if a real file or directory already occupies that name — nothing
// else may claim it.
func EnsureLocalModules(machinesDir, modulesDir, host string) error {
	target := filepath.Join(machinesDir, host, localModulesDir)
	if err := MkdirAll(target); err != nil {
		return err
	}

	link := filepath.Join(modulesDir, localLinkName)
	if err := refuseRealFile(link, "reserved for the link to the active host's local modules; nothing else may claim it"); err != nil {
		return err
	}

	rel, err := filepath.Rel(filepath.Dir(link), target)
	if err != nil {
		return err
	}

	return relink(link, rel)
}

// A real file or directory named hardware in any machine's modules/ is an error.
func ensureHardwareLink(machinesDir, hardwareRoot, host, key string) error {
	entries, err := os.ReadDir(machinesDir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		link := filepath.Join(machinesDir, e.Name(), localModulesDir, HardwareUnitName)
		if err := refuseRealFile(link, "reserved for the link to this computer's hardware folder"); err != nil {
			return err
		}
		if e.Name() == host {
			continue
		}
		if err := os.Remove(link); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	linkDir := filepath.Join(machinesDir, host, localModulesDir)
	if err := MkdirAll(linkDir); err != nil {
		return err
	}
	rel, err := filepath.Rel(linkDir, filepath.Join(hardwareRoot, key))
	if err != nil {
		return err
	}
	return relink(filepath.Join(linkDir, HardwareUnitName), rel)
}

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

// Removes any existing symlink first so self-heal stays idempotent.
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
