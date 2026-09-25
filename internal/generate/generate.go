// Package generate renders the per-host Nix files staged next to the
// embedded framework: configuration.nix, and flake-file.nix (the module
// flake-file evaluates to write flake.nix, holding the host's nixosSystem
// wiring). configuration.nix's imports are fixed by the host folder layout
// (L9): it does not read per-machine values out of the host folder.
package generate

import (
	"bytes"
	"embed"
	"fmt"
	"text/template"
)

// shadow is one hard-shadowed command, rendered into configuration.nix's
// environment.systemPackages.
type shadow struct {
	Name string
	Args string
}

// hardShadows is the fixed set of hard shadows configuration.nix installs.
var hardShadows = []shadow{
	{Name: "nixos-rebuild", Args: "rebuild"},
	{Name: "nixos", Args: ""},
}

//go:embed templates/*.tmpl
var templatesFS embed.FS

var (
	configurationTmpl = template.Must(template.New("configuration.nix.tmpl").ParseFS(templatesFS, "templates/configuration.nix.tmpl"))
	bootstrapTmpl     = template.Must(template.New("bootstrap.nix.tmpl").ParseFS(templatesFS, "templates/bootstrap.nix.tmpl"))
)

// configurationData feeds templates/configuration.nix.tmpl.
type configurationData struct {
	Host        string
	HardShadows []shadow
}

// Configuration renders configuration.nix for host. Its imports are fixed (L9): time zone, locale and
// stateVersion come from machine.nix/.plsdonttouch.nix via those imports,
// not from any value passed here.
func Configuration(host string) ([]byte, error) {
	data := configurationData{
		Host:        host,
		HardShadows: hardShadows,
	}

	var buf bytes.Buffer
	if err := configurationTmpl.Execute(&buf, data); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// bootstrapData feeds templates/bootstrap.nix.tmpl.
type bootstrapData struct {
	Host string
}

// FlakeBootstrap renders flake-file.nix for host: the module flake.nix
// (embedded, static) loads through flake-file. It imports the host's
// selection, declares the bootstrap inputs and defines
// nixosConfigurations.<host>. The channel overlay and luxos.modules are
// computed in Nix from framework/overlay.nix and framework/units.nix.
func FlakeBootstrap(host string) ([]byte, error) {
	if host == "" {
		return nil, fmt.Errorf("generate.FlakeBootstrap: host name is required")
	}

	var buf bytes.Buffer
	if err := bootstrapTmpl.Execute(&buf, bootstrapData{Host: host}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
