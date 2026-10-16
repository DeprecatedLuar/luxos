package commands

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/DeprecatedLuar/luxos/internal/commands/help"
	"github.com/DeprecatedLuar/luxos/internal/commands/shared"
	"github.com/DeprecatedLuar/luxos/internal/config"
	"github.com/DeprecatedLuar/luxos/internal/modules"
	"github.com/DeprecatedLuar/luxos/internal/password"
	"github.com/DeprecatedLuar/luxos/internal/paths"
	"github.com/DeprecatedLuar/luxos/internal/ui"
)

const (
	passwdCommand  = "passwd"
	passwordPrompt = "New password: "
	againPrompt    = "Retype new password: "
	newKeyNotice   = "New key for %s. Save it somewhere safe (a password manager); every computer that builds %s needs it (luxos key add %s):\n\n%s\n\n"
	badKeyNotice   = "warning: the key of %s on this computer is unusable (%v); creating a new one\n"
	passwdDone     = "Password of %s written to %s\n"
	passwdStale    = "%w\n  the key of %s on this computer may have been replaced, so an existing %s may no longer decrypt: rerun `luxos passwd %s`"
)

var errPasswordMismatch = errors.New("passwords do not match; nothing changed")

// Passwd runs `luxos passwd <user>`: escalates, asks the password twice,
// reuses or creates the user's key and writes the encrypted hash into the
// user's unit (a shadow's when one wins). Nothing is written until both
// answers are in and the hash is made.
func Passwd(args []string) error {
	if len(args) != 1 || args[0] == "--help" || args[0] == "-h" {
		return help.Run([]string{"help", passwdCommand})
	}
	if err := shared.EnsureRoot(append([]string{passwdCommand}, args...)); err != nil {
		return err
	}
	p, err := paths.Resolve()
	if err != nil {
		return err
	}
	name := args[0]
	h, err := loadActive(p)
	if err != nil {
		return err
	}
	m, ok := h.Find(name)
	if !ok || !modules.IsUser(m) {
		return fmt.Errorf("'%s' is not a user: a user is a folder in modules/users/ (luxos user add %s)", name, name)
	}

	pw, err := readNewPassword(func(prompt string) (string, error) {
		return ui.AskSecret(os.Stdin, os.Stderr, prompt)
	})
	if err != nil {
		return err
	}
	hash, err := password.Hash(pw)
	if err != nil {
		return err
	}
	pub, err := userKey(p.Keys, name, keyOwnerUID, os.Stdout)
	if err != nil {
		return err
	}
	file := filepath.Join(m.Abs, password.File)
	enc, err := password.Encrypt(hash, pub)
	if err != nil {
		return fmt.Errorf(passwdStale, err, name, password.File, name)
	}
	if err := config.WriteFile(file, enc); err != nil {
		return fmt.Errorf(passwdStale, err, name, password.File, name)
	}
	fmt.Printf(passwdDone, name, file)
	return nil
}

// readNewPassword asks twice; both answers must match and not be empty.
func readNewPassword(ask func(prompt string) (string, error)) (string, error) {
	first, err := ask(passwordPrompt)
	if err != nil {
		return "", err
	}
	if first == "" {
		return "", fmt.Errorf("%w; nothing changed", password.ErrEmpty)
	}
	second, err := ask(againPrompt)
	if err != nil {
		return "", err
	}
	if first != second {
		return "", errPasswordMismatch
	}
	return first, nil
}

// userKey returns the public key of user's key, creating and printing a new
// key when there is none or it is unusable.
func userKey(keysDir, user string, uid int, out io.Writer) (string, error) {
	secret, err := password.ReadKey(keysDir, user, uid)
	if err == nil {
		return password.PublicKey(secret)
	}
	if !errors.Is(err, password.ErrNoKey) {
		fmt.Fprintf(out, badKeyNotice, user, err)
	}
	if secret, err = password.NewKey(); err != nil {
		return "", err
	}
	if err := password.WriteKey(keysDir, user, secret); err != nil {
		return "", err
	}
	fmt.Fprintf(out, newKeyNotice, user, user, user, secret)
	return password.PublicKey(secret)
}
