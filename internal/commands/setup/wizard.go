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
	hostsTitle    = "Hosts in your config:"
	newHostKey    = "n"
	hostPrompt    = "Pick a host: "
	namePrompt    = "Host name: "
	desktopTitle  = "Desktop:"
	desktopPrompt = "Pick one: "
	desktopRow    = "desktop"
	reviewPrompt  = "Number to change, y to continue: "
	invalidName   = "a host name is letters, digits and -, 1 to 63 characters, not starting or ending with -"
	nameTaken     = "%s already exists, pick it from the list or choose another name\n"
	userEnabled   = "Enabled %s on %s.\n"
	freshDone     = "Your config is at %s. Put it in git to use it on other computers.\n"
	menuLine      = "  %s  %s\n"
	reviewLine    = "  %d  %-10s %s\n"
)

// Selection lines of a new host, without "./".
const (
	hardwareSelection      = "local/hardware-support"
	localPackagesSelection = "local/packages.nix"
	unstableSelection      = "unstable.nix"
	usersCategory          = "users"
)

var hostNameRe = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?$`)

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

// Fresh runs the whole wizard on an empty config and returns the host to build.
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
	return w.HostScreen()
}

// HostScreen lets the user pick a host of the config or create one.
func (w *Wizard) HostScreen() (string, error) {
	names, err := config.Hosts(w.Paths.Hosts)
	if err != nil {
		return "", err
	}
	fmt.Fprintln(w.Out, hostsTitle)
	for i, n := range names {
		fmt.Fprintf(w.Out, menuLine, strconv.Itoa(i+1), n)
	}
	fmt.Fprintf(w.Out, menuLine, newHostKey, "new host")
	fmt.Fprintln(w.Out)

	for {
		reply, err := w.Ask.Ask(hostPrompt)
		if err != nil {
			return "", err
		}
		if reply == newHostKey {
			return w.newHost(names)
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

	sel := append([]string{hardwareSelection, localPackagesSelection, unstableSelection, w.userPath()}, d.selection...)
	m := w.host(name, values, sel)
	if err := config.CheckHost(m); err != nil {
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
	if err := config.CreateHost(w.Paths.Hosts, m); err != nil {
		return "", err
	}
	fmt.Fprintf(w.Out, freshDone, w.Paths.Config)
	return name, nil
}

func (w *Wizard) newHost(existing []string) (string, error) {
	name, err := w.askName(existing)
	if err != nil {
		return "", err
	}
	values := w.System.Values
	if err := w.review(&values, nil); err != nil {
		return "", err
	}

	if err := config.CheckHost(w.host(name, values, []string{hardwareSelection, localPackagesSelection, w.userPath()})); err != nil {
		return "", err
	}

	userPath, err := w.ensureUserUnit()
	if err != nil {
		return "", err
	}
	sel := []string{hardwareSelection, localPackagesSelection, userPath}
	return name, config.CreateHost(w.Paths.Hosts, w.host(name, values, sel))
}

func (w *Wizard) host(name string, values config.HostValues, sel []string) config.HostSpec {
	return config.HostSpec{Name: name, StateVersion: w.System.Release, Values: values, Selection: sel}
}

// enableUser adds the user to host's selection, creating the unit when the
// config has none by that name.
func (w *Wizard) enableUser(host string) error {
	hostDir := filepath.Join(w.Paths.Hosts, host)
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
		fmt.Fprintf(w.Out, userEnabled, w.Paths.User, host)
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
		case !hostNameRe.MatchString(name):
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
func (w *Wizard) review(v *config.HostValues, d *desktop) error {
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
