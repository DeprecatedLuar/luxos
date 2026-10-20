package config

import (
	"bytes"
	"fmt"
	"os"
	"path"
	"path/filepath"

	"github.com/DeprecatedLuar/luxos/internal/templates"
)

const (
	userPackagesStarter = "starters/user/modules/packages.nix"

	// UsersDir is the category of modules/ that holds user units.
	UsersDir = "users"

	// UserSelectionFile is the user's selection inside a user unit.
	UserSelectionFile = "modules/default.nix"

	userPackagesLine = "./packages.nix"

	userModulesDir  = "modules"
	userDefaultFile = "default.nix"
	userOldSuffix   = ".old"

	// UserPackagesFile is the starter's path inside a user unit.
	UserPackagesFile = "modules/packages.nix"

	userGroupsTemplate = "{\n  %s.groups = [ \"wheel\" \"networkmanager\" ];\n}\n"

	// luxosPropertyHeader is the first line of every vendored file.
	luxosPropertyHeader = "# LUXOS property - keep walking buddy"
)

// userVendored are the files luxos owns inside every user unit, by name, with
// their embedded source.
var userVendored = []struct{ name, src string }{
	{"default.nix", "user/default.nix"},
	{"account.nix", "user/account.nix"},
	{"options.nix", "user/options.nix"},
}

// WriteUser creates the user unit dir, which must not exist: the starter
// modules/packages.nix plus the vendored files.
func WriteUser(dir string) error {
	if err := MkdirAll(filepath.Dir(dir)); err != nil {
		return err
	}
	if err := Mkdir(dir); err != nil {
		return err
	}
	_, err := SyncUser(dir)
	return err
}

// SyncUser writes each vendored file into the user unit dir when missing or
// differing, keeping the folder's owner, and locks it to 0444. A differing
// file without the luxos header is hand-written: SyncUser refuses it and
// writes nothing for the unit, except default.nix, which a unit cannot load
// without: that one is kept as default.nix.old and replaced. A unit without
// a modules/ folder gets one holding the starter modules/packages.nix; a unit
// without a selection gets one, selecting the starter when SyncUser wrote it.
// An existing selection is never rewritten.
func SyncUser(dir string) ([]SyncChange, error) {
	type pending struct {
		name, path string
		want, have []byte
		missing    bool
		replace    bool
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
		handWritten := err == nil && !bytes.Equal(have, want) && !bytes.HasPrefix(have, []byte(luxosPropertyHeader+"\n"))
		if handWritten && f.name != userDefaultFile {
			return nil, fmt.Errorf("%s is not a luxos file: move your values into %s, then delete %s and rebuild", path, filepath.Join(dir, userModulesDir), path)
		}
		files = append(files, pending{name: f.name, path: path, want: want, have: have, missing: err != nil, replace: handWritten})
	}

	var changes []SyncChange
	for _, f := range files {
		action := ActionRestored
		switch {
		case f.missing:
			action = ActionCreated
		case f.replace:
			action = ActionReplaced
			if err := os.Rename(f.path, f.path+userOldSuffix); err != nil {
				return nil, err
			}
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

	modulesDir := filepath.Join(dir, userModulesDir)
	var lines []string
	if _, err := os.Stat(modulesDir); os.IsNotExist(err) {
		if err := Mkdir(modulesDir); err != nil {
			return nil, err
		}
		data, err := templates.File(userPackagesStarter)
		if err != nil {
			return nil, err
		}
		if err := WriteFile(filepath.Join(dir, UserPackagesFile), data); err != nil {
			return nil, err
		}
		changes = append(changes, SyncChange{Path: UserPackagesFile, Action: ActionCreated})
		lines = []string{userPackagesLine}
	} else if err != nil {
		return nil, err
	}

	selection := UserSelectionPath(dir)
	if _, err := os.Lstat(selection); err == nil {
		return changes, nil
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	if err := CreateUserSelection(dir, lines); err != nil {
		return nil, err
	}
	return append(changes, SyncChange{Path: UserSelectionFile, Action: ActionCreated}), nil
}

// CreateUserSelection writes the selection of the user unit dir, holding
// lines. The caller makes sure none exists.
func CreateUserSelection(dir string, lines []string) error {
	data, err := templates.Render(selectionTemplate, lines)
	if err != nil {
		return err
	}
	return WriteFile(UserSelectionPath(dir), data)
}

// UserScope is the modules-relative folder a user's selection lines are relative to.
func UserScope(name string) string {
	return path.Join(UsersDir, name, userModulesDir)
}

// UserSelectionPath is the selection of the user unit dir.
func UserSelectionPath(dir string) string {
	return filepath.Join(dir, UserSelectionFile)
}

// UserGroups is the host settings file setup writes for its first user.
func UserGroups(name string) []byte {
	return []byte(fmt.Sprintf(userGroupsTemplate, name))
}
