package nixsrc

import (
	"os"
	"regexp"
	"strings"
)

// inputDeclRe matches one flat `flake-file.inputs.<name>.url = "<url>";`
// declaration line.
var inputDeclRe = regexp.MustCompile(`^\s*flake-file\.inputs\.([A-Za-z0-9_'-]+)\.url\s*=\s*"([^"]*)"\s*;`)

// InputDecl is one flake input declared in a module's source: its name, its
// url, and the 1-based line it sits on.
type InputDecl struct {
	Name string
	URL  string
	Line int
}

// InputDecls returns the flake input declarations in file, in source order.
// It is a raw line scan, not a parse: a commented-out declaration is just a
// comment and never matches. The file is read only.
func InputDecls(file string) ([]InputDecl, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}

	var decls []InputDecl
	for i, line := range strings.Split(string(data), "\n") {
		m := inputDeclRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		decls = append(decls, InputDecl{Name: m[1], URL: m[2], Line: i + 1})
	}
	return decls, nil
}
