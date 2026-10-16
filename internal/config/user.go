package config

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	"github.com/DeprecatedLuar/luxos/internal/templates"
)

const (
	userDefaultStarter  = "starters/user/default.nix"
	userPackagesStarter = "starters/user/packages.nix"

	userDefaultFile  = "default.nix"
	userPackagesFile = "packages.nix"

	userGroupsTemplate = "{\n  %s.groups = [ \"wheel\" \"networkmanager\" ];\n}\n"
)

// userVendored are the files luxos owns inside every user unit, by name, with
// their embedded source.
var userVendored = []struct{ name, src string }{
	{"account.nix", "user/account.nix"},
	{"options.nix", "user/options.nix"},
}

// WriteUser creates the user unit dir, which must not exist: the starter
// default.nix and packages.nix plus the vendored files.
func WriteUser(dir string) error {
	if err := MkdirAll(filepath.Dir(dir)); err != nil {
		return err
	}
	if err := Mkdir(dir); err != nil {
		return err
	}
	for _, f := range []struct{ name, src string }{
		{userDefaultFile, userDefaultStarter},
		{userPackagesFile, userPackagesStarter},
	} {
		data, err := templates.File(f.src)
		if err != nil {
			return err
		}
		if err := WriteFile(filepath.Join(dir, f.name), data); err != nil {
			return err
		}
	}
	_, err := SyncUser(dir)
	return err
}

// SyncUser writes each vendored file into the user unit dir when missing or
// differing, keeping the folder's owner, and locks it to 0444.
func SyncUser(dir string) ([]SyncChange, error) {
	var changes []SyncChange
	for _, f := range userVendored {
		want, err := templates.File(f.src)
		if err != nil {
			return nil, err
		}
		path := filepath.Join(dir, f.name)
		have, err := os.ReadFile(path)
		action := ActionRestored
		switch {
		case os.IsNotExist(err):
			action = ActionCreated
		case err != nil:
			return nil, err
		case bytes.Equal(have, want):
			if err := os.Chmod(path, lockedMode); err != nil {
				return nil, err
			}
			continue
		default:
			if err := os.Chmod(path, fileMode); err != nil {
				return nil, err
			}
		}
		if err := WriteFile(path, want); err != nil {
			return nil, err
		}
		if err := os.Chmod(path, lockedMode); err != nil {
			return nil, err
		}
		changes = append(changes, SyncChange{Path: f.name, Action: action})
	}
	return changes, nil
}

// UserGroups is the host settings file setup writes for its first user.
func UserGroups(name string) []byte {
	return []byte(fmt.Sprintf(userGroupsTemplate, name))
}
