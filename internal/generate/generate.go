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

//go:embed templates/*.tmpl
var templatesFS embed.FS

var (
	configurationTmpl = template.Must(template.New("configuration.nix.tmpl").ParseFS(templatesFS, "templates/configuration.nix.tmpl"))
	bootstrapTmpl     = template.Must(template.New("bootstrap.nix.tmpl").ParseFS(templatesFS, "templates/bootstrap.nix.tmpl"))
)

type configurationData struct {
	Host string
}

// Configuration renders configuration.nix for host. Its imports are fixed.
func Configuration(host string) ([]byte, error) {
	data := configurationData{
		Host: host,
	}

	var buf bytes.Buffer
	if err := configurationTmpl.Execute(&buf, data); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

type bootstrapData struct {
	Host string
}

// FlakeBootstrap renders flake-file.nix for host.
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
