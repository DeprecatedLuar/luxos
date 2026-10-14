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
	noHostFormat = "I couldn't find a %q host in %s"
	noHostHas    = "\n(has %s)."
	noHostNone   = "."
	noHostTail   = "\nRun `luxos setup`, or pass --host <name>."
	hostListSep  = ", "
)

func NoConfigError(configDir string) error {
	return fmt.Errorf(noConfigFormat, configDir)
}

// NoHostError names the hosts configDir does have, when it has any.
func NoHostError(configDir, hostsDir, host string) error {
	names, err := config.Hosts(hostsDir)
	if err != nil {
		return err
	}
	msg := fmt.Sprintf(noHostFormat, host, configDir)
	if len(names) > 0 {
		msg += fmt.Sprintf(noHostHas, strings.Join(names, hostListSep))
	} else {
		msg += noHostNone
	}
	return errors.New(msg + noHostTail)
}

// ActiveHost is config.ActiveHost; when the modules/local link is missing it
// reports a missing config or host for this computer's hostname instead.
func ActiveHost(p paths.Paths) (string, error) {
	host, err := config.ActiveHost(p.Modules)
	if err == nil {
		return host, nil
	}
	hostname, herr := os.Hostname()
	if herr != nil {
		return "", errors.Join(err, herr)
	}
	state, serr := config.ConfigState(p.Config, p.Hosts, hostname)
	if serr != nil {
		return "", serr
	}
	switch state {
	case config.StateNoConfig:
		return "", NoConfigError(p.Config)
	case config.StateNoHost:
		return "", NoHostError(p.Config, p.Hosts, hostname)
	}
	return "", err
}
