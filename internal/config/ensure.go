package config

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	"github.com/DeprecatedLuar/luxos/internal/paths"
	"github.com/DeprecatedLuar/luxos/internal/templates"
	"github.com/DeprecatedLuar/luxos/internal/ui"
)

const (
	EnvironmentFile     = "environment"
	environmentTemplate = "starters/environment"

	gitignoreFile = ".gitignore"
	// SystemDir is the folder of modules/ holding the framework modules.
	SystemDir = "system"
)

var gitignoreLines = []string{"/modules/system", "/modules/default.nix", "/modules/host", "/modules/machine", "/host", "/machine"}

// Ensure brings CONFIG_DIR to the state a rebuild of host expects: .gitignore,
// the environment file, the synced framework modules, modules/default.nix,
// host.nix, the host folder (validated and protected), this computer's
// machine folder, the scope links and the host.nix checks.
func Ensure(out *ui.Progress, p paths.Paths, host string) error {
	hostDir := filepath.Join(p.Hosts, host)

	out.Printf("Ensuring .gitignore...\n")
	added, err := ensureGitignore(filepath.Join(p.Config, gitignoreFile), gitignoreLines)
	if err != nil {
		return err
	}
	for _, line := range added {
		out.Changef("  added: %s", line)
	}

	out.Printf("Ensuring environment file...\n")
	tmpl, err := templates.File(environmentTemplate)
	if err != nil {
		return err
	}
	envPath := filepath.Join(p.Config, EnvironmentFile)
	created, err := CreateFile(envPath, tmpl)
	if err != nil {
		return err
	}
	if created {
		out.Changef("  created: %s", envPath)
	}

	out.Printf("Syncing framework modules...\n")
	changes, err := Sync(filepath.Join(p.Modules, SystemDir))
	if err != nil {
		return err
	}
	for _, c := range changes {
		line := fmt.Sprintf("%s: %s", c.Action, c.Path)
		if c.Action == ActionCreated {
			out.Changef("%s", line)
		} else {
			out.Warnf("%s", line)
		}
	}

	action, err := SyncEntry(p.Modules)
	if err != nil {
		return err
	}
	switch action {
	case ActionCreated:
		out.Changef("created: %s", DefaultFile)
	case ActionRestored:
		out.Warnf("restored: %s", DefaultFile)
	}

	if err := ensureHostFile(out, hostDir); err != nil {
		return err
	}

	out.Printf("Validating %s...\n", hostDir)
	if err := ValidateHost(hostDir); err != nil {
		return err
	}
	protected, err := ProtectHost(hostDir)
	if err != nil {
		return err
	}
	if protected {
		out.Changef("  protected: %s", filepath.Join(hostDir, plsDontTouchFile))
	}

	machineDir, err := EnsureMachine(out, p)
	if err != nil {
		return err
	}
	out.Printf("Ensuring host and machine links...\n")
	if err := EnsureScopeLinks(p.Config, p.Modules, hostDir, machineDir); err != nil {
		return err
	}

	if err := checkHostFile(filepath.Join(hostDir, HostFile)); err != nil {
		return err
	}
	if _, err := readBaseChannel(filepath.Join(hostDir, HostFile)); err != nil {
		return err
	}

	return out.Err()
}

// ActiveHost returns the active host's name, read from the modules/host link
// to <hosts>/<host>/modules.
func ActiveHost(modulesDir string) (string, error) {
	real, err := filepath.EvalSymlinks(filepath.Join(modulesDir, HostLinkName))
	if err != nil {
		return "", fmt.Errorf("no active host: modules/host is missing, run luxos rebuild first")
	}
	return filepath.Base(filepath.Dir(real)), nil
}

// CopyLockBack writes the staged flake.lock to hostLock when the bytes differ
// or hostLock is missing, and reports whether it did.
func CopyLockBack(stagedLock, hostLock string) (changed bool, err error) {
	staged, err := os.ReadFile(stagedLock)
	if err != nil {
		return false, err
	}
	host, err := os.ReadFile(hostLock)
	if err == nil && bytes.Equal(staged, host) {
		return false, nil
	}
	if err != nil && !os.IsNotExist(err) {
		return false, err
	}
	return true, WriteFile(hostLock, staged)
}
