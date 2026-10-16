package password

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"syscall"
)

const (
	keysDirMode = 0o700
	keyMode     = 0o400
	writeMode   = 0o600
)

// ErrNoKey is returned when a user has no key in the folder.
var ErrNoKey = errors.New("no key")

// ErrNotKey is returned when a user's key file holds no age identity.
var ErrNotKey = errors.New("not an age key")

// KeyPath is user's key inside keysDir.
func KeyPath(keysDir, user string) string {
	return filepath.Join(keysDir, user)
}

// WriteKey stores secret as user's key: folder 0700, file 0400, replacing any
// existing key.
func WriteKey(keysDir, user, secret string) error {
	if err := os.MkdirAll(keysDir, keysDirMode); err != nil {
		return err
	}
	if err := os.Chmod(keysDir, keysDirMode); err != nil {
		return err
	}
	path := KeyPath(keysDir, user)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := os.WriteFile(path, []byte(secret+"\n"), writeMode); err != nil {
		return err
	}
	return os.Chmod(path, keyMode)
}

// CheckKey reports whether user's key exists, is owned by uid with mode 0400
// and is an age identity.
func CheckKey(keysDir, user string, uid int) error {
	_, err := ReadKey(keysDir, user, uid)
	return err
}

// ReadKey returns user's secret after the CheckKey checks.
func ReadKey(keysDir, user string, uid int) (string, error) {
	path := KeyPath(keysDir, user)
	fi, err := os.Stat(path)
	if os.IsNotExist(err) {
		return "", fmt.Errorf("%s: %w", path, ErrNoKey)
	}
	if err != nil {
		return "", err
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return "", fmt.Errorf("%s: cannot read its owner", path)
	}
	if fi.Mode().Perm() != keyMode || int(st.Uid) != uid {
		return "", fmt.Errorf("%s must be owned by uid %d with mode 0400 (is uid %d, %04o)", path, uid, st.Uid, fi.Mode().Perm())
	}
	return readSecret(path)
}

// RepairKey returns user's secret when the key file holds an age identity,
// first setting its owner to uid and its mode to 0400 when they differ. A
// file holding none is left untouched (ErrNotKey).
func RepairKey(keysDir, user string, uid int) (string, error) {
	path := KeyPath(keysDir, user)
	fi, err := os.Stat(path)
	if os.IsNotExist(err) {
		return "", fmt.Errorf("%s: %w", path, ErrNoKey)
	}
	if err != nil {
		return "", err
	}
	secret, err := readSecret(path)
	if err != nil {
		return "", err
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return "", fmt.Errorf("%s: cannot read its owner", path)
	}
	if int(st.Uid) != uid {
		if err := os.Chown(path, uid, -1); err != nil {
			return "", fmt.Errorf("%s: cannot set its owner to uid %d: %w", path, uid, err)
		}
	}
	if fi.Mode().Perm() != keyMode {
		if err := os.Chmod(path, keyMode); err != nil {
			return "", err
		}
	}
	return secret, nil
}

// readSecret returns the age identity in the key file at path.
func readSecret(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	secret, err := SecretLine(string(data))
	if err != nil {
		return "", fmt.Errorf("%s: %w (%v)", path, ErrNotKey, err)
	}
	if _, err := PublicKey(secret); err != nil {
		return "", fmt.Errorf("%s: %w (%v)", path, ErrNotKey, err)
	}
	return secret, nil
}

// RemoveKey deletes user's key.
func RemoveKey(keysDir, user string) error {
	err := os.Remove(KeyPath(keysDir, user))
	if os.IsNotExist(err) {
		return fmt.Errorf("%s: %w", KeyPath(keysDir, user), ErrNoKey)
	}
	return err
}

// Users lists, sorted, every user with a key file. A missing folder has none.
func Users(keysDir string) ([]string, error) {
	entries, err := os.ReadDir(keysDir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var users []string
	for _, e := range entries {
		if e.Type().IsRegular() {
			users = append(users, e.Name())
		}
	}
	sort.Strings(users)
	return users, nil
}

// Check is the rebuild preflight for one user whose unit is unitDir: the
// password file exists and user's key is valid and decrypts it. Each error
// names the fixing command.
func Check(unitDir, keysDir, user string, uid int) error {
	file := filepath.Join(unitDir, File)
	if _, err := os.Stat(file); os.IsNotExist(err) {
		return fmt.Errorf("user %s has no password: luxos passwd %s", user, user)
	} else if err != nil {
		return err
	}
	if err := CheckKey(keysDir, user, uid); err != nil {
		return fmt.Errorf("user %s: %w\n  fix: luxos key add %s", user, err, user)
	}
	if err := Decrypt(file, KeyPath(keysDir, user)); err != nil {
		return fmt.Errorf("user %s: the key on this computer does not decrypt %s: %v\n  fix: luxos key add %s (or set a new password: luxos passwd %s)", user, file, err, user, user)
	}
	return nil
}
