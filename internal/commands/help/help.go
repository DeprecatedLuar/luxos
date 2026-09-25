// Package help renders luxos's help pages via github.com/DeprecatedLuar/
// gohelp-luar, adapted from bin/lib/lux/commands/help.sh (implementation-
// plan.md Phase 11 step 3b). No other command defines help text
// (implementation-plan.md G13).
package help

import (
	gohelp "github.com/DeprecatedLuar/gohelp-luar"
)

// binaryName is the CLI's own name, used in the root page's usage line.
const binaryName = "luxos"

const (
	rootDescription    = "manages NixOS machine configuration"
	rebuildDescription = "rebuild the system from CONFIG_DIR"
	flakeDescription   = "update the host's flake inputs"
	moduleDescription  = "manage modules under CONFIG_DIR/modules"
	userDescription    = "same verbs as module, fixed to modules/users"
	shellDescription   = "exec nix-shell"

	environmentDescription = "syntax of CONFIG_DIR/environment"
)

// rootPage is the top-level usage text, ported from help.sh's cmd_help
// (extended with rebuild and shell, added ahead of this port).
func rootPage() *gohelp.Page {
	return gohelp.NewPage(binaryName, rootDescription).
		Usage(binaryName+" <command> [args]").
		Section("Commands",
			gohelp.Item("help", "Show this message"),
			gohelp.Item("rebuild [flags] [nixos-rebuild args]", "Rebuild the system from CONFIG_DIR"),
			gohelp.Item("flake update [inputs...]", "Update the host's flake inputs"),
			gohelp.Item("module|modules list|ls [category-path]", "List modules (grouped by category); bare 'modules', or top-level 'list'/'ls', is a shortcut for this"),
			gohelp.Item("module|modules add|a <category/name> [--enable]", "Scaffold a module; local/<name> creates a module private to the active machine"),
			gohelp.Item("module|modules edit|e <name>", "Open a module in $EDITOR; top-level 'edit' is a shortcut for this"),
			gohelp.Item("module|modules enable <name>...", "Enable one or more modules on this host; top-level 'enable' is a shortcut for this"),
			gohelp.Item("module|modules disable <name>...", "Disable one or more modules on this host; top-level 'disable' is a shortcut for this"),
			gohelp.Item("module|modules remove|rm <name> [-y]", "Delete a module everywhere it's imported; top-level 'remove'/'rm' is a shortcut for this"),
			gohelp.Item("module|modules rename|rn <old> <new>", "Rename a module's identity; top-level 'rename'/'rn' is a shortcut for this"),
			gohelp.Item("user|users ...", "Same verbs as module, fixed to modules/users; bare 'users' is a shortcut for 'user list'"),
			gohelp.Item("shell [args]", "Exec nix-shell"),
			gohelp.Item("help environment", "Syntax of CONFIG_DIR/environment"),
		)
}

// rebuildPage documents `luxos rebuild`'s flags.
func rebuildPage() *gohelp.Page {
	return gohelp.NewPage("rebuild", rebuildDescription).
		Usage(binaryName+" rebuild [flags] [nixos-rebuild args]").
		Section("Flags",
			gohelp.Item("--bypass", "Skip staging; use the channel-based nixos-rebuild"),
			gohelp.Item("--update-lock", "Update every flake input to its latest revision (new inputs are locked automatically without it)"),
			gohelp.Item("--prune", "Remove unresolvable imports on the active host"),
			gohelp.Item("--machine <name>", "Override the hostname lookup"),
			gohelp.Item("--config|-C <dir>", "Build from another luxos config folder (sets LUXOS_CONFIG_DIR)"),
		)
}

// flakePage documents `luxos flake`.
func flakePage() *gohelp.Page {
	return gohelp.NewPage("flake", flakeDescription).
		Usage(binaryName+" flake update [inputs...]").
		Section("Flags",
			gohelp.Item("--machine <name>", "Override the hostname lookup"),
			gohelp.Item("--config|-C <dir>", "Use another luxos config folder (sets LUXOS_CONFIG_DIR)"),
		).
		Text("With no input names, every input is updated. Names are the input names declared by your modules, e.g. luxos, nixpkgs, unstable. Transitive inputs are addressed by path, e.g. ambxst/axctl.")
}

// modulePage documents every `luxos module` verb.
func modulePage() *gohelp.Page {
	return gohelp.NewPage("module", moduleDescription).
		Usage(binaryName+" module <verb> ...").
		Section("Commands",
			gohelp.Item("list|ls [category-path]", "List modules (grouped by category)"),
			gohelp.Item("add|a <category/name> [--enable]", "Scaffold a module; local/<name> creates a module private to the active machine"),
			gohelp.Item("edit|e <name>", "Open a module in $EDITOR"),
			gohelp.Item("enable <name>...", "Enable one or more modules on this host"),
			gohelp.Item("disable <name>...", "Disable one or more modules on this host"),
			gohelp.Item("remove|rm <name> [-y]", "Delete a module everywhere it's imported"),
			gohelp.Item("rename|rn <old> <new> [-y]", "Rename a module's identity"),
		)
}

// userPage documents `luxos user`, the same verbs as module fixed to
// modules/users.
func userPage() *gohelp.Page {
	return gohelp.NewPage("user", userDescription).
		Usage(binaryName+" user <verb> ...").
		Section("Commands",
			gohelp.Item("add|a <name> [--enable]", "Scaffold modules/users/<name>"),
			gohelp.Item("list|ls [subpath]", "List users (or a users/ subcategory)"),
			gohelp.Item("edit|e <name>", "Open a user module in $EDITOR"),
			gohelp.Item("enable <name>...", "Enable one or more users on this host"),
			gohelp.Item("disable <name>...", "Disable one or more users on this host"),
			gohelp.Item("remove|rm <name> [-y]", "Delete a user everywhere it's imported"),
			gohelp.Item("rename|rn <old> <new> [-y]", "Rename a user's identity"),
		).
		Text("Every verb here is `luxos module <verb>` with the category fixed to users/. Users are always shared across every host — there is no local/ equivalent for users.")
}

// shellPage documents `luxos shell`.
func shellPage() *gohelp.Page {
	return gohelp.NewPage("shell", shellDescription).
		Usage(binaryName + " shell [args]")
}

// environmentPage documents the syntax of CONFIG_DIR/environment.
func environmentPage() *gohelp.Page {
	return gohelp.NewPage("environment", environmentDescription).
		Usage("CONFIG_DIR/environment").
		Section("Syntax",
			gohelp.Item("KEY=VALUE", "One variable per line; blank lines and # comments are ignored"),
			gohelp.Item("export KEY=VALUE", "The export prefix is accepted"),
			gohelp.Item("KEY=\"value\"", "Double quotes: spaces allowed, $ references expand"),
			gohelp.Item("KEY='value'", "Single quotes: everything literal"),
			gohelp.Item("KEY=value # note", "Inline comment after whitespace"),
			gohelp.Item("$HOME, $USER", "Expanded per user at login"),
			gohelp.Item("$KEY, ${KEY}", "A key defined earlier in the file"),
		).
		Section("Fails the build",
			gohelp.Item("Reserved keys", "HOME USER LOGNAME SHELL XDG_RUNTIME_DIR XDG_SESSION_ID XDG_SESSION_TYPE XDG_SEAT XDG_VTNR DISPLAY WAYLAND_DISPLAY DBUS_SESSION_BUS_ADDRESS PATH"),
			gohelp.Item("Unknown $VAR", "Only $HOME, $USER and earlier keys"),
			gohelp.Item("\\ or backtick", "Outside single quotes"),
			gohelp.Item("\" or @{", "Anywhere in a value"),
			gohelp.Item("Duplicate key", "Each key once"),
			gohelp.Item("Other syntax", "Spaces around =, unquoted spaces, mixed quoting"),
		).
		Text("Applied to every host as environment.sessionVariables. A module setting the same key to a different value fails the build; lib.mkForce in a module overrides this file. Your shell rc runs later and overrides it in that shell. Delete the file and the next rebuild restores the template; empty it to set nothing.")
}

// Run routes luxos's help output: `luxos help`, `luxos help <topic>`,
// `luxos help --all`, `-h`/`--help` and no args all funnel through here.
// args is the full argv tail. An unknown topic comes back as a non-nil
// error; the caller prints and exits like any other command error (G10).
func Run(args []string) error {
	return gohelp.Run(args, rootPage(), rebuildPage(), flakePage(), modulePage(), userPage(), shellPage(), environmentPage())
}
