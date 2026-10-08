package config

import (
	"path/filepath"

	"github.com/DeprecatedLuar/luxos/internal/templates"
)

const (
	userAccountTemplate  = "user/account.nix.tmpl"
	userPackagesTemplate = "user/packages.nix.tmpl"
	userDefaultStarter   = "starters/user/default.nix"

	userDefaultFile  = "default.nix"
	userAccountFile  = "account.nix"
	userPackagesFile = "packages.nix"
)

type userValues struct {
	Name  string
	Wheel bool
}

// UserAccount renders a user unit's account.nix; wheel enables its
// networkmanager and wheel groups.
func UserAccount(name string, wheel bool) ([]byte, error) {
	return templates.Render(userAccountTemplate, userValues{Name: name, Wheel: wheel})
}

// WriteUser creates the user unit dir, which must not exist, holding
// default.nix, the given account.nix and packages.nix.
func WriteUser(dir, name string, account []byte) error {
	packages, err := templates.Render(userPackagesTemplate, userValues{Name: name})
	if err != nil {
		return err
	}
	entry, err := templates.File(userDefaultStarter)
	if err != nil {
		return err
	}
	if err := MkdirAll(filepath.Dir(dir)); err != nil {
		return err
	}
	if err := Mkdir(dir); err != nil {
		return err
	}
	files := []struct {
		name string
		data []byte
	}{
		{userDefaultFile, entry},
		{userAccountFile, account},
		{userPackagesFile, packages},
	}
	for _, f := range files {
		if err := WriteFile(filepath.Join(dir, f.name), f.data); err != nil {
			return err
		}
	}
	return nil
}
