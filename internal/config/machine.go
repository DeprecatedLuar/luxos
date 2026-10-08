package config

import (
	"fmt"
	"io/fs"
	"path/filepath"

	"github.com/DeprecatedLuar/luxos/internal/nix"
	"github.com/DeprecatedLuar/luxos/internal/templates"
)

const (
	starterModulesDir    = "starters/modules"
	localPackagesStarter = "starters/local-packages.nix"
	plsDontTouchTemplate = "plsdonttouch.nix.tmpl"
	selectionTemplate    = "modules.nix.tmpl"
	localPackagesFile    = "packages.nix"
	parseCheckPattern    = "luxos-setup-*.nix"
)

// Machine is what setup writes for a new machine. Selection holds import
// paths without "./", e.g. "local/hardware-support".
type Machine struct {
	Name         string
	StateVersion string
	Values       MachineValues
	Selection    []string
}

// CopyStarterModules writes every embedded starter module into modulesDir,
// keeping the layout. An existing file is an error, never overwritten.
func CopyStarterModules(modulesDir string) error {
	src, err := templates.Dir(starterModulesDir)
	if err != nil {
		return err
	}
	return fs.WalkDir(src, ".", func(rel string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		dst := filepath.Join(modulesDir, filepath.FromSlash(rel))
		if d.IsDir() {
			return MkdirAll(dst)
		}
		data, err := fs.ReadFile(src, rel)
		if err != nil {
			return err
		}
		created, err := CreateFile(dst, data)
		if err != nil {
			return err
		}
		if !created {
			return fmt.Errorf("%s already exists", dst)
		}
		return nil
	})
}

// CreateMachine writes machinesDir/<m.Name>/, which must not exist. Every file
// is rendered and, when nix-instantiate is available, parsed before the folder
// is created.
func CreateMachine(machinesDir string, m Machine) error {
	hostDir := filepath.Join(machinesDir, m.Name)
	files, err := checkedMachineFiles(m)
	if err != nil {
		return err
	}

	if err := MkdirAll(machinesDir); err != nil {
		return err
	}
	if err := Mkdir(hostDir); err != nil {
		return err
	}
	if err := Mkdir(filepath.Join(hostDir, localModulesDir)); err != nil {
		return err
	}
	for _, f := range files {
		if err := WriteFile(filepath.Join(hostDir, f.name), f.data); err != nil {
			return err
		}
	}
	_, err = ProtectHost(hostDir)
	return err
}

// CheckMachine renders m and, when nix-instantiate is available, parses every
// file, so a caller can refuse a machine before writing anything else.
func CheckMachine(m Machine) error {
	_, err := checkedMachineFiles(m)
	return err
}

func checkedMachineFiles(m Machine) ([]machineFile, error) {
	files, err := machineFiles(m)
	if err != nil {
		return nil, err
	}
	for _, f := range files {
		if err := checkParses(f.name, f.data); err != nil {
			return nil, err
		}
	}
	return files, nil
}

type machineFile struct {
	name string
	data []byte
}

func machineFiles(m Machine) ([]machineFile, error) {
	machine, err := RenderMachine(m.Values)
	if err != nil {
		return nil, err
	}
	stateVersion, err := templates.Render(plsDontTouchTemplate, m.StateVersion)
	if err != nil {
		return nil, err
	}
	selection, err := templates.Render(selectionTemplate, m.Selection)
	if err != nil {
		return nil, err
	}
	packages, err := templates.File(localPackagesStarter)
	if err != nil {
		return nil, err
	}
	return []machineFile{
		{plsDontTouchFile, stateVersion},
		{MachineFile, machine},
		{SelectionFile, selection},
		{filepath.Join(localModulesDir, localPackagesFile), packages},
	}, nil
}

// checkParses fails when data, written as name, would not parse. Without
// nix-instantiate there is nothing to check with and the build reports it.
func checkParses(name string, data []byte) error {
	if !nix.Available() {
		return nil
	}
	return nix.WithTemp(parseCheckPattern, data, func(path string) error {
		if _, err := nix.Parse(path); err != nil {
			return fmt.Errorf("%s would not parse: %w", name, err)
		}
		return nil
	})
}
