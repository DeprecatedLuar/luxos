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
	noUserFormat   = "'%s' is not a user: a user is a folder in modules/users/ (luxos user add %s)"
	passwdStale    = "%w\n  the key of %s on this computer may have been replaced, so an existing %s may no longer decrypt: rerun `luxos user passwd %s`"
)

// Passwd runs `luxos passwd <user>`: escalates, asks the password twice,
// reuses (repairing its mode and owner) or creates the user's key and writes
// the encrypted hash into the user's unit in modules/users/. Nothing is
// written until both answers are in and the hash is made.
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
	m, ok, err := findUserUnit(p.Modules, name)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf(noUserFormat, name, name)
	}

	pw, err := shared.AskNewPassword(func(prompt string) (string, error) {
		return ui.AskSecret(os.Stdin, os.Stderr, prompt)
	}, passwordPrompt, againPrompt)
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
	enc, err := password.Seal(hash, secret)
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
