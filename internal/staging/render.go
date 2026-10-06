package staging

import (
	"errors"

	"github.com/DeprecatedLuar/luxos/internal/config"
	"github.com/DeprecatedLuar/luxos/internal/templates"
)

const (
	configurationTemplate = "configuration.nix.tmpl"
	bootstrapTemplate     = "bootstrap.nix.tmpl"
)

const stagedSettingsImport = "../" + configDir + "/" + config.SettingsDir + "/"

type hostData struct {
	Host string
}

type configurationData struct {
	Host     string
	Settings []string
}

// configuration renders framework/configuration.nix: every import of the
// stage, ending with the staged settings files of the units in settings.
func configuration(host string, settings []string) ([]byte, error) {
	imports := make([]string, len(settings))
	for i, name := range settings {
		imports[i] = stagedSettingsImport + name + ".nix"
	}
	return templates.Render(configurationTemplate, configurationData{Host: host, Settings: imports})
}

// flakeBootstrap renders framework/flake-file.nix, the module flake-file
// evaluates to write flake.nix: the host's nixosSystem wiring.
func flakeBootstrap(host string) ([]byte, error) {
	if host == "" {
		return nil, errors.New("staging.flakeBootstrap: host name is required")
	}
	return templates.Render(bootstrapTemplate, hostData{Host: host})
}
