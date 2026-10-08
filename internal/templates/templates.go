// Package templates holds every file luxos embeds: framework files copied into
// the stage, system modules synced into CONFIG_DIR, starter files written once
// when missing, and text templates rendered with values.
package templates

import (
	"bytes"
	"embed"
	"io/fs"
	"text/template"
)

const renderDir = "render/"

//go:embed all:framework all:modules all:starters all:render
var files embed.FS

// File returns the embedded file at name, e.g. "starters/user/default.nix".
func File(name string) ([]byte, error) {
	return files.ReadFile(name)
}

// Dir returns the embedded directory dir as a file system rooted at it.
func Dir(dir string) (fs.FS, error) {
	return fs.Sub(files, dir)
}

// Render fills the template render/<name> with data.
func Render(name string, data any) ([]byte, error) {
	tmpl, err := template.ParseFS(files, renderDir+name)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
