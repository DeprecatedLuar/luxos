package setup

import (
	"errors"
	"io"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/DeprecatedLuar/luxos/internal/commands/shared"
	"github.com/DeprecatedLuar/luxos/internal/config"
	"github.com/DeprecatedLuar/luxos/internal/hardware"
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

	_, err := Launch(ui.NewPrompter(strings.NewReader("y\n"), io.Discard), io.Discard, p, "nixos", config.StateNoHost)
	if err == nil || err.Error() != shared.NoHostError(p.Config, p.Hosts, "nixos").Error() {
		t.Errorf("err = %v, want the no-host error", err)
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

func captureBoth(t *testing.T, fn func()) (stdout, stderr string) {
	t.Helper()
	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	errR, errW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	origOut, origErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = outW, errW
	defer func() { os.Stdout, os.Stderr = origOut, origErr }()
	fn()
	outW.Close()
	errW.Close()
	o, _ := io.ReadAll(outR)
	e, _ := io.ReadAll(errR)
	return string(o), string(e)
}

func TestRun_HelpFlag(t *testing.T) {
	var err error
	stdout, stderr := captureBoth(t, func() { err = Run([]string{"-h"}) })
	if err != nil || !strings.Contains(stdout, "luxos setup") || stderr != "" {
		t.Errorf("err = %v, stdout = %q, stderr = %q", err, stdout, stderr)
	}
}

func TestRun_ExtraArgument(t *testing.T) {
	var err error
	stdout, stderr := captureBoth(t, func() { err = Run([]string{"extra"}) })
	var code shared.ExitCode
	if !errors.As(err, &code) || int(code) != 2 {
		t.Fatalf("err = %v, want exit code 2", err)
	}
	if stdout != "" || !strings.Contains(stderr, `unexpected argument "extra"`) || !strings.Contains(stderr, "luxos setup") {
		t.Errorf("stdout = %q, stderr = %q", stdout, stderr)
	}
}

func TestStoreKeyArgs_PassConfigDir(t *testing.T) {
	got := storeKeyArgs("/c", "/bin/luxos", "ana")
	want := []string{"LUXOS_CONFIG_DIR=/c", "/bin/luxos", "key", "add", "ana"}
	if !slices.Equal(got, want) {
		t.Errorf("args = %q, want %q", got, want)
	}
}

func TestLaunch_LiveBootRefused(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("runs as a normal user")
	}
	p := testPaths(t)
	mustWrite(t, p.Mounts, "/dev/loop0 /nix/.ro-store squashfs ro 0 0\n")
	defer func(f func(*os.File) bool) { isTerminal = f }(isTerminal)
	isTerminal = func(*os.File) bool { return true }

	_, err := Launch(ui.NewPrompter(strings.NewReader("n\n"), io.Discard), io.Discard, p, "nixos", config.StateNoConfig)
	if !errors.Is(err, hardware.ErrLiveSystem) {
		t.Fatalf("err = %v, want ErrLiveSystem", err)
	}
	if _, err := os.Stat(p.Config); !os.IsNotExist(err) {
		t.Errorf("config written on a live boot: %v", err)
	}
}
