// Package generate renders configuration.nix, flake.nix and a host's
// default.nix from machine.toml/channels.toml and the module walk, and
// checks flake.lock for drift against the channels/flakes it declares.
// Ported from configgen::generate, configgen::generate_default,
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
	"github.com/DeprecatedLuar/luxos/internal/units"
)

// nixSystem is the platform every generated flake builds for.
const nixSystem = "x86_64-linux"

// baseInputName is the flake input name the base channel is always emitted
// under (system.nix references inputs.nixpkgs directly).
const baseInputName = "nixpkgs"

// modulesDirName is the shared modules folder name, at CONFIG_DIR root and
// under the staged config/ tree.
const modulesDirName = "modules"

// hostDefaultSkip are host-directory entries HostDefault never imports:
// machine.toml isn't a Nix file, default.nix is the file being generated,
// and modules/ is already wired in directly by Configuration.
var hostDefaultSkip = map[string]bool{
	"machine.toml": true,
	"default.nix":  true,
	modulesDirName: true,
}

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
	defaultTmpl       = template.Must(template.New("default.nix.tmpl").ParseFS(templatesFS, "templates/default.nix.tmpl"))
)

// configurationData feeds templates/configuration.nix.tmpl.
type configurationData struct {
	TimeZone     string
	Locale       string
	StateVersion string
	Host         string
	Exe          string
	HardShadows  []shadow
}

// Configuration renders configuration.nix for host from an already-loaded,
// validated Machine and the absolute path to the luxos binary.
func Configuration(host string, m config.Machine, exe string) ([]byte, error) {
	data := configurationData{
		TimeZone:     m.TimeZone,
		Locale:       m.Locale,
		StateVersion: m.StateVersion,
		Host:         host,
		Exe:          exe,
		HardShadows:  hardShadows,
	}

	var buf bytes.Buffer
	if err := configurationTmpl.Execute(&buf, data); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// flakeData feeds templates/flake.nix.tmpl.
type flakeData struct {
	InputsBlock  string
	OutputsArgs  string
	OverlayBlock string
	UnitsBlock   string
	Host         string
}

// Flake renders flake.nix for host from already-loaded, validated Channels
// and the module walk (units.Walk's result, sorted by Name).
func Flake(host string, c config.Channels, unitList []units.Unit) ([]byte, error) {
	if host == "" {
		return nil, fmt.Errorf("generate.Flake: host name is required")
	}

	var inputLines, overlayLines []string
	outputsArgs := "self, " + baseInputName

	for _, ch := range c.Channels {
		if ch.Name == c.Base {
			inputLines = append(inputLines, fmt.Sprintf("    %s.url = %q;  # base channel: %s", baseInputName, ch.URL, c.Base))
			overlayLines = append(overlayLines, fmt.Sprintf("      %s = prev;", c.Base))
		}
	}
	for _, ch := range c.Channels {
		if ch.Name == c.Base {
			continue
		}
		inputLines = append(inputLines, fmt.Sprintf("    %s.url = %q;", ch.Name, ch.URL))
		overlayLines = append(overlayLines, fmt.Sprintf("      %s = import %s { inherit (prev.stdenv.hostPlatform) system; inherit (prev) config; };", ch.Name, ch.Name))
		outputsArgs += ", " + ch.Name
	}
	for _, f := range c.Flakes {
		inputLines = append(inputLines, fmt.Sprintf("    %s.url = %q;", f.Name, f.URL))
	}

	var unitLines []string
	for _, u := range unitList {
		unitLines = append(unitLines, fmt.Sprintf("      %q = ./config/%s/%s;", u.Name, modulesDirName, u.Path))
	}

	data := flakeData{
		InputsBlock:  strings.Join(inputLines, "\n"),
		OutputsArgs:  outputsArgs,
		OverlayBlock: strings.Join(overlayLines, "\n"),
		UnitsBlock:   strings.Join(unitLines, "\n"),
		Host:         host,
	}

	var buf bytes.Buffer
	if err := flakeTmpl.Execute(&buf, data); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// defaultData feeds templates/default.nix.tmpl.
type defaultData struct {
	ImportsBlock string
}

// HostDefault renders a host's own default.nix: every plain .nix file at
// machineDir's root, plus any root-level directory carrying its own
// default.nix, imported by name — excluding machine.toml, default.nix and
// modules.
func HostDefault(machineDir string) ([]byte, error) {
	entries, err := os.ReadDir(machineDir)
	if err != nil {
		return nil, err
	}

	var names []string
	for _, e := range entries {
		name := e.Name()
		if hostDefaultSkip[name] {
			continue
		}

		info, err := os.Stat(machineDir + "/" + name)
		if err != nil {
			// Vanished mid-walk or a broken symlink: skip it.
			continue
		}

		if info.IsDir() {
			if _, err := os.Stat(machineDir + "/" + name + "/default.nix"); err != nil {
				continue
			}
			names = append(names, name)
			continue
		}

		if strings.HasSuffix(name, ".nix") {
			names = append(names, name)
		}
	}

	sort.Strings(names)

	var importsBlock string
	if len(names) > 0 {
		lines := make([]string, len(names))
		for i, n := range names {
			lines[i] = "    ./" + n
		}
		importsBlock = "\n" + strings.Join(lines, "\n") + "\n  "
	}

	data := defaultData{ImportsBlock: importsBlock}

	var buf bytes.Buffer
	if err := defaultTmpl.Execute(&buf, data); err != nil {
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
