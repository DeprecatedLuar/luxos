// Package nix is the sole adapter to external Nix tooling
// (implementation-plan.md G14). Every call to nix-instantiate, nix, or
// nix-build goes through here.
package nix

import (
	"bytes"
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

// writeFlakeArgs runs flake-file's write-flake app. --no-write-lock-file
// keeps the bootstrap's default nixpkgs from moving a lock pinned to another
// one; flakeLockArgs then fills in whatever is missing.
var writeFlakeArgs = []string{"run", "--no-write-lock-file", ".#write-flake"}

// flakeLockArgs locks missing inputs without moving existing pins.
var flakeLockArgs = []string{"flake", "lock"}

// rebuildAttr is the nix attribute path to a flake's built nixos-rebuild.
const rebuildAttr = "config.system.build.nixos-rebuild"

// rebuildBinRelpath is the binary's location inside that derivation's output.
const rebuildBinRelpath = "bin/nixos-rebuild"

// nixConfigVar is the environment variable nix reads extra settings from,
// one per line, applied after nix.conf.
const nixConfigVar = "NIX_CONFIG"

// flakeFeatures enables flakes for luxos's own nix calls, so the first
// rebuild on a machine whose nix.conf has them off (before system.nix
// turns them on) still works. extra- appends, so it is a no-op elsewhere.
const flakeFeatures = "extra-experimental-features = nix-command flakes"

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

// FlakeUpdate runs `nix flake update <inputs...> --flake <flakeDir>`, passing
// stdout and stderr through to the caller's. With no inputs every input moves.
func FlakeUpdate(flakeDir string, inputs ...string) error {
	args := append([]string{"flake", "update"}, inputs...)
	args = append(args, "--flake", flakeDir)
	cmd := exec.Command(flakeBin, args...)
	cmd.Env = flakeEnv(os.Environ())
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// BuildFlakeRef builds the flake reference ref and returns its store path.
// It tries --offline first; a reference that is not yet realized fails there
// and is retried once with the network allowed.
func BuildFlakeRef(ref string) (string, error) {
	out, err := buildFlakeRef(ref, true)
	if err != nil {
		out, err = buildFlakeRef(ref, false)
	}
	return out, err
}

// buildFlakeRef runs one `nix build <ref> --no-link --print-out-paths`.
func buildFlakeRef(ref string, offline bool) (string, error) {
	args := []string{"build", ref}
	if offline {
		args = append(args, "--offline")
	}
	args = append(args, "--no-link", "--print-out-paths")
	cmd := exec.Command(flakeBin, args...)
	cmd.Env = flakeEnv(os.Environ())
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("%s %s: %w", flakeBin, strings.Join(args, " "), err)
	}
	outPath := strings.TrimSpace(string(out))
	if outPath == "" {
		return "", fmt.Errorf("%s %s: no output path printed", flakeBin, strings.Join(args, " "))
	}
	return outPath, nil
}

// WriteFlake runs flake-file's write-flake app in stagingDir, regenerating
// flake.nix from the input declarations in the staged modules.
func WriteFlake(stagingDir string) error {
	return runIn(stagingDir, writeFlakeArgs)
}

// FlakeLock runs `nix flake lock` in stagingDir: missing inputs are locked,
// existing pins stay where they are.
func FlakeLock(stagingDir string) error {
	return runIn(stagingDir, flakeLockArgs)
}

// runIn runs the nix binary with args in dir, wrapping a failure with the
// command line and its stderr.
func runIn(dir string, args []string) error {
	cmd := exec.Command(flakeBin, args...)
	cmd.Dir = dir
	cmd.Env = flakeEnv(os.Environ())
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s %s (in %s) failed: %w\n%s", flakeBin, strings.Join(args, " "), dir, err, stderr.String())
	}
	return nil
}

// RebuildFromFlake builds nixosConfigurations.<host>.config.system.build.nixos-rebuild
// out of the staged flake at stagingDir and returns the path to the
// resulting nixos-rebuild binary.
func RebuildFromFlake(stagingDir, host string) (string, error) {
	target := fmt.Sprintf("%s#nixosConfigurations.%s.%s", stagingDir, host, rebuildAttr)
	cmd := exec.Command(flakeBin, "build", target, "--no-link", "--print-out-paths")
	cmd.Env = flakeEnv(os.Environ())
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

// flakeEnv returns env with flakeFeatures added to NIX_CONFIG, keeping any
// settings already there.
func flakeEnv(env []string) []string {
	prefix := nixConfigVar + "="
	out := make([]string, 0, len(env)+1)
	value := flakeFeatures
	for _, kv := range env {
		if existing, ok := strings.CutPrefix(kv, prefix); ok {
			if existing != "" {
				value = existing + "\n" + flakeFeatures
			}
			continue
		}
		out = append(out, kv)
	}
	return append(out, prefix+value)
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
