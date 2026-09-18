// Package generate renders configuration.nix and flake.nix from
// channels.toml and the module walk, and checks flake.lock for drift
// against the channels/flakes it declares. configuration.nix's imports are
// fixed by the host folder layout (L9): it no longer reads per-machine
// values out of the host folder. Ported from configgen::generate,
// configgen::generate_flake and _configgen_warn_lock_drift (see
// implementation-plan.md §4 bash-to-Go map).
package generate

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"text/template"

	"github.com/DeprecatedLuar/luxos/internal/config"
)

// nixSystem is the platform every generated flake builds for.
const nixSystem = "x86_64-linux"

// baseInputName is the flake input name the base channel is always emitted
// under (system.nix references inputs.nixpkgs directly).
const baseInputName = "nixpkgs"

// shadow is one hard-shadowed command, rendered into configuration.nix's
// environment.systemPackages.
type shadow struct {
	Name string
	Args string
	Real string
}

// hardShadows is the fixed set of hard shadows configuration.nix installs.
var hardShadows = []shadow{
	{Name: "nixos-rebuild", Args: "rebuild", Real: "${config.system.build.nixos-rebuild}/bin/nixos-rebuild"},
	{Name: "nixos", Args: "", Real: ""},
}

//go:embed templates/*.tmpl
var templatesFS embed.FS

var (
	configurationTmpl = template.Must(template.New("configuration.nix.tmpl").ParseFS(templatesFS, "templates/configuration.nix.tmpl"))
	flakeTmpl         = template.Must(template.New("flake.nix.tmpl").ParseFS(templatesFS, "templates/flake.nix.tmpl"))
)

// configurationData feeds templates/configuration.nix.tmpl.
type configurationData struct {
	Host        string
	Exe         string
	HardShadows []shadow
}

// Configuration renders configuration.nix for host and the absolute path to
// the luxos binary. Its imports are fixed (L9): time zone, locale and
// stateVersion come from machine.nix/.plsdonttouch.nix via those imports,
// not from any value passed here.
func Configuration(host, exe string) ([]byte, error) {
	data := configurationData{
		Host:        host,
		Exe:         exe,
		HardShadows: hardShadows,
	}

	var buf bytes.Buffer
	if err := configurationTmpl.Execute(&buf, data); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// flakeData feeds templates/flake.nix.tmpl.
type flakeData struct {
	InputsBlock string
	OutputsArgs string
	Host        string
}

// Flake renders flake.nix for host from already-loaded, validated Channels.
// The channel overlay and luxos.modules are computed in Nix, from
// framework/overlay.nix and framework/units.nix (staged alongside by
// staging.Materialize), not generated here.
func Flake(host string, c config.Channels) ([]byte, error) {
	if host == "" {
		return nil, fmt.Errorf("generate.Flake: host name is required")
	}

	var inputLines []string
	outputsArgs := "self, " + baseInputName

	for _, ch := range c.Channels {
		if ch.Name == c.Base {
			inputLines = append(inputLines, fmt.Sprintf("    %s.url = %q;  # base channel: %s", baseInputName, ch.URL, c.Base))
		}
	}
	for _, ch := range c.Channels {
		if ch.Name == c.Base {
			continue
		}
		inputLines = append(inputLines, fmt.Sprintf("    %s.url = %q;", ch.Name, ch.URL))
		outputsArgs += ", " + ch.Name
	}
	for _, f := range c.Flakes {
		inputLines = append(inputLines, fmt.Sprintf("    %s.url = %q;", f.Name, f.URL))
	}

	data := flakeData{
		InputsBlock: strings.Join(inputLines, "\n"),
		OutputsArgs: outputsArgs,
		Host:        host,
	}

	var buf bytes.Buffer
	if err := flakeTmpl.Execute(&buf, data); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// LockInputs is the full set of flake input names Flake generates, in the
// same order _configgen_warn_lock_drift compared against: the base channel
// under its fixed name, then every other declared channel, then every
// flake.
func LockInputs(c config.Channels) []string {
	inputs := []string{baseInputName}
	for _, ch := range c.Channels {
		if ch.Name != c.Base {
			inputs = append(inputs, ch.Name)
		}
	}
	for _, f := range c.Flakes {
		inputs = append(inputs, f.Name)
	}
	return inputs
}

// flakeLock is the subset of flake.lock's structure LockDrift reads.
type flakeLock struct {
	Nodes struct {
		Root struct {
			Inputs map[string]json.RawMessage `json:"inputs"`
		} `json:"root"`
	} `json:"nodes"`
}

// LockDrift compares flake.lock's root inputs against inputs (LockInputs'
// result): orphaned names are locked but no longer generated, unlocked
// names are generated but not yet locked (they will resolve to HEAD). A
// missing lock file is not drift: (nil, nil, nil).
func LockDrift(lockPath string, inputs []string) (orphaned, unlocked []string, err error) {
	data, err := os.ReadFile(lockPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, nil
		}
		return nil, nil, err
	}

	var lock flakeLock
	if err := json.Unmarshal(data, &lock); err != nil {
		return nil, nil, fmt.Errorf("%s: %w", lockPath, err)
	}

	locked := make(map[string]bool, len(lock.Nodes.Root.Inputs))
	for name := range lock.Nodes.Root.Inputs {
		locked[name] = true
	}

	generated := make(map[string]bool, len(inputs))
	for _, name := range inputs {
		generated[name] = true
	}

	for name := range locked {
		if !generated[name] {
			orphaned = append(orphaned, name)
		}
	}
	for name := range generated {
		if !locked[name] {
			unlocked = append(unlocked, name)
		}
	}

	sort.Strings(orphaned)
	sort.Strings(unlocked)

	return orphaned, unlocked, nil
}
