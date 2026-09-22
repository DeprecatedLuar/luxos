package shared

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"

	"github.com/DeprecatedLuar/luxos/internal/paths"
)

// configDirEnv mirrors internal/paths' LUXOS_CONFIG_DIR override; the
// resolved config dir is passed through it across the sudo re-exec.
const configDirEnv = "LUXOS_CONFIG_DIR"

// EnsureRoot re-execs the running binary under sudo when not already root
// (implementation-plan.md G9). args are the original CLI arguments (with
// the command name prepended by the caller). It returns only on error;
// success replaces the process image.
func EnsureRoot(args []string) error {
	if os.Geteuid() == 0 {
		return nil
	}

	sudoPath, err := exec.LookPath("sudo")
	if err != nil {
		return fmt.Errorf("sudo not found: %w", err)
	}

	exe, err := Executable()
	if err != nil {
		return err
	}

	// Resolve as the invoking user: under sudo, pam_env re-expands
	// XDG_CONFIG_HOME against root's HOME.
	p, err := paths.Resolve()
	if err != nil {
		return err
	}

	argv := []string{sudoPath, fmt.Sprintf("%s=%s", configDirEnv, p.Config), exe}
	argv = append(argv, args...)

	return syscall.Exec(sudoPath, argv, os.Environ())
}
