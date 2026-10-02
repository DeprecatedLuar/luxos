package shared

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/DeprecatedLuar/luxos/internal/config"
	"github.com/DeprecatedLuar/luxos/internal/paths"
)

const (
	configOpt  = "config"
	machineOpt = "machine"
)

// ResolveHost applies --config and returns the paths, host name and host
// folder for --machine, defaulting to this computer's hostname.
func ResolveHost(opts map[string]string) (p paths.Paths, host, hostDir string, err error) {
	if opts[configOpt] != "" {
		abs, err := filepath.Abs(opts[configOpt])
		if err != nil {
			return p, "", "", err
		}
		if fi, err := os.Stat(abs); err != nil || !fi.IsDir() {
			return p, "", "", fmt.Errorf("config dir %s does not exist", abs)
		}
		if err := os.Setenv(paths.ConfigDirEnv, abs); err != nil {
			return p, "", "", err
		}
	}

	host = opts[machineOpt]
	if host == "" {
		host, err = os.Hostname()
		if err != nil {
			return p, "", "", err
		}
	}

	p, err = paths.Resolve()
	if err != nil {
		return p, "", "", err
	}

	hostDir, err = config.ResolveHost(p.Machines, host)
	if err != nil {
		return p, "", "", err
	}
	return p, host, hostDir, nil
}
