package setup

import (
	"io"
	"os"
	"strings"
	"testing"

	"github.com/DeprecatedLuar/luxos/internal/commands/shared"
	"github.com/DeprecatedLuar/luxos/internal/config"
	"github.com/DeprecatedLuar/luxos/internal/ui"
)

func TestLaunch_EndOfInputIsStateError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("runs as a normal user")
	}
	p := testPaths(t)
	// Launch checks the terminal on os.Stdin; isTerminal is swapped so the
	// prompts run and input ends at the first one.
	defer func(f func(*os.File) bool) { isTerminal = f }(isTerminal)
	isTerminal = func(*os.File) bool { return true }
	defer func(f func(string) (System, error)) { detect = f }(detect)
	detect = func(string) (System, error) { return testSystem, nil }

	_, err := Launch(ui.NewPrompter(strings.NewReader(""), io.Discard), io.Discard, p, "nixos", config.StateNoConfig)
	if err == nil || err.Error() != shared.NoConfigError(p.Config).Error() {
		t.Errorf("err = %v, want the no-config error", err)
	}
}

func TestLaunch_NoTerminalIsStateError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("runs as a normal user")
	}
	p := testPaths(t)
	defer func(f func(*os.File) bool) { isTerminal = f }(isTerminal)
	isTerminal = func(*os.File) bool { return false }

	_, err := Launch(ui.NewPrompter(strings.NewReader("y\n"), io.Discard), io.Discard, p, "nixos", config.StateNoMachine)
	if err == nil || err.Error() != shared.NoMachineError(p.Config, p.Machines, "nixos").Error() {
		t.Errorf("err = %v, want the no-machine error", err)
	}
}

func TestLaunch_RootRefused(t *testing.T) {
	p := testPaths(t)
	defer func(f func() int) { geteuid = f }(geteuid)
	geteuid = func() int { return 0 }

	_, err := Launch(ui.NewPrompter(strings.NewReader("n\n"), io.Discard), io.Discard, p, "nixos", config.StateNoConfig)
	if err == nil || !strings.Contains(err.Error(), "without sudo") {
		t.Errorf("err = %v, want the root refusal", err)
	}
	if _, err := os.Stat(p.Config); !os.IsNotExist(err) {
		t.Errorf("config written as root: %v", err)
	}
}
