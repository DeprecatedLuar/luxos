package shared

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/DeprecatedLuar/luxos/internal/config"
	"github.com/DeprecatedLuar/luxos/internal/paths"
)

const (
	configOpt = "config"
	hostOpt   = "host"
)

// Target applies --config and returns the paths and the host name for
// --host, defaulting to this computer's hostname. The config folder may be
// missing; an existing one must be a directory.
func Target(opts map[string]string) (p paths.Paths, host string, err error) {
	if opts[configOpt] != "" {
		abs, err := filepath.Abs(opts[configOpt])
		if err != nil {
			return p, "", err
		}
		if fi, err := os.Stat(abs); err == nil && !fi.IsDir() {
			return p, "", fmt.Errorf("config dir %s is not a directory", abs)
		}
		if err := os.Setenv(paths.ConfigDirEnv, abs); err != nil {
			return p, "", err
		}
	}

	host = opts[hostOpt]
	if host == "" {
		if host, err = os.Hostname(); err != nil {
			return p, "", err
		}
	}

	p, err = paths.Resolve()
	return p, host, err
}

// ResolveHost is Target plus the host folder. A missing config is
// NoConfigError unless --host was given; a missing host is NoHostError.
func ResolveHost(opts map[string]string) (p paths.Paths, host, hostDir string, err error) {
	p, host, err = Target(opts)
	if err != nil {
		return p, "", "", err
	}

	state, err := config.ConfigState(p.Config, p.Hosts, host)
	if err != nil {
		return p, "", "", err
	}
	if state == config.StateNoConfig && opts[hostOpt] == "" {
		return p, "", "", NoConfigError(p.Config)
	}

	hostDir, err = config.ResolveHost(p.Hosts, host)
	if errors.Is(err, config.ErrNoHost) {
		return p, "", "", NoHostError(p.Config, p.Hosts, host)
	}
	if err != nil {
		return p, "", "", err
	}
	return p, host, hostDir, nil
}
