// Package paths resolves every directory luxos uses from the environment.
// Only internal/commands/* call Resolve(); every other package takes
// directories as parameters so it can be tested against t.TempDir().
package paths

import (
	"os"
	"os/user"
	"path/filepath"
)

// The only overrides; commands set them to carry flags across the sudo re-exec.
const (
	ConfigDirEnv = "LUXOS_CONFIG_DIR"
	BackupDirEnv = "LUXOS_BACKUP_DIR"
)

const (
	sudoUserEnv      = "SUDO_USER"
	homeEnv          = "HOME"
	xdgConfigHomeEnv = "XDG_CONFIG_HOME"

	configDirName   = "luxos"
	backupDirName   = "your-old-nixos-config-is-here"
	configRelToHome = ".config/luxos"
	hostsRel        = ".local/hosts"
	machinesRel     = ".local/machines"
	modulesRel      = "modules"

	staging           = "/etc/nixos"
	keysDir           = "/var/lib/luxos/keys"
	sysDir            = "/sys"
	procDir           = "/proc"
	devDir            = "/dev"
	mountsFile        = "/proc/mounts"
	runningSelections = "/run/current-system/luxos/modules"
	newStage          = "/etc/.nixos-luxos-new"
	oldStage          = "/etc/.nixos-luxos-old"
)

type Paths struct {
	Home, User        string // invoking user: $SUDO_USER's passwd entry when set (ignoring $HOME); else user.Current(), with $HOME overriding its HomeDir when set
	Config            string // $LUXOS_CONFIG_DIR, else $XDG_CONFIG_HOME/luxos, else <Home>/.config/luxos
	SudoUser          bool   // $SUDO_USER was set: Home is the invoking user's, not root's
	Backup            string // $LUXOS_BACKUP_DIR, else <Home>/your-old-nixos-config-is-here when $SUDO_USER is set, else empty (no invoking user)
	Hosts             string // <Config>/.local/hosts
	Modules           string // <Config>/modules
	Staging           string // /etc/nixos, the flake root
	Keys              string // /var/lib/luxos/keys, the root-only folder of user age keys
	Machines          string // <Config>/.local/machines
	Sys               string // /sys
	Proc              string // /proc
	Dev               string // /dev
	Mounts            string // /proc/mounts
	RunningSelections string // the running generation's copy of modules/host/default.nix and modules/machine/default.nix
	NewStage          string // /etc/.nixos-luxos-new, where rebuild builds a stage before promoting it to Staging
	OldStage          string // /etc/.nixos-luxos-old, Staging while a promotion swaps it out
}

func Resolve() (Paths, error) {
	u, err := invokingUser()
	if err != nil {
		return Paths{}, err
	}

	config := os.Getenv(ConfigDirEnv)
	if config == "" {
		if xdgConfigHome := os.Getenv(xdgConfigHomeEnv); xdgConfigHome != "" {
			config = filepath.Join(xdgConfigHome, configDirName)
		} else {
			config = filepath.Join(u.HomeDir, configRelToHome)
		}
	}

	sudoUser := os.Getenv(sudoUserEnv) != ""
	backup := os.Getenv(BackupDirEnv)
	if backup == "" && sudoUser {
		backup = filepath.Join(u.HomeDir, backupDirName)
	}

	return Paths{
		Home:              u.HomeDir,
		User:              u.Username,
		SudoUser:          sudoUser,
		Backup:            backup,
		Config:            config,
		Hosts:             filepath.Join(config, hostsRel),
		Modules:           filepath.Join(config, modulesRel),
		Staging:           staging,
		Keys:              keysDir,
		Machines:          filepath.Join(config, machinesRel),
		Sys:               sysDir,
		Proc:              procDir,
		Dev:               devDir,
		Mounts:            mountsFile,
		RunningSelections: runningSelections,
		NewStage:          newStage,
		OldStage:          oldStage,
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
