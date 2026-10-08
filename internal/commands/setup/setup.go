package setup

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/DeprecatedLuar/luxos/internal/commands/help"
	"github.com/DeprecatedLuar/luxos/internal/commands/shared"
	"github.com/DeprecatedLuar/luxos/internal/config"
	"github.com/DeprecatedLuar/luxos/internal/paths"
	"github.com/DeprecatedLuar/luxos/internal/shell"
	"github.com/DeprecatedLuar/luxos/internal/ui"
)

const (
	setupFlagSpec    = "config|C:value"
	setupUsage       = "usage: luxos setup [--config|-C <dir>]"
	systemRoot       = "/"
	rebuildNowPrompt = "Rebuild now? [Y/n] "
	nextHint         = "next: luxos rebuild switch --machine %s\n"
	alreadySetUp     = "Setup is done: %s has a %q machine. Build it with: luxos rebuild switch\n"
	rebuildCmd       = "rebuild"
	rebuildAction    = "switch"
	machineFlag      = "--machine"
)

var errRoot = errors.New("luxos setup writes your config and your user module, so it runs as you: run it again without sudo")

// Swapped by tests.
var (
	isTerminal = ui.IsTerminal
	geteuid    = os.Geteuid
	detect     = Detect
)

// Launch runs the wizard for state and returns the machine to build. Without
// a terminal, or when input ends, it fails with state's own error.
func Launch(ask *ui.Prompter, out io.Writer, p paths.Paths, host string, state config.State) (string, error) {
	if geteuid() == 0 {
		return "", errRoot
	}
	stateErr := shared.NoConfigError(p.Config)
	if state == config.StateNoMachine {
		stateErr = shared.NoMachineError(p.Config, p.Machines, host)
	}
	if !isTerminal(os.Stdin) {
		return "", stateErr
	}

	sys, err := detect(systemRoot)
	if err != nil {
		return "", err
	}
	w := &Wizard{Ask: ask, Out: out, Paths: p, System: sys, Clone: Clone}

	var name string
	if state == config.StateNoConfig {
		name, err = w.Fresh()
	} else {
		name, err = w.MachineScreen()
	}
	if errors.Is(err, io.EOF) {
		return "", stateErr
	}
	return name, err
}

// Run is `luxos setup`: the wizard for this computer's hostname, then an
// offer to rebuild.
func Run(args []string) error {
	if len(args) > 0 && (args[0] == "--help" || args[0] == "-h") {
		return help.Run([]string{"help", "setup"})
	}
	opts, rest, err := shared.Parse(setupFlagSpec, args)
	if err != nil {
		return err
	}
	if len(rest) > 0 {
		return errors.New(setupUsage)
	}

	p, host, err := shared.Target(opts)
	if err != nil {
		return err
	}
	state, err := config.ConfigState(p.Config, p.Machines, host)
	if err != nil {
		return err
	}
	if state == config.StateReady {
		fmt.Printf(alreadySetUp, p.Config, host)
		return nil
	}

	ask := ui.NewPrompter(os.Stdin, os.Stdout)
	name, err := Launch(ask, os.Stdout, p, host, state)
	if err != nil {
		return err
	}

	reply, err := ask.Ask(rebuildNowPrompt)
	if errors.Is(err, io.EOF) || (err == nil && reply != "" && !ui.IsYes(reply)) {
		fmt.Printf(nextHint, name)
		return nil
	}
	if err != nil {
		return err
	}
	exe, err := shell.Self()
	if err != nil {
		return err
	}
	return shell.Exec(exe, []string{rebuildCmd, rebuildAction, machineFlag, name})
}
