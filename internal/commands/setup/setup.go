package setup

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/DeprecatedLuar/luxos/internal/commands/help"
	"github.com/DeprecatedLuar/luxos/internal/commands/shared"
	"github.com/DeprecatedLuar/luxos/internal/config"
	"github.com/DeprecatedLuar/luxos/internal/paths"
	"github.com/DeprecatedLuar/luxos/internal/shell"
	"github.com/DeprecatedLuar/luxos/internal/ui"
)

const (
	setupFlagSpec    = "config|C:value"
	setupCommand     = "setup"
	unexpectedArg    = "unexpected argument %q"
	systemRoot       = "/"
	rebuildNowPrompt = "Rebuild now? [Y/n] "
	nextHint         = "next: luxos rebuild switch --host %s"
	alreadySetUp     = "Setup is done: %s has a %q host. Build it with: luxos rebuild switch"
	rebuildCmd       = "rebuild"
	rebuildAction    = "switch"
	hostFlag         = "--host"
	sudoBin          = "sudo"
)

var errRoot = errors.New("luxos setup writes your config and your user module, so it runs as you: run it again without sudo")

// Swapped by tests.
var (
	isTerminal = ui.IsTerminal
	geteuid    = os.Geteuid
	detect     = Detect
)

// Launch runs the wizard for state and returns the host to build. Without
// a terminal, or when input ends, it fails with state's own error.
func Launch(ask *ui.Prompter, out io.Writer, p paths.Paths, host string, state config.State) (string, error) {
	if geteuid() == 0 {
		return "", errRoot
	}
	stateErr := shared.NoConfigError(p.Config)
	if state == config.StateNoHost {
		stateErr = shared.NoHostError(p.Config, p.Hosts, host)
	}
	if !isTerminal(os.Stdin) {
		return "", stateErr
	}

	sys, err := detect(systemRoot)
	if err != nil {
		return "", err
	}
	w := &Wizard{Ask: ask, Out: out, Paths: p, System: sys, Clone: Clone,
		Secret:   func(prompt string) (string, error) { return ui.AskSecret(os.Stdin, out, prompt) },
		StoreKey: func(u, s string) error { return StoreKey(p.Config, u, s) },
	}

	var name string
	if state == config.StateNoConfig {
		name, err = w.Fresh()
	} else {
		name, err = w.HostScreen()
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
		return help.Command(os.Stdout, setupCommand)
	}
	opts, rest, err := shared.Parse(setupFlagSpec, args)
	if err != nil {
		return err
	}
	if len(rest) > 0 {
		return help.Misuse(os.Stderr, setupCommand, fmt.Errorf(unexpectedArg, rest[0]))
	}

	p, host, err := shared.Target(opts)
	if err != nil {
		return err
	}
	state, err := config.ConfigState(p.Config, p.Hosts, host)
	if err != nil {
		return err
	}
	if state == config.StateReady {
		ui.Text(os.Stdout, fmt.Sprintf(alreadySetUp, p.Config, host))
		return nil
	}

	ask := ui.NewPrompter(os.Stdin, os.Stdout)
	name, err := Launch(ask, os.Stdout, p, host, state)
	if err != nil {
		return err
	}

	reply, err := ask.Ask(rebuildNowPrompt)
	if errors.Is(err, io.EOF) || (err == nil && reply != "" && !ui.IsYes(reply)) {
		ui.Text(os.Stdout, fmt.Sprintf(nextHint, name))
		return nil
	}
	if err != nil {
		return err
	}
	exe, err := shell.Self()
	if err != nil {
		return err
	}
	return shell.Exec(exe, []string{rebuildCmd, rebuildAction, hostFlag, name})
}

// StoreKey hands secret to `luxos key add user` under sudo, on stdin, with
// configDir as the config directory. sudo reads its own password from the
// terminal, not from stdin.
func StoreKey(configDir, user, secret string) error {
	self, err := shell.Self()
	if err != nil {
		return err
	}
	return shell.Run(shell.Cmd{Bin: sudoBin, Args: storeKeyArgs(configDir, self, user), Stdin: strings.NewReader(secret + "\n")})
}

// storeKeyArgs is the sudo command line for StoreKey; the config directory is
// set so key add finds the user's unit.
func storeKeyArgs(configDir, self, user string) []string {
	return []string{paths.ConfigDirEnv + "=" + configDir, self, "key", "add", user}
}
