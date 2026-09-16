package config

import (
	"fmt"
	"os"
	"regexp"
	"sort"

	"github.com/BurntSushi/toml"
)

// channelNameRegex is the shape a channel/flake name must match to become a
// Nix identifier (flake input name, overlay attribute).
var channelNameRegex = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_-]*$`)

// baseInputName is the flake input name the base channel is always emitted
// under; a channel or flake may not claim it, or "self".
const baseInputName = "nixpkgs"

// reservedNames are names the generator emits itself; a channel/flake name
// may not claim one of these.
var reservedNames = []string{"self", baseInputName}

// Input is one declared flake input: a name and its flake URL.
type Input struct {
	Name string
	URL  string
}

// Channels is the decoded, validated content of channels.toml.
type Channels struct {
	Base     string
	Channels []Input // sorted by Name
	Flakes   []Input // sorted by Name
}

// rawChannels is the shape channels.toml decodes into before validation.
type rawChannels struct {
	Base     string            `toml:"base"`
	Channels map[string]string `toml:"channels"`
	Flakes   map[string]string `toml:"flakes"`
}

// LoadChannels decodes and validates the channels.toml at path.
func LoadChannels(path string) (Channels, error) {
	if _, err := os.Stat(path); err != nil {
		return Channels{}, fmt.Errorf("%s not found\n  create it from templates/channels.toml in the luxos repo", path)
	}

	var raw rawChannels
	meta, err := toml.DecodeFile(path, &raw)
	if err != nil {
		return Channels{}, err
	}

	for _, key := range meta.Undecoded() {
		return Channels{}, fmt.Errorf("unknown top-level key '%s' in %s", key.String(), path)
	}

	if raw.Base == "" {
		return Channels{}, fmt.Errorf("'base' is required in %s\n  It names the channel that builds the system (provides lib).", path)
	}

	channels, err := toValidatedInputs(raw.Channels, path)
	if err != nil {
		return Channels{}, err
	}

	flakes, err := toValidatedInputs(raw.Flakes, path)
	if err != nil {
		return Channels{}, err
	}

	if len(channels) == 0 {
		return Channels{}, fmt.Errorf("%s declares no [channels] entry", path)
	}

	if dupes := duplicateNames(channels, flakes); len(dupes) > 0 {
		msg := fmt.Sprintf("name declared more than once in %s:", path)
		for _, d := range dupes {
			msg += "\n  - " + d
		}
		return Channels{}, fmt.Errorf("%s", msg)
	}

	if !hasName(channels, raw.Base) {
		return Channels{}, fmt.Errorf("base = \"%s\" is not a declared [channels] entry in %s", raw.Base, path)
	}

	return Channels{Base: raw.Base, Channels: channels, Flakes: flakes}, nil
}

// toValidatedInputs converts a decoded map into a name-sorted, validated
// slice of Input.
func toValidatedInputs(m map[string]string, path string) ([]Input, error) {
	names := make([]string, 0, len(m))
	for name := range m {
		names = append(names, name)
	}
	sort.Strings(names)

	inputs := make([]Input, 0, len(names))
	for _, name := range names {
		if err := validateName(name, path); err != nil {
			return nil, err
		}
		inputs = append(inputs, Input{Name: name, URL: m[name]})
	}
	return inputs, nil
}

// validateName rejects a channel/flake name that can't be a flake input or
// an overlay attribute.
func validateName(name, path string) error {
	if !channelNameRegex.MatchString(name) {
		return fmt.Errorf("'%s' in %s is not a valid Nix identifier\n  Names must match %s", name, path, channelNameRegex.String())
	}
	for _, reserved := range reservedNames {
		if name == reserved {
			return fmt.Errorf("'%s' in %s is reserved — the generator emits it itself", name, path)
		}
	}
	return nil
}

// duplicateNames returns, sorted, every name declared in both channels and
// flakes.
func duplicateNames(channels, flakes []Input) []string {
	seen := make(map[string]bool, len(channels))
	for _, c := range channels {
		seen[c.Name] = true
	}
	var dupes []string
	for _, f := range flakes {
		if seen[f.Name] {
			dupes = append(dupes, f.Name)
		}
	}
	sort.Strings(dupes)
	return dupes
}

// hasName reports whether inputs contains an entry named name.
func hasName(inputs []Input, name string) bool {
	for _, in := range inputs {
		if in.Name == name {
			return true
		}
	}
	return false
}
