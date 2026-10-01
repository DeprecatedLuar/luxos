package staging

import (
	"fmt"

	"github.com/DeprecatedLuar/luxos/internal/templates"
)

type configurationData struct {
	Host string
}

// configuration renders framework/configuration.nix: the host's imports are
// fixed by the host folder layout.
func configuration(host string) ([]byte, error) {
	return templates.Render("configuration.nix.tmpl", configurationData{Host: host})
}

type bootstrapData struct {
	Host string
}

// flakeBootstrap renders framework/flake-file.nix, the module flake-file
// evaluates to write flake.nix: the host's nixosSystem wiring.
func flakeBootstrap(host string) ([]byte, error) {
	if host == "" {
		return nil, fmt.Errorf("staging.flakeBootstrap: host name is required")
	}
	return templates.Render("bootstrap.nix.tmpl", bootstrapData{Host: host})
}
