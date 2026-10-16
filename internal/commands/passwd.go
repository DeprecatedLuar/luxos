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
// reuses (repairing its mode and owner) or creates the user's key and writes the encrypted hash into the
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
	file := filepath.Join(m.Abs, password.File)
	if err := storePassword(p.Keys, name, file, hash, keyOwnerUID, os.Stdout, os.Stderr); err != nil {
		return err
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

// userKey resolves user's key in memory: the stored one (its mode and owner
// repaired), or a new, unstored one (fresh) when there is none or the file
// holds no age key, which is warned about on warn.
func userKey(keysDir, user string, uid int, warn io.Writer) (secret string, fresh bool, err error) {
	secret, err = password.RepairKey(keysDir, user, uid)
	switch {
	case err == nil:
		return secret, false, nil
	case errors.Is(err, password.ErrNotKey):
		fmt.Fprintf(warn, badKeyNotice, user, err)
	case !errors.Is(err, password.ErrNoKey):
		return "", false, err
	}
	if secret, err = password.NewKey(); err != nil {
		return "", false, err
	}
	return secret, true, nil
}

// storePassword encrypts hash to user's key and writes it to file. A fresh
// key is stored, and printed on out, only once hash is encrypted to it.
func storePassword(keysDir, user, file, hash string, uid int, out, warn io.Writer) error {
	secret, fresh, err := userKey(keysDir, user, uid, warn)
	if err != nil {
		return err
	}
	pub, err := password.PublicKey(secret)
	if err != nil {
		return err
	}
	enc, err := password.Encrypt(hash, pub)
	if err != nil {
		return err
	}
	if !fresh {
		return config.WriteFile(file, enc)
	}
	if err := password.WriteKey(keysDir, user, secret); err != nil {
		return fmt.Errorf(passwdStale, err, user, password.File, user)
	}
	fmt.Fprintf(out, newKeyNotice, user, user, user, secret)
	if err := config.WriteFile(file, enc); err != nil {
		return fmt.Errorf(passwdStale, err, user, password.File, user)
	}
	return nil
}
