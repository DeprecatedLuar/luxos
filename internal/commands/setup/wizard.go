package setup

import (
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"

	"github.com/DeprecatedLuar/luxos/internal/commands/shared"
	"github.com/DeprecatedLuar/luxos/internal/config"
	"github.com/DeprecatedLuar/luxos/internal/modules"
	"github.com/DeprecatedLuar/luxos/internal/password"
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
	nameTaken     = "%s already exists, pick it from the list or choose another name"
	userEnabled   = "Enabled %s on %s."
	freshDone     = "Your config is at %s. Put it in git to use it on other computers."
	menuLine      = "  %s  %s"
	reviewLine    = "  %d  %-10s %s"

	setupPasswordPrompt = "Password for %s: "
	setupAgainPrompt    = "Retype password for %s: "
	setupStoreKeyFailed = "%s's password file is written but its key was not stored on this computer: %w\n  the key is shown above; store it with: luxos user key add %s"
)

var errSetupMismatch = errors.New("passwords do not match")

// Selection lines of a new host, without "./".
const (
	localPackagesSelection = "host/packages.nix"
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

	// Secret asks for a value without echoing it.
	Secret func(prompt string) (string, error)
	// StoreKey keeps the user's key on this computer.
	StoreKey func(user, secret string) error
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
	ui.Text(w.Out, hostsTitle)
	for i, n := range names {
		ui.Text(w.Out, fmt.Sprintf(menuLine, strconv.Itoa(i+1), n))
	}
	ui.Text(w.Out, fmt.Sprintf(menuLine, newHostKey, "new host"))
	ui.Text(w.Out, "")

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

	sel := append([]string{localPackagesSelection, unstableSelection}, w.userLines()...)
	sel = append(sel, d.selection...)
	m := w.host(name, values, sel)
	if err := config.CheckHost(m); err != nil {
		return "", err
	}

	pw, err := w.askPassword()
	if err != nil {
		return "", err
	}
	if err := config.MkdirAll(w.Paths.Config); err != nil {
		return "", err
	}
	if err := config.CopyStarterModules(w.Paths.Modules); err != nil {
		return "", err
	}
	if err := w.writeUser(pw); err != nil {
		return "", err
	}
	if err := config.CreateHost(w.Paths.Hosts, m); err != nil {
		return "", err
	}
	if err := w.writeGroups(name); err != nil {
		return "", err
	}
	ui.Done(w.Out, fmt.Sprintf(freshDone, w.Paths.Config))
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

	if err := config.CheckHost(w.host(name, values, []string{localPackagesSelection, w.userPath()})); err != nil {
		return "", err
	}

	userLines, err := w.ensureUserUnit()
	if err != nil {
		return "", err
	}
	sel := append([]string{localPackagesSelection}, userLines...)
	if err := config.CreateHost(w.Paths.Hosts, w.host(name, values, sel)); err != nil {
		return "", err
	}
	return name, w.writeGroups(name)
}

func (w *Wizard) host(name string, values config.HostValues, sel []string) config.HostSpec {
	return config.HostSpec{Name: name, StateVersion: w.System.Release, Values: values, Selection: sel}
}

// enableUser adds the user to host's selection, creating the unit when the
// config has none by that name.
func (w *Wizard) enableUser(host string) error {
	hostDir := filepath.Join(w.Paths.Hosts, host)
	h, err := modules.Load(w.Paths.Modules, hostDir, "")
	if err != nil {
		return err
	}
	if _, ok := h.Find(w.Paths.User); !ok {
		pw, err := w.askPassword()
		if err != nil {
			return err
		}
		if err := w.writeUser(pw); err != nil {
			return err
		}
		if h, err = modules.Load(w.Paths.Modules, hostDir, ""); err != nil {
			return err
		}
	}
	changed, err := modules.Enable(h, w.Paths.User, false)
	if err != nil {
		return err
	}
	if changed {
		ui.Done(w.Out, fmt.Sprintf(userEnabled, w.Paths.User, host))
	}
	return w.writeGroups(host)
}

// ensureUserUnit returns the selection lines of the user's unit: its path
// when the config has it, else the lines of the unit it creates.
func (w *Wizard) ensureUserUnit() ([]string, error) {
	ms, _, err := modules.Walk(w.Paths.Modules, "", "")
	if err != nil {
		return nil, err
	}
	if m, ok := modules.Find(ms, w.Paths.User); ok {
		return []string{m.Path}, nil
	}
	pw, err := w.askPassword()
	if err != nil {
		return nil, err
	}
	return w.userLines(), w.writeUser(pw)
}

func (w *Wizard) userPath() string {
	return usersCategory + "/" + w.Paths.User
}

// userLines are the selection lines of a user unit setup creates.
func (w *Wizard) userLines() []string {
	return []string{w.userPath()}
}

// askPassword asks the user's password twice; both must match and not be empty.
func (w *Wizard) askPassword() (string, error) {
	user := w.Paths.User
	pw, err := w.Secret(fmt.Sprintf(setupPasswordPrompt, user))
	if err != nil {
		return "", err
	}
	if pw == "" {
		return "", password.ErrEmpty
	}
	again, err := w.Secret(fmt.Sprintf(setupAgainPrompt, user))
	if err != nil {
		return "", err
	}
	if pw != again {
		return "", errSetupMismatch
	}
	return pw, nil
}

// writeUser creates the user unit with pw as its password: the encrypted
// hash goes into the unit; the new key is shown once, then handed to StoreKey.
func (w *Wizard) writeUser(pw string) error {
	user := w.Paths.User
	hash, err := password.Hash(pw)
	if err != nil {
		return err
	}
	secret, err := password.NewKey()
	if err != nil {
		return err
	}
	pub, err := password.PublicKey(secret)
	if err != nil {
		return err
	}
	enc, err := password.Encrypt(hash, pub)
	if err != nil {
		return err
	}
	dir := filepath.Join(w.Paths.Modules, usersCategory, user)
	if err := config.WriteUser(dir); err != nil {
		return err
	}
	if err := config.WriteFile(filepath.Join(dir, password.File), enc); err != nil {
		return err
	}
	shared.KeyNotice(w.Out, user, secret)
	if err := w.StoreKey(user, secret); err != nil {
		return fmt.Errorf(setupStoreKeyFailed, user, err, user)
	}
	return nil
}

// writeGroups gives the user its groups on host unless the host already has
// a settings file for the user.
func (w *Wizard) writeGroups(host string) error {
	dir := filepath.Join(w.Paths.Hosts, host, config.SettingsDir)
	if err := config.MkdirAll(dir); err != nil {
		return err
	}
	_, err := config.CreateFile(filepath.Join(dir, w.Paths.User+".nix"), config.UserGroups(w.Paths.User))
	return err
}

func (w *Wizard) askName(existing []string) (string, error) {
	for {
		name, err := w.Ask.Ask(namePrompt)
		if err != nil {
			return "", err
		}
		switch {
		case !hostNameRe.MatchString(name):
			ui.Warn(w.Out, invalidName)
		case slices.Contains(existing, name):
			ui.Warn(w.Out, fmt.Sprintf(nameTaken, name))
		default:
			return name, nil
		}
	}
}

func (w *Wizard) askDesktop() (desktop, error) {
	ui.Text(w.Out, "")
	ui.Text(w.Out, desktopTitle)
	for i, d := range desktops {
		ui.Text(w.Out, fmt.Sprintf(menuLine, strconv.Itoa(i+1), d.label))
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
		ui.Text(w.Out, "")
		for i, r := range rows {
			ui.Text(w.Out, fmt.Sprintf(reviewLine, i+1, r.label, *r.value))
		}
		if d != nil {
			ui.Text(w.Out, fmt.Sprintf(reviewLine, len(rows)+1, desktopRow, d.name))
		}
		ui.Text(w.Out, "")

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
