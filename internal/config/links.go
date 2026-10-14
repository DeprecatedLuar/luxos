// Every directory is a parameter; nothing here resolves paths or prints.
package config

import (
	"fmt"
	"os"
	"path/filepath"
)

// Link names at CONFIG_DIR and at the root of modules/.
const (
	HostLinkName    = "host"
	MachineLinkName = "machine"
)

// EnsureScopeLinks points modules/host and modules/machine at the scopes'
// modules folders, and CONFIG_DIR/host and CONFIG_DIR/machine at the
// folders themselves, all relative. A real entry at any of them is an error.
func EnsureScopeLinks(configDir, modulesDir, hostDir, machineDir string) error {
	links := []struct{ link, target string }{
		{filepath.Join(modulesDir, HostLinkName), filepath.Join(hostDir, ScopeModulesDir)},
		{filepath.Join(modulesDir, MachineLinkName), filepath.Join(machineDir, ScopeModulesDir)},
		{filepath.Join(configDir, HostLinkName), hostDir},
		{filepath.Join(configDir, MachineLinkName), machineDir},
	}
	for _, l := range links {
		if err := refuseRealFile(l.link, "reserved for the link to "+l.target+"; nothing else may claim it"); err != nil {
			return err
		}
		rel, err := filepath.Rel(filepath.Dir(l.link), l.target)
		if err != nil {
			return err
		}
		if err := relink(l.link, rel); err != nil {
			return err
		}
	}
	return nil
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
