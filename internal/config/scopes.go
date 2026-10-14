package config

import (
	"path"
	"strings"
)

// Scope names: the per-hostname folder and the per-computer folder, each
// reached through the link of that name at the root of modules/.
const (
	ScopeShared  = ""
	ScopeHost    = "host"
	ScopeMachine = "machine"
)

const (
	ownLinePrefix    = "./"
	parentLinePrefix = "../"
	pathSep          = "/"
)

// LineOf is the selection line, in scope's selection, for the
// modules-relative path p: "./x" for a unit of scope, "../p" otherwise.
// The shared scope's selection sits at the root of modules/.
func LineOf(scope, p string) string {
	if scope == ScopeShared {
		return ownLinePrefix + p
	}
	if rest, ok := strings.CutPrefix(p, scope+pathSep); ok {
		return ownLinePrefix + rest
	}
	return parentLinePrefix + p
}

// PathOf is the modules-relative path a line of scope's selection names.
func PathOf(scope, line string) string {
	return path.Clean(path.Join(scope, line))
}
