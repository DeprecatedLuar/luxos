package config

import (
	"os"
	"path/filepath"
)

// State is where CONFIG_DIR stands for one machine name.
type State int

const (
	StateReady State = iota
	StateNoConfig
	StateNoMachine
)

// ConfigState classifies configDir for host: a missing or empty folder is
// StateNoConfig, one without machinesDir/<host>/modules.nix is StateNoMachine.
func ConfigState(configDir, machinesDir, host string) (State, error) {
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
	_, err = os.Stat(filepath.Join(machinesDir, host, SelectionFile))
	if os.IsNotExist(err) {
		return StateNoMachine, nil
	}
	if err != nil {
		return 0, err
	}
	return StateReady, nil
}

// Machines lists the machines under machinesDir, sorted: every directory
// holding modules.nix.
func Machines(machinesDir string) ([]string, error) {
	entries, err := os.ReadDir(machinesDir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if _, err := os.Stat(filepath.Join(machinesDir, e.Name(), SelectionFile)); err == nil {
			names = append(names, e.Name())
		}
	}
	return names, nil
}
