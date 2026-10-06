package help

import (
	gohelp "github.com/DeprecatedLuar/gohelp-luar"
)

const binaryName = "luxos"

const (
	rootDescription    = "manages NixOS machine configuration"
	rebuildDescription = "rebuild the system from CONFIG_DIR"
	flakeDescription   = "list, inspect and update the host's flake inputs"
	moduleDescription  = "manage modules under CONFIG_DIR/modules"
	userDescription    = "same verbs as module, fixed to modules/users"
	shellDescription   = "exec nix-shell"

	environmentDescription = "syntax of CONFIG_DIR/environment"
	settingsDescription    = "module options and per-host settings files"
)

func rootPage() *gohelp.Page {
	return gohelp.NewPage(binaryName, rootDescription).
		Usage(binaryName+" <command> [args]").
		Section("Commands",
			gohelp.Item("help", "Show this message"),
			gohelp.Item("version|-v|--version", "Print the commit this binary was built from"),
			gohelp.Item("rebuild [flags] [nixos-rebuild args]", "Rebuild the system from CONFIG_DIR"),
			gohelp.Item("flakes|flake list|ls", "List the host's flake inputs as a tree; bare 'flakes' is a shortcut for this"),
			gohelp.Item("flake <name>...", "Show where each input comes from and what updating it would give"),
			gohelp.Item("flake update [inputs...]", "Update the host's flake inputs"),
			gohelp.Item("module|modules|m list|ls [category-path]", "List modules (grouped by category); bare 'modules', or top-level 'list'/'ls', is a shortcut for this"),
			gohelp.Item("module|modules|m add|a <category/name> [--enable]", "Scaffold a module; local/<name> creates a module private to the active machine"),
			gohelp.Item("module|modules|m edit|e <name>", "Open a module in $EDITOR; top-level 'edit' is a shortcut for this"),
			gohelp.Item("module|modules|m configure|config <name>", "Open this host's settings for a module in $EDITOR, creating or updating them first; top-level 'configure'/'config' is a shortcut for this"),
			gohelp.Item("module|modules|m enable|1 <name>...", "Enable one or more modules on this host; top-level 'enable' is a shortcut for this"),
			gohelp.Item("module|modules|m disable|0 <name>... [-y]", "Disable one or more modules on this host; top-level 'disable' is a shortcut for this"),
			gohelp.Item("module|modules|m remove|rm <name> [-y]", "Delete a module everywhere it's imported; top-level 'remove'/'rm' is a shortcut for this"),
			gohelp.Item("module|modules|m rename|rn <old> <new>", "Rename a module's identity; top-level 'rename'/'rn' is a shortcut for this"),
			gohelp.Item("user|users ...", "Same verbs as module, fixed to modules/users; bare 'users' is a shortcut for 'user list'"),
			gohelp.Item("shell [args]", "Exec nix-shell"),
			gohelp.Item("help environment", "Syntax of CONFIG_DIR/environment"),
			gohelp.Item("help settings", "Module options and per-host settings files"),
		)
}

func rebuildPage() *gohelp.Page {
	return gohelp.NewPage("rebuild", rebuildDescription).
		Usage(binaryName+" rebuild [flags] [nixos-rebuild args]").
		Section("Flags",
			gohelp.Item("--prune", "Remove unresolvable imports on the active host"),
			gohelp.Item("--machine <name>", "Override the hostname lookup"),
			gohelp.Item("--config|-C <dir>", "Build from another luxos config folder (sets LUXOS_CONFIG_DIR)"),
			gohelp.Item("--backup-dir <dir>", "Move unrecognized /etc/nixos entries here (sets LUXOS_BACKUP_DIR)"),
			gohelp.Item("--goodbye-luxos <dir>", "Replace /etc/nixos with <dir> as is and build it without luxos"),
			gohelp.Item("-y, --yes", "Answer yes to luxos' own confirmations"),
		)
}

func flakePage() *gohelp.Page {
	return gohelp.NewPage("flake", flakeDescription).
		Usage(binaryName+" flake <list|ls|update|name> ...").
		Section("Commands",
			gohelp.Item("list|ls", "List the host's inputs as a tree; 'luxos flakes' is a shortcut for this"),
			gohelp.Item("<name>", "Show one input: source, declaring files, current and latest version, inputs it pulls in"),
			gohelp.Item("update [inputs...]", "Update the host's inputs"),
		).
		Section("Flags",
			gohelp.Item("--machine <name>", "Override the hostname lookup"),
			gohelp.Item("--config|-C <dir>", "Use another luxos config folder (sets LUXOS_CONFIG_DIR)"),
			gohelp.Item("--offline", "list, <name>: skip the upstream check and make no network request"),
			gohelp.Item("--raw", "list, <name>: plain output even on a terminal"),
			gohelp.Item("--json", "list, <name>: JSON on stdout; field names match the plain output"),
		).
		Section("Markers (list)",
			gohelp.Item("◉", "declared by a module and locked"),
			gohelp.Item("⊕", "declared but not locked yet; the next rebuild fetches it"),
			gohelp.Item("⊘", "locked but no longer declared; the next rebuild prunes it"),
			gohelp.Item("◍", "never declared, pulled in by another input"),
			gohelp.Item("↑", "after a name: upstream has moved past the locked revision"),
			gohelp.Item("?", "after a name: upstream could not be checked (unsupported source or request failed)"),
		).
		Text("The list nests an input under its declaring module's category; an input declared by several modules, or built in (luxos, nixpkgs), sits at the root, and pulled-in inputs nest under their parent. Piped, or with --raw, it prints one line per input: path, state (active, staged, leftover, pulled), status (behind, current, unknown), tab-separated; <name> prints key=value lines (name, state, status, source, declared, current, latest, commits, pulls). With no input names, every input is updated. Names are the input names declared by your modules, e.g. luxos, unstable. The base channel, nixpkgs, is declared in the host's machine.nix as flake-file.inputs.nixpkgs.url = \"<url>\"; and is required. Transitive inputs are addressed by path, e.g. ambxst/axctl. An input named like a verb (update, list, ls) is reached only through the tree path of its parent.")
}

func modulePage() *gohelp.Page {
	return gohelp.NewPage("module", moduleDescription).
		Usage(binaryName+" module <verb> ...").
		Section("Commands",
			gohelp.Item("list|ls [category-path|bundle] [--json]", "List modules (grouped by category), or the submodules of a bundle; --json prints JSON"),
			gohelp.Item("add|a <category/name> [--enable]", "Scaffold a module; local/<name> creates a module private to the active machine"),
			gohelp.Item("edit|e <name>", "Open a module in $EDITOR"),
			gohelp.Item("configure|config <name>", "Open this host's settings for a module in $EDITOR, creating or updating them first"),
			gohelp.Item("enable|1 <name>...", "Enable one or more modules on this host; a submodule also enables its bundle"),
			gohelp.Item("disable|0 <name>... [-y]", "Disable one or more modules on this host; a bundle also disables its submodules"),
			gohelp.Item("<bundle>", "List a bundle (shorthand for list <bundle>); a verb always wins over a bundle of the same name"),
			gohelp.Item("remove|rm <name> [-y]", "Delete a module everywhere it's imported"),
			gohelp.Item("rename|rn <old> <new> [-y]", "Rename a module's identity"),
		).
		Text("A bundle is a folder module with a modules/ folder inside: everything outside modules/ is the bundle itself, imported whenever it is selected, and each module under modules/ is a submodule that a host selects separately. A submodule is addressed as <bundle>/<name> (eduardo/git) wherever a module name is accepted; modules reach other modules through luxos.modules by global name only, never a submodule. Renaming a submodule keeps it in its bundle. A local module named like a shared one replaces it on this host, and is shown underlined in its place in the list. A bundle is listed as one row, ◈ in its state's color followed by how many of its submodules are enabled (◈ eduardo 1); list <bundle> shows its submodules. After a name, ❄ marks a module declaring flake inputs and ! one nix cannot evaluate (a syntax or type error); on a module outside the build they are dimmed. Piped, or with --raw, the list prints path, state (active, staged, modified, removed, leftover, pulled, off), the declared inputs and ok or broken, tab-separated, with every submodule on its own row. A blue ⊕ (modified) means the module's files changed since the running build. A struck-through red ⊘ (removed) is a module the running system imports that no longer exists in the config; it disappears after the next switch. --json prints the same fields (path, state, inputs, ok) as a JSON array; field names match the plain output. It cannot be combined with --raw or --flat.")
}

func userPage() *gohelp.Page {
	return gohelp.NewPage("user", userDescription).
		Usage(binaryName+" user <verb> ...").
		Section("Commands",
			gohelp.Item("add|a <name> [--enable]", "Scaffold modules/users/<name>"),
			gohelp.Item("list|ls [subpath] [--json]", "List users (or a users/ subcategory); --json prints them as JSON, fields as in the plain output"),
			gohelp.Item("edit|e <name>", "Open a user module in $EDITOR"),
			gohelp.Item("enable|1 <name>...", "Enable one or more users on this host"),
			gohelp.Item("disable|0 <name>... [-y]", "Disable one or more users on this host"),
			gohelp.Item("remove|rm <name> [-y]", "Delete a user everywhere it's imported"),
			gohelp.Item("rename|rn <old> <new> [-y]", "Rename a user's identity"),
		).
		Text("Every verb here is `luxos module <verb>` with the category fixed to users/. Users are always shared across every host — there is no local/ equivalent for users.")
}

func shellPage() *gohelp.Page {
	return gohelp.NewPage("shell", shellDescription).
		Usage(binaryName + " shell [args]")
}

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

func settingsPage() *gohelp.Page {
	return gohelp.NewPage("settings", settingsDescription).
		Usage("<unit>/options.nix  and  .local/machines/<host>/settings/<unit>.nix").
		Section("options.nix rules",
			gohelp.Item("{ lib, ... }:", "Takes only lib"),
			gohelp.Item("options.<unit>.<key>", "Declarations only, one level under the unit's name (eduardo/git: options.eduardo.git.<key>)"),
			gohelp.Item("type, default", "Both on every option; the default is a plain value: null, bool, number, string, or a list or attrset of those"),
			gohelp.Item("imports = [ ./options.nix ];", "The unit's default.nix imports it; without luxos the unit builds with its defaults"),
			gohelp.Item("description", "Optional plain string; becomes the trailing comment of the key's line in the settings file (e.g. performance | balanced | power-saver)"),
		).
		Section("Settings file",
			gohelp.Item("created", "On enable or rebuild, for a selected unit, with every default"),
			gohelp.Item("missing key", "Appended with its default; existing lines are never rewritten"),
			gohelp.Item("undeclared key", "Commented out with a warning; delete it by hand"),
			gohelp.Item("rename / remove", "The file moves or is deleted with the unit"),
		).
		Text("A broken options.nix or settings file fails the rebuild before anything is staged and shows ⚙ in yellow in module list. A missing settings file builds the unit with its defaults.")
}

// args is the full argv tail. An unknown topic comes back as a non-nil
// error; the caller prints and exits like any other command error.
func Run(args []string) error {
	return gohelp.Run(args, rootPage(), rebuildPage(), flakePage(), modulePage(), userPage(), shellPage(), environmentPage(), settingsPage())
}
