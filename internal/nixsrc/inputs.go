package nixsrc

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"strconv"
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

// BaseChannelInput is the name of the flake input that is the base channel.
const BaseChannelInput = "nixpkgs"

// ErrNoBaseChannel reports that file holds no literal nixpkgs declaration.
var ErrNoBaseChannel = errors.New("missing base channel")

// BaseChannel returns the url and 1-based line of the one nixpkgs declaration
// in file. None (a non-literal value does not match the declaration form, so it
// counts as none) is ErrNoBaseChannel; more than one is an error naming the lines.
func BaseChannel(file string) (url string, line int, err error) {
	decls, err := InputDecls(file)
	if err != nil {
		return "", 0, err
	}
	var found []InputDecl
	for _, d := range decls {
		if d.Name == BaseChannelInput {
			found = append(found, d)
		}
	}
	switch len(found) {
	case 0:
		return "", 0, ErrNoBaseChannel
	case 1:
		return found[0].URL, found[0].Line, nil
	}
	lines := make([]string, len(found))
	for i, d := range found {
		lines[i] = strconv.Itoa(d.Line)
	}
	return "", 0, fmt.Errorf("%s: base channel declared %d times (lines %s); keep one", file, len(found), strings.Join(lines, ", "))
}
