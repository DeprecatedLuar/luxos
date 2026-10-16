package commands

import (
	"path/filepath"

	"github.com/DeprecatedLuar/luxos/internal/config"
	"github.com/DeprecatedLuar/luxos/internal/modules"
	"github.com/DeprecatedLuar/luxos/internal/ui"
)

// syncUsers restores the vendored files of every user unit h sees, shadows
// included; a hidden original is left alone.
func syncUsers(out *ui.Progress, h *modules.Host) error {
	out.Printf("Syncing user units...\n")
	for _, m := range h.Modules {
		if !modules.IsUser(m) {
			continue
		}
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
