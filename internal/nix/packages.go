package nix

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/DeprecatedLuar/luxos/internal/shell"
)

// packagesAttrFormat addresses a host's configuration in a flake directory.
const packagesAttrFormat = "%s#nixosConfigurations.%s"

// packagesExpr maps a NixOS configuration to the environment.systemPackages
// definitions made by files under the stage's config/, paths relative to it.
// A package's source is the root input whose store path holds its
// meta.position. It never reads outPath: that forces every derivation.
const packagesExpr = `cfg:
let
  inputs = cfg._module.specialArgs.inputs;
  hasPrefix = pre: s: builtins.substring 0 (builtins.stringLength pre) s == pre;
  root = toString inputs.self.outPath + "/config/";
  sources = builtins.mapAttrs (_: i: toString i.outPath + "/") (builtins.removeAttrs inputs [ "self" ]);
  sourceOf = pos:
    let hits = builtins.filter (n: hasPrefix sources.${n} pos) (builtins.attrNames sources);
    in if pos == null || hits == [ ] then null else builtins.head hits;
  package = p: {
    name = p.name or null;
    pname = p.pname or null;
    version = p.version or null;
    source = sourceOf (p.meta.position or null);
  };
in
map (d: {
  file = builtins.substring (builtins.stringLength root) (-1) d.file;
  packages = map package (builtins.filter builtins.isAttrs d.value);
}) (builtins.filter (d: hasPrefix root d.file)
  cfg.options.environment.systemPackages.definitionsWithLocations)`

// PackageDefs is one config file's systemPackages definition.
type PackageDefs struct {
	File     string    `json:"file"` // relative to the stage's config/
	Packages []Package `json:"packages"`
}

// Package is one declared package; a field Nix had no value for is "".
type Package struct {
	Name    string `json:"name"`
	Pname   string `json:"pname"`
	Version string `json:"version"`
	Source  string `json:"source"` // root input holding its meta.position
}

// Packages evaluates host's configuration in the flake at flakeDir and returns
// the systemPackages definitions of its config/ files, in definition order.
func Packages(flakeDir, host string) ([]PackageDefs, error) {
	out, err := shell.Output(shell.Cmd{
		Bin:  flakeBin,
		Args: []string{"eval", "--json", fmt.Sprintf(packagesAttrFormat, flakeDir, host), "--apply", packagesExpr},
		Env:  flakeEnv(os.Environ()),
	})
	if err != nil {
		return nil, err
	}
	return decodePackages(out)
}

func decodePackages(data []byte) ([]PackageDefs, error) {
	var defs []PackageDefs
	if err := json.Unmarshal(data, &defs); err != nil {
		return nil, fmt.Errorf("decoding package definitions: %w", err)
	}
	return defs, nil
}

// hostsAttrFormat addresses the host configurations of a flake directory.
const hostsAttrFormat = "path:%s#nixosConfigurations"

// hostsApply lists the attribute names without evaluating any configuration.
const hostsApply = "builtins.attrNames"

// Hosts returns the host names the flake at flakeDir defines.
func Hosts(flakeDir string) ([]string, error) {
	out, err := shell.Output(shell.Cmd{
		Bin:  flakeBin,
		Args: []string{"eval", "--json", fmt.Sprintf(hostsAttrFormat, flakeDir), "--apply", hostsApply},
		Env:  flakeEnv(os.Environ()),
	})
	if err != nil {
		return nil, err
	}
	var hosts []string
	if err := json.Unmarshal(out, &hosts); err != nil {
		return nil, fmt.Errorf("decoding host names: %w", err)
	}
	return hosts, nil
}
