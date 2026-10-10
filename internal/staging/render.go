package staging

import (
	"github.com/DeprecatedLuar/luxos/internal/config"
	"github.com/DeprecatedLuar/luxos/internal/templates"
)

const (
	configurationTemplate = "configuration.nix.tmpl"
)

const stagedSettingsImport = "../" + configDir + "/" + config.SettingsDir + "/"

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
