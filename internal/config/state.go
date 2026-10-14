package config

import (
	"os"
	"path/filepath"
)

// State is where CONFIG_DIR stands for one host name.
type State int

const (
	StateReady State = iota
	StateNoConfig
	StateNoHost
)

// ConfigState classifies configDir for host: a missing or empty folder is
// StateNoConfig, one without hostsDir/<host>/modules/default.nix is StateNoHost.
func ConfigState(configDir, hostsDir, host string) (State, error) {
	entries, err := os.ReadDir(configDir)
	if os.IsNotExist(err) {
		return StateNoConfig, nil
	}
	if err != nil {
		return 0, err
	}
	if len(entries) == 0 {
		return StateNoConfig, nil
	}
	_, err = os.Stat(SelectionPath(filepath.Join(hostsDir, host)))
	if os.IsNotExist(err) {
		return StateNoHost, nil
	}
	if err != nil {
		return 0, err
	}
	return StateReady, nil
}

// Hosts lists the hosts under hostsDir, sorted: every directory
// holding a selection.
func Hosts(hostsDir string) ([]string, error) {
	return scopeFolders(hostsDir)
}

// scopeFolders lists the directories under root holding a SelectionPath,
// sorted; a missing root gives none.
func scopeFolders(root string) ([]string, error) {
	entries, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if _, err := os.Stat(SelectionPath(filepath.Join(root, e.Name()))); err == nil {
			names = append(names, e.Name())
		}
	}
	return names, nil
}
