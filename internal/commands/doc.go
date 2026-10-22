// Package commands is the application layer, one file per CLI command. A
// command parses its input, calls domain packages in order (prompting between
// calls) and maps what they return onto internal/ui; it owns no logic about
// the config.
package commands
