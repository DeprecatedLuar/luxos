// Package password hashes a password, encrypts the hash to a user's age key and
// manages the folder of those keys. It runs mkpasswd, age and age-keygen; the
// luxos flake sets their store paths at build time.
package password

import (
	"errors"
	"fmt"
	"strings"

	"github.com/DeprecatedLuar/luxos/internal/shell"
)

// Set by the flake with -ldflags -X; bare names resolve on PATH.
var (
	ageBin       = "age"
	ageKeygenBin = "age-keygen"
	mkpasswdBin  = "mkpasswd"
)

const (
	// File is the encrypted hash inside a user unit.
	File = ".password.age"

	hashMethod    = "yescrypt"
	secretPrefix  = "AGE-SECRET-KEY-1"
	commentPrefix = "#"
)

// ErrEmpty is returned for an empty password, which mkpasswd would hash.
var ErrEmpty = errors.New("empty password")

// Hash returns the yescrypt hash of pw. The password goes on stdin, never in
// the argument list.
func Hash(pw string) (string, error) {
	if pw == "" {
		return "", ErrEmpty
	}
	out, err := shell.Output(shell.Cmd{
		Bin:   mkpasswdBin,
		Args:  []string{"-m", hashMethod, "--stdin"},
		Stdin: strings.NewReader(pw),
	})
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// NewKey generates an age identity and returns its secret line.
func NewKey() (string, error) {
	out, err := shell.Output(shell.Cmd{Bin: ageKeygenBin})
	if err != nil {
		return "", err
	}
	return SecretLine(string(out))
}

// PublicKey returns the recipient of secret; it fails on anything that is
// not an age identity.
func PublicKey(secret string) (string, error) {
	out, err := shell.Output(shell.Cmd{Bin: ageKeygenBin, Args: []string{"-y"}, Stdin: strings.NewReader(secret + "\n")})
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// SecretLine returns the one AGE-SECRET-KEY-1 line of text, ignoring comment
// lines and surrounding space.
func SecretLine(text string) (string, error) {
	var found []string
	for _, line := range strings.Split(text, "\n") {
		if line = strings.TrimSpace(line); strings.HasPrefix(line, secretPrefix) {
			found = append(found, line)
		}
	}
	if len(found) != 1 {
		return "", fmt.Errorf("expected exactly one %s… line, found %d", secretPrefix, len(found))
	}
	return found[0], nil
}

// IsKeyComment reports whether line is a comment line of age-keygen output.
func IsKeyComment(line string) bool {
	return strings.HasPrefix(strings.TrimSpace(line), commentPrefix)
}

// Encrypt returns hash encrypted, armored, to recipient.
func Encrypt(hash, recipient string) ([]byte, error) {
	return shell.Output(shell.Cmd{
		Bin:   ageBin,
		Args:  []string{"-a", "-r", recipient},
		Stdin: strings.NewReader(hash + "\n"),
	})
}

// Seal returns hash encrypted, armored, to the public key of the age
// identity secret.
func Seal(hash, secret string) ([]byte, error) {
	recipient, err := PublicKey(secret)
	if err != nil {
		return nil, err
	}
	return Encrypt(hash, recipient)
}

// Decrypt reports whether keyFile decrypts file. The plaintext is discarded.
func Decrypt(file, keyFile string) error {
	_, err := shell.Output(shell.Cmd{Bin: ageBin, Args: []string{"-d", "-i", keyFile, file}})
	return err
}
