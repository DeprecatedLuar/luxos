package commands

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/DeprecatedLuar/luxos/internal/config"
	"github.com/DeprecatedLuar/luxos/internal/modules"
	"github.com/DeprecatedLuar/luxos/internal/password"
	"github.com/DeprecatedLuar/luxos/internal/ui"
)

// keyOwnerUID is the uid that must own the stored user keys.
const keyOwnerUID = 0

// syncUsers restores the vendored files of users, the user units in the build.
func syncUsers(out *ui.Progress, users []modules.Module) error {
	out.Printf("Syncing user units...\n")
	for _, m := range users {
		changes, err := config.SyncUser(m.Abs)
		if err != nil {
			return err
		}
		for _, c := range changes {
			line := filepath.Join(m.Path, c.Path)
			if c.Action == config.ActionCreated {
				out.Changef("created: %s", line)
			} else {
				out.Warnf("%s: %s", c.Action, line)
			}
		}
	}
	return out.Err()
}

// checkPasswords runs the password preflight on users, the user units in the
// build, and reports every failing user before failing.
func checkPasswords(out *ui.Progress, users []modules.Module, keysDir string, uid int) error {
	out.Printf("Checking user passwords...\n")
	failed := 0
	for _, m := range users {
		if err := password.Check(m.Abs, keysDir, m.Name, uid); err != nil {
			out.Errf("%s", err)
			failed++
		}
	}
	if failed > 0 {
		return fmt.Errorf("user passwords: %d user(s) cannot log in", failed)
	}
	return out.Err()
}

// builtUsers returns every user unit the host builds, selected or pulled in
// through luxos.modules.
func builtUsers(h *modules.Host) ([]modules.Module, error) {
	built, err := h.Built()
	if err != nil {
		return nil, err
	}
	var users []modules.Module
	for _, m := range built {
		if m.Role() == modules.RoleUser {
			users = append(users, m)
		}
	}
	return users, nil
}

// requirePassword fails when m is a user unit without a password file.
func requirePassword(m modules.Module) error {
	if m.Role() != modules.RoleUser {
		return nil
	}
	if _, err := os.Stat(filepath.Join(m.Abs, password.File)); os.IsNotExist(err) {
		return fmt.Errorf("user %s has no password: luxos user passwd %s", m.Name, m.Name)
	} else if err != nil {
		return err
	}
	return nil
}
