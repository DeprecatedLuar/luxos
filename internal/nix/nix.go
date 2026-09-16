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
	"strings"
	"syscall"
)

// instantiateBin is the binary used to parse Nix source.
const instantiateBin = "nix-instantiate"

// flakeBin is the binary used to operate on flakes.
const flakeBin = "nix"

// buildBin is the classic nix-build binary, used for the channel-based
// (--bypass) escape hatch.
const buildBin = "nix-build"

// rebuildAttr is the nix attribute path to a flake's built nixos-rebuild.
const rebuildAttr = "config.system.build.nixos-rebuild"

// rebuildBinRelpath is the binary's location inside that derivation's output.
const rebuildBinRelpath = "bin/nixos-rebuild"

// rebuildChannelExpr is the classic <nixpkgs/nixos> NIX_PATH entry, used
// only when staging itself is broken.
const rebuildChannelExpr = "<nixpkgs/nixos>"

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

// RebuildFromFlake builds nixosConfigurations.<host>.config.system.build.nixos-rebuild
// out of the staged flake at stagingDir and returns the path to the
// resulting nixos-rebuild binary.
func RebuildFromFlake(stagingDir, host string) (string, error) {
	target := fmt.Sprintf("%s#nixosConfigurations.%s.%s", stagingDir, host, rebuildAttr)
	cmd := exec.Command(flakeBin, "build", target, "--no-link", "--print-out-paths")
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("%s build %s: %w", flakeBin, target, err)
	}
	outPath := strings.TrimSpace(string(out))
	if outPath == "" {
		return "", fmt.Errorf("%s build %s: no output path printed", flakeBin, target)
	}
	return filepath.Join(outPath, rebuildBinRelpath), nil
}

// RebuildFromChannel builds nixos-rebuild from the channel-based
// <nixpkgs/nixos>, bypassing staging and the flake entirely, and returns
// the path to the resulting nixos-rebuild binary.
func RebuildFromChannel() (string, error) {
	cmd := exec.Command(buildBin, rebuildChannelExpr, "-A", rebuildAttr, "--no-out-link")
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("%s %s -A %s: %w", buildBin, rebuildChannelExpr, rebuildAttr, err)
	}
	outPath := strings.TrimSpace(string(out))
	if outPath == "" {
		return "", fmt.Errorf("%s %s -A %s: no output path printed", buildBin, rebuildChannelExpr, rebuildAttr)
	}
	return filepath.Join(outPath, rebuildBinRelpath), nil
}

// Exec replaces the current process image with bin, passing args as
// argv[1:] (argv[0] is bin).
func Exec(bin string, args []string) error {
	argv := append([]string{bin}, args...)
	return syscall.Exec(bin, argv, os.Environ())
}
