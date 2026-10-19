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
	badKeyNotice   = "key of %s is unusable, creating a new one"
	passwdDone     = "password of %s set"
	passwdStale    = "%w\n  the key of %s on this computer may have been replaced, so an existing %s may no longer decrypt: rerun `luxos passwd %s`"
)

var errPasswordMismatch = errors.New("passwords do not match; nothing changed")

// Passwd runs `luxos passwd <user>`: escalates, asks the password twice,
// reuses (repairing its mode and owner) or creates the user's key and writes the encrypted hash into the
// user's unit (a shadow's when one wins). Nothing is written until both
// answers are in and the hash is made.
func Passwd(args []string) error {
	if len(args) > 0 && isHelpFlag(args[0]) {
		return help.Command(os.Stdout, passwdCommand)
	}
	if len(args) != 1 {
		return help.Misuse(os.Stderr, passwdCommand, errWantOneUser)
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
	secret, err := storePassword(p.Keys, name, file, hash, keyOwnerUID, os.Stderr)
	if err == nil {
		ui.Done(os.Stdout, fmt.Sprintf(passwdDone, ui.PaletteOf(os.Stdout).Name(name)))
	}
	if secret != "" {
		shared.KeyNotice(os.Stdout, name, secret)
	}
	return err
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
		ui.Warn(warn, fmt.Sprintf(badKeyNotice, user))
	case !errors.Is(err, password.ErrNoKey):
		return "", false, err
	}
	if secret, err = password.NewKey(); err != nil {
		return "", false, err
	}
	return secret, true, nil
}

// storePassword encrypts hash to user's key and writes it to file. A fresh
// key is stored only once hash is encrypted to it, and returned, also when
// writing file then fails: a rerun reuses the key and never shows it, so the
// caller shows it now.
func storePassword(keysDir, user, file, hash string, uid int, warn io.Writer) (string, error) {
	secret, fresh, err := userKey(keysDir, user, uid, warn)
	if err != nil {
		return "", err
	}
	pub, err := password.PublicKey(secret)
	if err != nil {
		return "", err
	}
	enc, err := password.Encrypt(hash, pub)
	if err != nil {
		return "", err
	}
	if !fresh {
		return "", config.WriteFile(file, enc)
	}
	if err := password.WriteKey(keysDir, user, secret); err != nil {
		return "", fmt.Errorf(passwdStale, err, user, password.File, user)
	}
	if err := config.WriteFile(file, enc); err != nil {
		return secret, fmt.Errorf(passwdStale, err, user, password.File, user)
	}
	return secret, nil
}
