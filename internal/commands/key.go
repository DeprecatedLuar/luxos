package commands

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/DeprecatedLuar/luxos/internal/commands/help"
	"github.com/DeprecatedLuar/luxos/internal/commands/shared"
	"github.com/DeprecatedLuar/luxos/internal/password"
	"github.com/DeprecatedLuar/luxos/internal/paths"
	"github.com/DeprecatedLuar/luxos/internal/ui"
)

const (
	keyCommand   = "key"
	keyPrompt    = "Age secret key (AGE-SECRET-KEY-1…): "
	keyRemoveAsk = "Remove the key of %s from this computer? The account %s is locked at the next boot or rebuild on this computer, and older generations stop accepting that password. [y/N] "
	keyNotStored = "%w\n  the key was not stored"
)

var errWantOneUser = errors.New("want exactly one user")

var errKeyMissing = errors.New("no key provided")

var errKeyRemoveAborted = errors.New("aborted: nothing changed\n  confirm with: luxos user key rm <user> --yes")

// Key runs `luxos key ls|add|get|rm`.
func Key(args []string) error {
	if len(args) == 0 {
		return help.Misuse(os.Stderr, keyCommand, nil)
	}
	if isHelpFlag(args[0]) {
		return help.Command(os.Stdout, keyCommand)
	}
	if err := shared.EnsureRoot(append([]string{keyCommand}, args...)); err != nil {
		return err
	}
	p, err := paths.Resolve()
	if err != nil {
		return err
	}
	verb, rest := args[0], args[1:]
	switch verb {
	case "ls", "list":
		return keyList(p.Keys, os.Stdout)
	case "add":
		return keyAdd(p, rest)
	case "get":
		return keyGet(p, rest, os.Stdout)
	case "rm", "remove":
		return keyRemove(p, rest)
	default:
		return help.Misuse(os.Stderr, keyCommand, fmt.Errorf(unknownVerbFormat, verb))
	}
}

// oneUser returns the single user argument, which must be a plain name: it
// becomes a file name in the keys folder.
func oneUser(rest []string) (string, error) {
	if len(rest) != 1 {
		return "", help.Misuse(os.Stderr, keyCommand, errWantOneUser)
	}
	user := rest[0]
	if user == "" || user == "." || user == ".." || strings.Contains(user, "/") {
		return "", fmt.Errorf("invalid user name %q: use a plain name, as listed by `luxos users`", user)
	}
	return user, nil
}

func keyAdd(p paths.Paths, args []string) error {
	user, err := oneUser(args)
	if err != nil {
		return err
	}
	secret, err := readKeyInput(os.Stdin, os.Stderr)
	if err != nil {
		return err
	}
	return storeKey(p, user, secret, keyOwnerUID)
}

// storeKey stores secret as user's key. When user's unit holds a password
// file, the key must decrypt it, or it is removed again.
func storeKey(p paths.Paths, user, secret string, uid int) error {
	pub, err := password.PublicKey(secret)
	if err != nil {
		return fmt.Errorf("not a valid age key: %w", err)
	}
	if err := password.WriteKey(p.Keys, user, secret); err != nil {
		return err
	}
	m, ok, err := findUserUnit(p.Modules, user)
	if err != nil {
		return err
	}
	if ok {
		if _, err := os.Stat(filepath.Join(m.Abs, password.File)); err == nil {
			if checkErr := password.Check(m.Abs, p.Keys, user, uid); checkErr != nil {
				if rmErr := password.RemoveKey(p.Keys, user); rmErr != nil {
					checkErr = errors.Join(checkErr, rmErr)
				}
				return fmt.Errorf(keyNotStored, checkErr)
			}
		} else if !os.IsNotExist(err) {
			return err
		}
	}
	ui.Done(os.Stdout, fmt.Sprintf("stored the key of %s (%s) in %s", user, pub, password.KeyPath(p.Keys, user)))
	return nil
}

// keyGet prints user's secret key alone on out, so it can be piped.
func keyGet(p paths.Paths, args []string, out io.Writer) error {
	user, err := oneUser(args)
	if err != nil {
		return err
	}
	secret, err := password.ReadKey(p.Keys, user, keyOwnerUID)
	if err != nil {
		return err
	}
	ui.Text(out, secret)
	return nil
}

// readKeyInput reads the secret line from a hidden prompt (skipping the comment lines of a pasted age-keygen block), or from all of in
// when in is not a terminal.
func readKeyInput(in *os.File, out io.Writer) (string, error) {
	term, err := ui.IsTTY(in)
	if err != nil {
		return "", err
	}
	if term {
		line, err := ui.AskSecretUnless(in, out, keyPrompt, password.IsKeyComment)
		if errors.Is(err, io.EOF) {
			return "", errKeyMissing
		}
		if err != nil {
			return "", err
		}
		return password.SecretLine(line)
	}
	data, err := io.ReadAll(in)
	if err != nil {
		return "", err
	}
	return password.SecretLine(string(data))
}

func keyRemove(p paths.Paths, args []string) error {
	opts, rest, err := shared.Parse(moduleYesSpec, args)
	if err != nil {
		return err
	}
	user, err := oneUser(rest)
	if err != nil {
		return err
	}
	if opts["yes"] == "" {
		ok, err := ui.Confirm(fmt.Sprintf(keyRemoveAsk, user, user), false)
		if err != nil {
			return err
		}
		if !ok {
			return errKeyRemoveAborted
		}
	}
	if err := password.RemoveKey(p.Keys, user); err != nil {
		return err
	}
	ui.Done(os.Stdout, fmt.Sprintf("removed the key of %s", user))
	return nil
}

// keyList prints every user with a key in keysDir, one per line.
func keyList(keysDir string, out io.Writer) error {
	have, err := password.Users(keysDir)
	if err != nil {
		return err
	}
	for _, u := range have {
		ui.Text(out, u)
	}
	return nil
}
