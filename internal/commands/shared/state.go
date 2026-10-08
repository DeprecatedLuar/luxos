package shared

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/DeprecatedLuar/luxos/internal/config"
	"github.com/DeprecatedLuar/luxos/internal/paths"
)

const (
	noConfigFormat  = "There's no luxos config at %s yet.\nRun `luxos setup` to create one, or pass --config <dir>."
	noMachineFormat = "I couldn't find a %q machine in %s"
	noMachineHas    = "\n(has %s)."
	noMachineNone   = "."
	noMachineTail   = "\nRun `luxos setup`, or pass --machine <name>."
	machineListSep  = ", "
)

func NoConfigError(configDir string) error {
	return fmt.Errorf(noConfigFormat, configDir)
}

// NoMachineError names the machines configDir does have, when it has any.
func NoMachineError(configDir, machinesDir, host string) error {
	names, err := config.Machines(machinesDir)
	if err != nil {
		return err
	}
	msg := fmt.Sprintf(noMachineFormat, host, configDir)
	if len(names) > 0 {
		msg += fmt.Sprintf(noMachineHas, strings.Join(names, machineListSep))
	} else {
		msg += noMachineNone
	}
	return errors.New(msg + noMachineTail)
}

// ActiveHost is config.ActiveHost; when the modules/local link is missing it
// reports a missing config or machine for this computer's hostname instead.
func ActiveHost(p paths.Paths) (string, error) {
	host, err := config.ActiveHost(p.Modules)
	if err == nil {
		return host, nil
	}
	hostname, herr := os.Hostname()
	if herr != nil {
		return "", errors.Join(err, herr)
	}
	state, serr := config.ConfigState(p.Config, p.Machines, hostname)
	if serr != nil {
		return "", serr
	}
	switch state {
	case config.StateNoConfig:
		return "", NoConfigError(p.Config)
	case config.StateNoMachine:
		return "", NoMachineError(p.Config, p.Machines, hostname)
	}
	return "", err
}
