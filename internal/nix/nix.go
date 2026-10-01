// Package nix is the only adapter for external Nix tooling (nix-instantiate, nix, nix-build).
package nix

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/DeprecatedLuar/luxos/internal/shell"
)

const instantiateBin = "nix-instantiate"

const flakeBin = "nix"

const buildBin = "nix-build"

// writeFlakeArgs runs flake-file's write-flake app. --no-write-lock-file
// keeps the bootstrap's default nixpkgs from moving a lock pinned to another
// one; flakeLockArgs then fills in whatever is missing.
var writeFlakeArgs = []string{"run", "--no-write-lock-file", ".#write-flake"}

// flakeLockArgs locks missing inputs without moving existing pins.
var flakeLockArgs = []string{"flake", "lock"}

const hardwareConfigBin = "nixos-generate-config"

var hardwareConfigArgs = []string{"--show-hardware-config"}

const rebuildAttr = "config.system.build.nixos-rebuild"

const rebuildBinRelpath = "bin/nixos-rebuild"

const nixConfigVar = "NIX_CONFIG"

// flakeFeatures enables flakes for luxos's own nix calls, so the first
// rebuild on a machine whose nix.conf has them off (before system.nix
// turns them on) still works. extra- appends, so it is a no-op elsewhere.
const flakeFeatures = "extra-experimental-features = nix-command flakes"

const rebuildChannelExpr = "<nixpkgs/nixos>"

// Parse runs `nix-instantiate --parse <absPath>` and returns its stdout.
// absPath must be an absolute path; the binary must be on PATH. On failure
// the returned error includes the command's stderr.
func Parse(absPath string) (string, error) {
	if !filepath.IsAbs(absPath) {
		return "", fmt.Errorf("nix.Parse: path %q is not absolute", absPath)
	}

	out, err := shell.Output(shell.Cmd{Bin: instantiateBin, Args: []string{"--parse", absPath}})
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// EvalJSON runs `nix-instantiate --eval --strict --json --expr <expr>` with
// args passed through as --argstr, and returns its stdout. On failure the
// returned error includes the command's stderr.
func EvalJSON(expr string, args map[string]string) ([]byte, error) {
	argv := []string{"--eval", "--strict", "--json", "--expr", expr}
	for k, v := range args {
		argv = append(argv, "--argstr", k, v)
	}
	return shell.Output(shell.Cmd{Bin: instantiateBin, Args: argv})
}

// FlakeUpdate runs `nix flake update <inputs...> --flake <flakeDir>`, passing
// stdout and stderr through to the caller's. With no inputs every input moves.
func FlakeUpdate(flakeDir string, inputs ...string) error {
	args := append([]string{"flake", "update"}, inputs...)
	args = append(args, "--flake", flakeDir)
	return shell.Run(shell.Cmd{Bin: flakeBin, Args: args, Env: flakeEnv(os.Environ())})
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

func buildFlakeRef(ref string, offline bool) (string, error) {
	args := []string{"build", ref}
	if offline {
		args = append(args, "--offline")
	}
	args = append(args, "--no-link", "--print-out-paths")
	out, err := shell.OutputLive(shell.Cmd{Bin: flakeBin, Args: args, Env: flakeEnv(os.Environ())})
	if err != nil {
		return "", err
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

func runIn(dir string, args []string) error {
	_, err := shell.Output(shell.Cmd{Bin: flakeBin, Args: args, Dir: dir, Env: flakeEnv(os.Environ())})
	return err
}

// RebuildFromFlake builds nixosConfigurations.<host>.config.system.build.nixos-rebuild
// out of the staged flake at stagingDir and returns the path to the
// resulting nixos-rebuild binary.
func RebuildFromFlake(stagingDir, host string) (string, error) {
	target := fmt.Sprintf("%s#nixosConfigurations.%s.%s", stagingDir, host, rebuildAttr)
	out, err := shell.OutputLive(shell.Cmd{
		Bin:  flakeBin,
		Args: []string{"build", target, "--no-link", "--print-out-paths"},
		Env:  flakeEnv(os.Environ()),
	})
	if err != nil {
		return "", err
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
// <nixpkgs/nixos>, independent of staging and the flake, and returns
// the path to the resulting nixos-rebuild binary.
func RebuildFromChannel() (string, error) {
	out, err := shell.OutputLive(shell.Cmd{Bin: buildBin, Args: []string{rebuildChannelExpr, "-A", rebuildAttr, "--no-out-link"}})
	if err != nil {
		return "", err
	}
	outPath := strings.TrimSpace(string(out))
	if outPath == "" {
		return "", fmt.Errorf("%s %s -A %s: no output path printed", buildBin, rebuildChannelExpr, rebuildAttr)
	}
	return filepath.Join(outPath, rebuildBinRelpath), nil
}

// ShowHardwareConfig returns the hardware-configuration.nix that
// nixos-generate-config generates for this computer.
func ShowHardwareConfig() ([]byte, error) {
	out, err := shell.OutputLive(shell.Cmd{Bin: hardwareConfigBin, Args: hardwareConfigArgs})
	line := strings.Join(append([]string{hardwareConfigBin}, hardwareConfigArgs...), " ")
	if err != nil {
		return nil, err
	}
	if len(bytes.TrimSpace(out)) == 0 {
		return nil, fmt.Errorf("%s: no output", line)
	}
	return out, nil
}
