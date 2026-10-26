package staging

import (
	"path/filepath"

	"github.com/DeprecatedLuar/luxos/internal/config"
	"github.com/DeprecatedLuar/luxos/internal/templates"
)

const (
	configurationTemplate = "configuration.nix.tmpl"
	// frameworkUp is framework/configuration.nix's way back to the stage root.
	frameworkUp = "../"
)

type configurationData struct {
	Host     string
	Settings []string
}

// configuration renders framework/configuration.nix: every import of the
// stage, ending with the staged settings files of the units in settings.
func configuration(host string, settings []string) ([]byte, error) {
	imports := make([]string, len(settings))
	for i, name := range settings {
		imports[i] = frameworkUp + filepath.ToSlash(config.SettingsPath(configDir, name))
	}
	return templates.Render(configurationTemplate, configurationData{Host: host, Settings: imports})
}
