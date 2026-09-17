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
	moduleDescription  = "manage modules under CONFIG_DIR/modules"
	userDescription    = "same verbs as module, fixed to modules/users"
	shellDescription   = "exec nix-shell"
)

// rootPage is the top-level usage text, ported from help.sh's cmd_help
// (extended with rebuild and shell, added ahead of this port).
func rootPage() *gohelp.Page {
	return gohelp.NewPage(binaryName, rootDescription).
		Usage(binaryName+" <command> [args]").
		Section("Commands",
			gohelp.Item("help", "Show this message"),
			gohelp.Item("rebuild [flags] [nixos-rebuild args]", "Rebuild the system from CONFIG_DIR"),
			gohelp.Item("module|modules list|ls [category-path]", "List modules (grouped by category)"),
			gohelp.Item("module|modules add|a <category/name> [--enable]", "Scaffold a module"),
			gohelp.Item("module|modules edit|e <name>", "Open a module in $EDITOR"),
			gohelp.Item("module|modules enable <name>", "Enable a module on this host"),
			gohelp.Item("module|modules disable <name>", "Disable a module on this host"),
			gohelp.Item("module|modules remove|rm <name> [-y]", "Delete a module everywhere it's imported"),
			gohelp.Item("module|modules rename|rn <old> <new>", "Rename a module's identity"),
			gohelp.Item("user|users ...", "Same verbs as module, fixed to modules/users"),
			gohelp.Item("shell [args]", "Exec nix-shell"),
		)
}

// rebuildPage documents `luxos rebuild`'s flags.
func rebuildPage() *gohelp.Page {
	return gohelp.NewPage("rebuild", rebuildDescription).
		Usage(binaryName+" rebuild [flags] [nixos-rebuild args]").
		Section("Flags",
			gohelp.Item("--bypass", "Skip staging; use the channel-based nixos-rebuild"),
			gohelp.Item("--update-lock", "Update flake.lock before building"),
			gohelp.Item("--prune", "Remove unresolvable imports on the active host"),
			gohelp.Item("--machine <name>", "Override the hostname lookup"),
		)
}

// modulePage documents every `luxos module` verb.
func modulePage() *gohelp.Page {
	return gohelp.NewPage("module", moduleDescription).
		Usage(binaryName+" module <verb> ...").
		Section("Commands",
			gohelp.Item("list|ls [category-path]", "List modules (grouped by category)"),
			gohelp.Item("add|a <category/name> [--enable]", "Scaffold a module"),
			gohelp.Item("edit|e <name>", "Open a module in $EDITOR"),
			gohelp.Item("enable <name>", "Enable a module on this host"),
			gohelp.Item("disable <name>", "Disable a module on this host"),
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
			gohelp.Item("enable <name>", "Enable a user on this host"),
			gohelp.Item("disable <name>", "Disable a user on this host"),
			gohelp.Item("remove|rm <name> [-y]", "Delete a user everywhere it's imported"),
			gohelp.Item("rename|rn <old> <new> [-y]", "Rename a user's identity"),
		).
		Text("Every verb here is `luxos module <verb>` with the category fixed to users/.")
}

// shellPage documents `luxos shell`.
func shellPage() *gohelp.Page {
	return gohelp.NewPage("shell", shellDescription).
		Usage(binaryName + " shell [args]")
}

// Run routes luxos's help output: `luxos help`, `luxos help <topic>`,
// `luxos help --all`, `-h`/`--help` and no args all funnel through here.
// args is the full argv tail. An unknown topic comes back as a non-nil
// error; the caller prints and exits like any other command error (G10).
func Run(args []string) error {
	return gohelp.Run(args, rootPage(), rebuildPage(), modulePage(), userPage(), shellPage())
}
