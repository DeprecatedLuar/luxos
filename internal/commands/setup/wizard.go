package setup

import (
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"

	"github.com/DeprecatedLuar/luxos/internal/config"
	"github.com/DeprecatedLuar/luxos/internal/modules"
	"github.com/DeprecatedLuar/luxos/internal/paths"
	"github.com/DeprecatedLuar/luxos/internal/ui"
)

const (
	sourcePrompt  = "Do you already have a luxos config in a git repo? [y/N] "
	repoPrompt    = "Repo URL: "
	cloneFixHint  = "use an https URL, or add an SSH key and run luxos setup again"
	machinesTitle = "Machines in your config:"
	newMachineKey = "n"
	machinePrompt = "Pick a machine: "
	namePrompt    = "Machine name: "
	desktopTitle  = "Desktop:"
	desktopPrompt = "Pick one: "
	desktopRow    = "desktop"
	reviewPrompt  = "Number to change, y to continue: "
	invalidName   = "a machine name is letters, digits and -, 1 to 63 characters, not starting or ending with -"
	nameTaken     = "%s already exists, pick it from the list or choose another name\n"
	userEnabled   = "Enabled %s on %s.\n"
	freshDone     = "Your config is at %s. Put it in git to use it on other computers.\n"
	menuLine      = "  %s  %s\n"
	reviewLine    = "  %d  %-10s %s\n"
)

// Selection lines of a new machine, without "./".
const (
	hardwareSelection      = "local/hardware-support"
	localPackagesSelection = "local/packages.nix"
	unstableSelection      = "unstable.nix"
	usersCategory          = "users"
)

var machineNameRe = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?$`)

type desktop struct {
	name, label string
	selection   []string
}

var desktops = []desktop{
	{"cinnamon", "cinnamon", []string{"desktop/compositors/cinnamon.nix", "desktop/greeters/lightdm.nix"}},
	{"xfce", "xfce", []string{"desktop/compositors/xfce.nix", "desktop/greeters/lightdm.nix"}},
	{"hyprland", "hyprland", []string{"desktop/compositors/hyprland.nix", "desktop/greeters/greetd.nix"}},
	{"console", "console (no graphical session)", nil},
}

// Wizard asks through Ask, prints to Out and writes into Paths' config.
type Wizard struct {
	Ask    *ui.Prompter
	Out    io.Writer
	Paths  paths.Paths
	System System
	Clone  func(url, dir string) error
}

// Fresh runs the whole wizard on an empty config and returns the machine to build.
func (w *Wizard) Fresh() (string, error) {
	reply, err := w.Ask.Ask(sourcePrompt)
	if err != nil {
		return "", err
	}
	if !ui.IsYes(reply) {
		return w.freshConfig()
	}
	url, err := w.askNonEmpty(repoPrompt)
	if err != nil {
		return "", err
	}
	if err := w.Clone(url, w.Paths.Config); err != nil {
		return "", fmt.Errorf("clone %s: %w\n  %s", url, err, cloneFixHint)
	}
	return w.MachineScreen()
}

// MachineScreen lets the user pick a machine of the config or create one.
func (w *Wizard) MachineScreen() (string, error) {
	names, err := config.Machines(w.Paths.Machines)
	if err != nil {
		return "", err
	}
	fmt.Fprintln(w.Out, machinesTitle)
	for i, n := range names {
		fmt.Fprintf(w.Out, menuLine, strconv.Itoa(i+1), n)
	}
	fmt.Fprintf(w.Out, menuLine, newMachineKey, "new machine")
	fmt.Fprintln(w.Out)

	for {
		reply, err := w.Ask.Ask(machinePrompt)
		if err != nil {
			return "", err
		}
		if reply == newMachineKey {
			return w.newMachine(names)
		}
		if i, err := strconv.Atoi(reply); err == nil && i >= 1 && i <= len(names) {
			return names[i-1], w.enableUser(names[i-1])
		}
	}
}

func (w *Wizard) freshConfig() (string, error) {
	name, err := w.askName(nil)
	if err != nil {
		return "", err
	}
	d, err := w.askDesktop()
	if err != nil {
		return "", err
	}
	values := w.System.Values
	if err := w.review(&values, &d); err != nil {
		return "", err
	}

	if err := config.MkdirAll(w.Paths.Config); err != nil {
		return "", err
	}
	if err := config.CopyStarterModules(w.Paths.Modules); err != nil {
		return "", err
	}
	if err := w.writeUser(); err != nil {
		return "", err
	}
	sel := append([]string{hardwareSelection, localPackagesSelection, unstableSelection, w.userPath()}, d.selection...)
	if err := config.CreateMachine(w.Paths.Machines, w.machine(name, values, sel)); err != nil {
		return "", err
	}
	fmt.Fprintf(w.Out, freshDone, w.Paths.Config)
	return name, nil
}

func (w *Wizard) newMachine(existing []string) (string, error) {
	name, err := w.askName(existing)
	if err != nil {
		return "", err
	}
	values := w.System.Values
	if err := w.review(&values, nil); err != nil {
		return "", err
	}

	userPath, err := w.ensureUserUnit()
	if err != nil {
		return "", err
	}
	sel := []string{hardwareSelection, localPackagesSelection, userPath}
	return name, config.CreateMachine(w.Paths.Machines, w.machine(name, values, sel))
}

func (w *Wizard) machine(name string, values config.MachineValues, sel []string) config.Machine {
	return config.Machine{Name: name, StateVersion: w.System.Release, Values: values, Selection: sel}
}

// enableUser adds the user to machine's selection, creating the unit when the
// config has none by that name.
func (w *Wizard) enableUser(machine string) error {
	hostDir := filepath.Join(w.Paths.Machines, machine)
	h, err := modules.Load(w.Paths.Modules, hostDir)
	if err != nil {
		return err
	}
	if _, ok := h.Find(w.Paths.User); !ok {
		if err := w.writeUser(); err != nil {
			return err
		}
		if h, err = modules.Load(w.Paths.Modules, hostDir); err != nil {
			return err
		}
	}
	changed, err := modules.Enable(h, w.Paths.User)
	if err != nil {
		return err
	}
	if changed {
		fmt.Fprintf(w.Out, userEnabled, w.Paths.User, machine)
	}
	return nil
}

// ensureUserUnit returns the selection path of the user's unit, creating it
// when the config has none by that name.
func (w *Wizard) ensureUserUnit() (string, error) {
	ms, err := modules.Walk(w.Paths.Modules, "")
	if err != nil {
		return "", err
	}
	if m, ok := modules.Find(ms, w.Paths.User); ok {
		return m.Path, nil
	}
	return w.userPath(), w.writeUser()
}

func (w *Wizard) userPath() string {
	return usersCategory + "/" + w.Paths.User
}

func (w *Wizard) writeUser() error {
	account, err := config.UserAccount(w.Paths.User, true)
	if err != nil {
		return err
	}
	return config.WriteUser(filepath.Join(w.Paths.Modules, usersCategory, w.Paths.User), w.Paths.User, account)
}

func (w *Wizard) askName(existing []string) (string, error) {
	for {
		name, err := w.Ask.Ask(namePrompt)
		if err != nil {
			return "", err
		}
		switch {
		case !machineNameRe.MatchString(name):
			fmt.Fprintln(w.Out, invalidName)
		case slices.Contains(existing, name):
			fmt.Fprintf(w.Out, nameTaken, name)
		default:
			return name, nil
		}
	}
}

func (w *Wizard) askDesktop() (desktop, error) {
	fmt.Fprintln(w.Out)
	fmt.Fprintln(w.Out, desktopTitle)
	for i, d := range desktops {
		fmt.Fprintf(w.Out, menuLine, strconv.Itoa(i+1), d.label)
	}
	for {
		reply, err := w.Ask.Ask(desktopPrompt)
		if err != nil {
			return desktop{}, err
		}
		if i, err := strconv.Atoi(reply); err == nil && i >= 1 && i <= len(desktops) {
			return desktops[i-1], nil
		}
	}
}

func (w *Wizard) askNonEmpty(prompt string) (string, error) {
	for {
		reply, err := w.Ask.Ask(prompt)
		if err != nil || reply != "" {
			return reply, err
		}
	}
}

type reviewRow struct {
	label string
	value *string
}

// review shows the detected values until the user answers y; a number
// re-asks that value as free text. d is nil when there is no desktop row.
func (w *Wizard) review(v *config.MachineValues, d *desktop) error {
	rows := []reviewRow{
		{"timezone", &v.Timezone},
		{"locale", &v.Locale},
		{"keyboard", &v.Keyboard},
		{"channel", &v.Channel},
	}
	for {
		fmt.Fprintln(w.Out)
		for i, r := range rows {
			fmt.Fprintf(w.Out, reviewLine, i+1, r.label, *r.value)
		}
		if d != nil {
			fmt.Fprintf(w.Out, reviewLine, len(rows)+1, desktopRow, d.name)
		}
		fmt.Fprintln(w.Out)

		reply, err := w.Ask.Ask(reviewPrompt)
		if err != nil {
			return err
		}
		if ui.IsYes(reply) {
			return nil
		}
		i, err := strconv.Atoi(reply)
		switch {
		case err != nil || i < 1:
		case i <= len(rows):
			value, err := w.askNonEmpty(rows[i-1].label + ": ")
			if err != nil {
				return err
			}
			*rows[i-1].value = value
		case d != nil && i == len(rows)+1:
			chosen, err := w.askDesktop()
			if err != nil {
				return err
			}
			*d = chosen
		}
	}
}
