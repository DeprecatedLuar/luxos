// Package paths resolves every directory luxos operates on. Only
// internal/commands/* call Resolve(); every other package takes directories
// as parameters so it can be tested against t.TempDir().
package paths

import (
	"os"
	"os/user"
	"path/filepath"
)

const (
	configDirEnv = "LUXOS_CONFIG_DIR"
	sudoUserEnv  = "SUDO_USER"

	configRelToHome = ".config/luxos"
	localRel        = ".local"
	modulesRel      = "modules"
	luxLinkRel      = ".local/bin/lux"

	etcNixos       = "/etc/nixos"
	staging        = "/etc/nixos/luxos"
	hardwareConfig = "/etc/nixos/hardware-configuration.nix"
	runningModules = "/run/current-system/etc/luxos/modules.nix"
)

// Paths holds every directory luxos needs, resolved once per invocation.
type Paths struct {
	Home, User     string // invoking user: $SUDO_USER's passwd entry when set, else current user
	Config         string // $LUXOS_CONFIG_DIR, else <Home>/.config/luxos
	Local          string // <Config>/.local
	Modules        string // <Config>/modules
	Staging        string // /etc/nixos/luxos
	EtcNixos       string // /etc/nixos
	HardwareConfig string // /etc/nixos/hardware-configuration.nix
	RunningModules string // /run/current-system/etc/luxos/modules.nix
	LuxLink        string // <Home>/.local/bin/lux
}

// Resolve determines the invoking user (honoring $SUDO_USER) and builds
// every directory luxos needs from it.
func Resolve() (Paths, error) {
	u, err := invokingUser()
	if err != nil {
		return Paths{}, err
	}

	config := os.Getenv(configDirEnv)
	if config == "" {
		config = filepath.Join(u.HomeDir, configRelToHome)
	}

	return Paths{
		Home:           u.HomeDir,
		User:           u.Username,
		Config:         config,
		Local:          filepath.Join(config, localRel),
		Modules:        filepath.Join(config, modulesRel),
		Staging:        staging,
		EtcNixos:       etcNixos,
		HardwareConfig: hardwareConfig,
		RunningModules: runningModules,
		LuxLink:        filepath.Join(u.HomeDir, luxLinkRel),
	}, nil
}

func invokingUser() (*user.User, error) {
	if sudoUser := os.Getenv(sudoUserEnv); sudoUser != "" {
		return user.Lookup(sudoUser)
	}
	return user.Current()
}
