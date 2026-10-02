package staging

import (
	"errors"

	"github.com/DeprecatedLuar/luxos/internal/templates"
)

const (
	configurationTemplate = "configuration.nix.tmpl"
	bootstrapTemplate     = "bootstrap.nix.tmpl"
)

type hostData struct {
	Host string
}

// configuration renders framework/configuration.nix: the host's imports are
// fixed by the host folder layout.
func configuration(host string) ([]byte, error) {
	return templates.Render(configurationTemplate, hostData{Host: host})
}

// flakeBootstrap renders framework/flake-file.nix, the module flake-file
// evaluates to write flake.nix: the host's nixosSystem wiring.
func flakeBootstrap(host string) ([]byte, error) {
	if host == "" {
		return nil, errors.New("staging.flakeBootstrap: host name is required")
	}
	return templates.Render(bootstrapTemplate, hostData{Host: host})
}
