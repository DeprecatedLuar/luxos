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
	out, err := storePath(shell.Cmd{
		Bin:  flakeBin,
		Args: []string{"build", target, "--no-link", "--print-out-paths"},
		Env:  flakeEnv(os.Environ()),
	})
	if err != nil {
		return "", err
	}
	return filepath.Join(out, rebuildBinRelpath), nil
}

// RebuildFromChannel builds nixos-rebuild from the channel-based
// <nixpkgs/nixos>, independent of staging and the flake, and returns
// the path to the resulting nixos-rebuild binary.
func RebuildFromChannel() (string, error) {
	out, err := storePath(shell.Cmd{Bin: buildBin, Args: []string{rebuildChannelExpr, "-A", rebuildAttr, "--no-out-link"}})
	if err != nil {
		return "", err
	}
	return filepath.Join(out, rebuildBinRelpath), nil
}

// storePath runs c, its stderr on the terminal, and returns the store path it prints.
func storePath(c shell.Cmd) (string, error) {
	out, err := shell.OutputLive(c)
	if err != nil {
		return "", err
	}
	path := strings.TrimSpace(string(out))
	if path == "" {
		return "", fmt.Errorf("%s: no output path printed", c)
	}
	return path, nil
}

// ShowHardwareConfig returns the hardware-configuration.nix that
// nixos-generate-config generates for this computer.
func ShowHardwareConfig() ([]byte, error) {
	c := shell.Cmd{Bin: hardwareConfigBin, Args: hardwareConfigArgs}
	out, err := shell.OutputLive(c)
	if err != nil {
		return nil, err
	}
	if len(bytes.TrimSpace(out)) == 0 {
		return nil, fmt.Errorf("%s: no output", c)
	}
	return out, nil
}
