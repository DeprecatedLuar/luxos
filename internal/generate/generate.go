// Package generate renders the per-host Nix files staged next to the
// embedded framework: configuration.nix, and flake-file.nix (the module
// flake-file evaluates to write flake.nix, holding the host's nixosSystem
// wiring). configuration.nix's imports are fixed by the host folder layout
// (L9): it does not read per-machine values out of the host folder.
package generate

import (
	"fmt"

	"github.com/DeprecatedLuar/luxos/internal/templates"
)

type configurationData struct {
	Host string
}

func Configuration(host string) ([]byte, error) {
	return templates.Render("configuration.nix.tmpl", configurationData{Host: host})
}

type bootstrapData struct {
	Host string
}

func FlakeBootstrap(host string) ([]byte, error) {
	if host == "" {
		return nil, fmt.Errorf("generate.FlakeBootstrap: host name is required")
	}
	return templates.Render("bootstrap.nix.tmpl", bootstrapData{Host: host})
}
