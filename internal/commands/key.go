package commands

import (
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/DeprecatedLuar/luxos/internal/commands/help"
	"github.com/DeprecatedLuar/luxos/internal/commands/shared"
	"github.com/DeprecatedLuar/luxos/internal/password"
	"github.com/DeprecatedLuar/luxos/internal/paths"
	"github.com/DeprecatedLuar/luxos/internal/ui"
)

const (
	keyCommand    = "key"
	keyPrompt     = "Age secret key (AGE-SECRET-KEY-1…): "
	keyRemoveAsk  = "Remove the key of %s from this computer? Without it the password of %s cannot be decrypted here. [y/N] "
	keyStatusHas  = "key"
	keyStatusNone = "missing"
)

var errKeyMissing = errors.New("no key provided")

var errKeyRemoveAborted = errors.New("aborted: nothing changed\n  confirm with: luxos key rm <user> --yes")

// Key runs `luxos key ls|add|rm`.
func Key(args []string) error {
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" {
		return help.Run([]string{"help", keyCommand})
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
		return keyList(p)
	case "add":
		return keyAdd(p, rest)
	case "rm", "remove":
		return keyRemove(p, rest)
	default:
		return fmt.Errorf("unknown key command: %s", verb)
	}
}

// oneUser returns the single user argument, which must be a plain name: it
// becomes a file name in the keys folder.
func oneUser(verb string, rest []string) (string, error) {
	if len(rest) != 1 {
		return "", fmt.Errorf("usage: luxos key %s <user>", verb)
	}
	user := rest[0]
	if user == "" || user == "." || user == ".." || strings.Contains(user, "/") {
		return "", fmt.Errorf("invalid user name %q: use a plain name, as listed by `luxos users`", user)
	}
	return user, nil
}

func keyAdd(p paths.Paths, args []string) error {
	user, err := oneUser("add", args)
	if err != nil {
		return err
	}
	secret, err := readKeyInput(os.Stdin, os.Stderr)
	if err != nil {
		return err
	}
	pub, err := password.PublicKey(secret)
	if err != nil {
		return fmt.Errorf("not a valid age key: %w", err)
	}
	if err := password.WriteKey(p.Keys, user, secret); err != nil {
		return err
	}
	fmt.Printf("stored the key of %s (%s) in %s\n", user, pub, password.KeyPath(p.Keys, user))
	return nil
}

// isKeyComment reports whether line is a comment of age-keygen output.
func isKeyComment(line string) bool {
	return strings.HasPrefix(strings.TrimSpace(line), "#")
}

// readKeyInput reads the secret line from a hidden prompt (skipping the comment lines of a pasted age-keygen block), or from all of in
// when in is not a terminal.
func readKeyInput(in *os.File, out io.Writer) (string, error) {
	term, err := ui.IsTTY(in)
	if err != nil {
		return "", err
	}
	if term {
		line, err := ui.AskSecretUnless(in, out, keyPrompt, isKeyComment)
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
	user, err := oneUser("rm", rest)
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
	fmt.Printf("removed the key of %s\n", user)
	return nil
}

// keyList prints every user with a key here, then users in the build without one.
func keyList(p paths.Paths) error {
	have, err := password.Users(p.Keys)
	if err != nil {
		return err
	}
	status := map[string]string{}
	for _, u := range have {
		status[u] = keyStatusHas
	}
	h, err := loadActive(p)
	if err != nil {
		return err
	}
	users, err := builtUsers(h)
	if err != nil {
		return err
	}
	for _, m := range users {
		if status[m.Name] == "" {
			status[m.Name] = keyStatusNone
		}
	}
	var lines []string
	for u, s := range status {
		lines = append(lines, u+"\t"+s)
	}
	sort.Strings(lines)
	fmt.Print(strings.Join(lines, "\n"))
	if len(lines) > 0 {
		fmt.Println()
	}
	return nil
}
