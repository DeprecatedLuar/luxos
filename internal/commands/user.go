package commands

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/DeprecatedLuar/luxos/internal/commands/help"
	"github.com/DeprecatedLuar/luxos/internal/commands/shared"
	"github.com/DeprecatedLuar/luxos/internal/paths"
)

const userCommand = "user"

var errMissingUserName = errors.New("missing user name")

// Only add/list take a category-relative argument; every other verb
// operates on bare, globally-unique names and is forwarded to Module's
// verb functions untouched.
func User(args []string) error {
	if len(args) == 0 {
		return help.Misuse(os.Stderr, userCommand, nil)
	}
	if isHelpFlag(args[0]) {
		return help.Command(os.Stdout, userCommand)
	}

	p, err := paths.Resolve()
	if err != nil {
		return err
	}

	verb, rest := args[0], args[1:]
	switch verb {
	case "add", "a":
		return userAdd(p, rest)
	case "list", "ls":
		return userList(p, rest)
	case "edit", "e":
		return moduleEdit(p, rest)
	case "configure", "config":
		return moduleConfigure(p, rest)
	case "enable", "1":
		return moduleEnable(p, rest)
	case "disable", "0":
		return moduleDisable(p, rest)
	case "remove", "rm":
		return moduleRemove(p, rest)
	case "rename", "rn":
		return moduleRename(p, rest)
	case "passwd", "password":
		return Passwd(rest)
	default:
		return help.Misuse(os.Stderr, userCommand, fmt.Errorf(unknownVerbFormat, verb))
	}
}

// userTarget lets `luxos user add foo` and `luxos user add users/foo`
// build the same module-add target.
func userTarget(name string) string {
	if strings.HasPrefix(name, usersCategory+"/") {
		return name
	}
	return usersCategory + "/" + name
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
		return help.Misuse(os.Stderr, userCommand, errMissingUserName)
	}

	fwd := []string{userTarget(name)}
	if opts["enable"] != "" {
		fwd = append(fwd, "--enable")
	}
	return moduleAdd(p, fwd)
}

func userCategory(sub string) string {
	if sub == "" {
		return usersCategory
	}
	return usersCategory + "/" + sub
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
