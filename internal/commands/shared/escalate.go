package shared

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"
)

// configDirEnv mirrors internal/paths' LUXOS_CONFIG_DIR override; it is
// preserved across the sudo re-exec because sudo otherwise drops it (sudo
// resets the environment by default).
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

	argv := []string{sudoPath}
	if v, ok := os.LookupEnv(configDirEnv); ok {
		argv = append(argv, fmt.Sprintf("%s=%s", configDirEnv, v))
	}
	argv = append(argv, exe)
	argv = append(argv, args...)

	return syscall.Exec(sudoPath, argv, os.Environ())
}
