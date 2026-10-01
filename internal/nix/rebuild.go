package nix

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/DeprecatedLuar/luxos/internal/shell"
)

const buildBin = "nix-build"

const hardwareConfigBin = "nixos-generate-config"

var hardwareConfigArgs = []string{"--show-hardware-config"}

const rebuildAttr = "config.system.build.nixos-rebuild"

const rebuildBinRelpath = "bin/nixos-rebuild"

const rebuildChannelExpr = "<nixpkgs/nixos>"

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
