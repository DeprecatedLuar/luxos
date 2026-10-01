package shared

import (
	"fmt"
	"os"

	"github.com/DeprecatedLuar/luxos/internal/paths"
	"github.com/DeprecatedLuar/luxos/internal/shell"
)

const sudoBin = "sudo"

// Resolved config dir is passed through this across the sudo re-exec.
const configDirEnv = "LUXOS_CONFIG_DIR"

// Crosses the sudo re-exec only when set.
const backupDirEnv = "LUXOS_BACKUP_DIR"

// EnsureRoot re-execs the running binary under sudo when not already root.
// args are the original CLI arguments, command name prepended by the
// caller. Returns only on error; success replaces the process image.
func EnsureRoot(args []string) error {
	if os.Geteuid() == 0 {
		return nil
	}

	exe, err := shell.Self()
	if err != nil {
		return err
	}

	// Resolve as the invoking user: under sudo, pam_env re-expands
	// XDG_CONFIG_HOME against root's HOME.
	p, err := paths.Resolve()
	if err != nil {
		return err
	}

	argv := []string{fmt.Sprintf("%s=%s", configDirEnv, p.Config)}
	if backup := os.Getenv(backupDirEnv); backup != "" {
		argv = append(argv, fmt.Sprintf("%s=%s", backupDirEnv, backup))
	}
	argv = append(argv, exe)
	argv = append(argv, args...)

	return shell.Exec(sudoBin, argv)
}
