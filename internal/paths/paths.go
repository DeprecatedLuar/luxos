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
	configDirEnv     = "LUXOS_CONFIG_DIR"
	backupDirEnv     = "LUXOS_BACKUP_DIR"
	sudoUserEnv      = "SUDO_USER"
	homeEnv          = "HOME"
	xdgConfigHomeEnv = "XDG_CONFIG_HOME"

	configDirName   = "luxos"
	backupDirName   = "your-old-nixos-config-is-here"
	configRelToHome = ".config/luxos"
	localRel        = ".local"
	modulesRel      = "modules"

	staging        = "/etc/nixos"
	hardwareConfig = "/etc/nixos/hardware-configuration.nix"
	bootConfig     = "/etc/nixos/boot.nix"
	sysDir         = "/sys"
	mountsFile     = "/proc/mounts"
	runningModules = "/run/current-system/luxos/modules.nix"
)

// Paths holds every directory luxos needs, resolved once per invocation.
type Paths struct {
	Home, User     string // invoking user: $SUDO_USER's passwd entry when set (ignoring $HOME); else user.Current(), with $HOME overriding its HomeDir when set
	Config         string // $LUXOS_CONFIG_DIR, else $XDG_CONFIG_HOME/luxos, else <Home>/.config/luxos
	SudoUser       bool   // $SUDO_USER was set: Home is the invoking user's, not root's
	Backup         string // $LUXOS_BACKUP_DIR, else <Home>/your-old-nixos-config-is-here when $SUDO_USER is set, else empty (no invoking user)
	Local          string // <Config>/.local
	Modules        string // <Config>/modules
	Staging        string // /etc/nixos, the flake root
	HardwareConfig string // /etc/nixos/hardware-configuration.nix
	BootConfig     string // /etc/nixos/boot.nix
	Sys            string // /sys
	Mounts         string // /proc/mounts
	RunningModules string // /run/current-system/luxos/modules.nix
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
		if xdgConfigHome := os.Getenv(xdgConfigHomeEnv); xdgConfigHome != "" {
			config = filepath.Join(xdgConfigHome, configDirName)
		} else {
			config = filepath.Join(u.HomeDir, configRelToHome)
		}
	}

	backup := os.Getenv(backupDirEnv)
	if backup == "" && os.Getenv(sudoUserEnv) != "" {
		backup = filepath.Join(u.HomeDir, backupDirName)
	}

	return Paths{
		Home:           u.HomeDir,
		User:           u.Username,
		SudoUser:       os.Getenv(sudoUserEnv) != "",
		Backup:         backup,
		Config:         config,
		Local:          filepath.Join(config, localRel),
		Modules:        filepath.Join(config, modulesRel),
		Staging:        staging,
		HardwareConfig: hardwareConfig,
		BootConfig:     bootConfig,
		Sys:            sysDir,
		Mounts:         mountsFile,
		RunningModules: runningModules,
	}, nil
}

func invokingUser() (*user.User, error) {
	if sudoUser := os.Getenv(sudoUserEnv); sudoUser != "" {
		// $HOME is unreliable under sudo (typically reset to /root without
		// -E), so the sudo user's passwd entry is authoritative here.
		return user.Lookup(sudoUser)
	}

	u, err := user.Current()
	if err != nil {
		return nil, err
	}
	if home := os.Getenv(homeEnv); home != "" {
		userCopy := *u
		userCopy.HomeDir = home
		u = &userCopy
	}
	return u, nil
}
