package shared

import (
	"fmt"
	"os"

	"github.com/DeprecatedLuar/luxos/internal/paths"
	"github.com/DeprecatedLuar/luxos/internal/shell"
)

const sudoBin = "sudo"

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

	// The resolved config dir always crosses the re-exec; the backup dir only when set.
	argv := []string{fmt.Sprintf("%s=%s", paths.ConfigDirEnv, p.Config)}
	if backup := os.Getenv(paths.BackupDirEnv); backup != "" {
		argv = append(argv, fmt.Sprintf("%s=%s", paths.BackupDirEnv, backup))
	}
	argv = append(argv, exe)
	argv = append(argv, args...)

	return shell.Exec(sudoBin, argv)
}
