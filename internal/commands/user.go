package commands

import (
	"fmt"
	"strings"

	"github.com/DeprecatedLuar/luxos/internal/commands/help"
	"github.com/DeprecatedLuar/luxos/internal/commands/shared"
	"github.com/DeprecatedLuar/luxos/internal/paths"
)

// Only add/list take a category-relative argument; every other verb
// operates on bare, globally-unique names and is forwarded to Module's
// verb functions untouched.
func User(args []string) error {
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" {
		return help.Run([]string{"help", "user"})
	}

	p, err := paths.Resolve()
	if err != nil {
		return err
	}

	var verb string
	var rest []string
	if len(args) > 0 {
		verb, rest = args[0], args[1:]
	}

	switch verb {
	case "add", "a":
		return userAdd(p, rest)
	case "list", "ls":
		return userList(p, rest)
	case "edit", "e":
		return moduleEdit(p, rest)
	case "enable":
		return moduleEnable(p, rest)
	case "disable":
		return moduleDisable(p, rest)
	case "remove", "rm":
		return moduleRemove(p, rest)
	case "rename", "rn":
		return moduleRename(p, rest)
	default:
		return fmt.Errorf("unknown user command: %s", verb)
	}
}

// userTarget lets `luxos user add foo` and `luxos user add users/foo`
// build the same module-add target.
func userTarget(name string) string {
	if strings.HasPrefix(name, "users/") {
		return name
	}
	return "users/" + name
}

func userAdd(p paths.Paths, args []string) error {
	opts, rest, err := shared.Parse("enable:bool", args)
	if err != nil {
		return err
	}

	var name string
	if len(rest) > 0 {
		name = rest[0]
	}
	if name == "" {
		return fmt.Errorf("usage: luxos user add <name> [--enable]")
	}

	fwd := []string{userTarget(name)}
	if opts["enable"] != "" {
		fwd = append(fwd, "--enable")
	}
	return moduleAdd(p, fwd)
}

func userCategory(sub string) string {
	if sub == "" {
		return "users"
	}
	return "users/" + sub
}

// Flags (--flat, --raw, ...) are forwarded untouched; the first non-flag
// argument, if any, is the subpath.
func userList(p paths.Paths, args []string) error {
	var sub string
	var flags []string
	for _, a := range args {
		if strings.HasPrefix(a, "-") {
			flags = append(flags, a)
			continue
		}
		if sub == "" {
			sub = a
		}
	}
	return moduleList(p, append([]string{userCategory(sub)}, flags...))
}
