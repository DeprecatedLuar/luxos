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

	// luxosPropertyHeader is the first line of every vendored file.
	luxosPropertyHeader = "# LUXOS property - keep walking buddy"
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
// differing, keeping the folder's owner, and locks it to 0444. A differing
// file without the luxos header is hand-written: SyncUser refuses it and
// writes nothing for the unit.
func SyncUser(dir string) ([]SyncChange, error) {
	type pending struct {
		name, path string
		want, have []byte
		missing    bool
	}
	var files []pending
	for _, f := range userVendored {
		want, err := templates.File(f.src)
		if err != nil {
			return nil, err
		}
		path := filepath.Join(dir, f.name)
		have, err := os.ReadFile(path)
		if err != nil && !os.IsNotExist(err) {
			return nil, err
		}
		if err == nil && !bytes.Equal(have, want) && !bytes.HasPrefix(have, []byte(luxosPropertyHeader+"\n")) {
			return nil, fmt.Errorf("%s is not a luxos file: move your values to another file of the unit, then delete %s and rebuild", path, path)
		}
		files = append(files, pending{name: f.name, path: path, want: want, have: have, missing: err != nil})
	}

	var changes []SyncChange
	for _, f := range files {
		action := ActionRestored
		switch {
		case f.missing:
			action = ActionCreated
		case bytes.Equal(f.have, f.want):
			if err := os.Chmod(f.path, lockedMode); err != nil {
				return nil, err
			}
			continue
		default:
			if err := os.Chmod(f.path, fileMode); err != nil {
				return nil, err
			}
		}
		if err := WriteFile(f.path, f.want); err != nil {
			return nil, err
		}
		if err := os.Chmod(f.path, lockedMode); err != nil {
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
