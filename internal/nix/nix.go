// Package nix is the sole adapter to external Nix tooling
// (implementation-plan.md G14). Every call to nix-instantiate, nix, or
// nix-build goes through here.
package nix

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// instantiateBin is the binary used to parse Nix source.
const instantiateBin = "nix-instantiate"

// flakeBin is the binary used to operate on flakes.
const flakeBin = "nix"

// Parse runs `nix-instantiate --parse <absPath>` and returns its stdout.
// absPath must be an absolute path; the binary must be on PATH. On failure
// the returned error includes the command's stderr.
func Parse(absPath string) (string, error) {
	if !filepath.IsAbs(absPath) {
		return "", fmt.Errorf("nix.Parse: path %q is not absolute", absPath)
	}

	if _, err := exec.LookPath(instantiateBin); err != nil {
		return "", fmt.Errorf("nix.Parse: %s not found: %w", instantiateBin, err)
	}

	cmd := exec.Command(instantiateBin, "--parse", absPath)
	out, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return "", fmt.Errorf("%s --parse %s failed:\n%s", instantiateBin, absPath, string(exitErr.Stderr))
		}
		return "", fmt.Errorf("%s --parse %s: %w", instantiateBin, absPath, err)
	}

	return string(out), nil
}

// FlakeUpdate runs `nix flake update --flake <flakeDir>`, passing stdout
// and stderr through to the caller's.
func FlakeUpdate(flakeDir string) error {
	cmd := exec.Command(flakeBin, "flake", "update", "--flake", flakeDir)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
