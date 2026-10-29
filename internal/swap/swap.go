// Package swap reads the running system's swap and adds a temporary zram
// device to it.
package swap

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/DeprecatedLuar/luxos/internal/shell"
)

const (
	modprobeBin = "modprobe"
	mkswapBin   = "mkswap"
	swaponBin   = "swapon"
	swapoffBin  = "swapoff"

	swapsFile = "swaps"

	zramModule        = "zram"
	zramHotAdd        = "class/zram-control/hot_add"
	zramHotRemove     = "class/zram-control/hot_remove"
	zramBlockDir      = "block"
	zramPrefix        = "zram"
	zramAlgorithmFile = "comp_algorithm"
	zramDisksizeFile  = "disksize"
	zramAlgorithm     = "zstd"

	// Above disk swap, so the kernel fills it first.
	zramPriority = "100"
)

// Zram is a swap device StartZram added.
type Zram struct {
	id     int
	sysDir string
	devDir string
}

// Device is the device's path under devDir.
func (z Zram) Device() string {
	return filepath.Join(z.devDir, z.name())
}

func (z Zram) name() string {
	return zramPrefix + strconv.Itoa(z.id)
}

// Active reports whether <procDir>/swaps lists any swap in use.
func Active(procDir string) (bool, error) {
	path := filepath.Join(procDir, swapsFile)
	data, err := os.ReadFile(path)
	if err != nil {
		return false, fmt.Errorf("cannot read %s: %w", path, err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	// The first line is the column header.
	return len(lines) > 1, nil
}

// StartZram adds a zram swap device of size bytes. It never uses zram0, which
// a NixOS generation with zramSwap configures on activation. On failure,
// whatever it set up is removed.
func StartZram(sysDir, devDir string, size int64) (Zram, error) {
	if _, err := shell.Output(shell.Cmd{Bin: modprobeBin, Args: []string{zramModule}}); err != nil {
		return Zram{}, err
	}
	raw, err := os.ReadFile(filepath.Join(sysDir, zramHotAdd))
	if err != nil {
		return Zram{}, fmt.Errorf("add a zram device: %w", err)
	}
	id, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil {
		return Zram{}, fmt.Errorf("%s: device id %q is not an integer", zramHotAdd, raw)
	}
	z := Zram{id: id, sysDir: sysDir, devDir: devDir}
	if err := z.setup(size); err != nil {
		return Zram{}, errors.Join(err, z.remove())
	}
	return z, nil
}

func (z Zram) setup(size int64) error {
	if z.id == 0 {
		return errors.New("zram0 is reserved for the system's own zram swap")
	}
	block := filepath.Join(z.sysDir, zramBlockDir, z.name())
	if err := os.WriteFile(filepath.Join(block, zramAlgorithmFile), []byte(zramAlgorithm), 0); err != nil {
		return fmt.Errorf("set %s compression: %w", z.name(), err)
	}
	if err := os.WriteFile(filepath.Join(block, zramDisksizeFile), []byte(strconv.FormatInt(size, 10)), 0); err != nil {
		return fmt.Errorf("set %s size: %w", z.name(), err)
	}
	if _, err := shell.Output(shell.Cmd{Bin: mkswapBin, Args: []string{z.Device()}}); err != nil {
		return err
	}
	_, err := shell.Output(shell.Cmd{Bin: swaponBin, Args: []string{"-p", zramPriority, z.Device()}})
	return err
}

// Stop turns the device's swap off and removes the device.
func (z Zram) Stop() error {
	if _, err := shell.Output(shell.Cmd{Bin: swapoffBin, Args: []string{z.Device()}}); err != nil {
		return err
	}
	return z.remove()
}

func (z Zram) remove() error {
	if err := os.WriteFile(filepath.Join(z.sysDir, zramHotRemove), []byte(strconv.Itoa(z.id)), 0); err != nil {
		return fmt.Errorf("remove %s: %w", z.name(), err)
	}
	return nil
}
