package setup

import (
	"errors"
	"fmt"
	"io"
	"os"
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
	cloneFixHint  = "use an https URL, or add an SSH key"
	urlHint       = "use a full URL: https://…, ssh://… or git@host:owner/repo"
	cloneFailed   = "clone %s: %s"
	notLuxos      = "%s is not a luxos config: it has no host (.local/hosts/<name>/modules/default.nix)"
	putConfigHint = "put your config at %s yourself (a symlink works), or pass --config <dir>"
	clonePrefix   = ".luxos-clone-"
	hostsTitle    = "Hosts in your config:"
	newHostKey    = "n"
	hostPrompt    = "Pick a host: "
	hostInvalid   = "pick a number or a host name from the list"
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

	channelTitle    = "Channel:"
	channelPrompt   = "Pick one: "
	customPrompt    = "Channel (YY.MM): "
	channelRow      = "channel"
	latestLabel     = "%s (latest)"
	currentLabel    = "%s (current)"
	customLabel     = "custom"
	latestFailed    = "could not fetch the latest NixOS channel: %s"
	unstableChannel = "nixos-unstable"

	setupPasswordPrompt = "Password for %s: "
	setupAgainPrompt    = "Retype password for %s: "
	setupStoreKeyFailed = "%s's password file is written but its key was not stored on this computer: %w\n  the key is shown above; store it with: luxos user key add %s"
)

// Selection lines of a new host, without "./".
const (
	localPackagesSelection = "host/packages.nix"
	unstableSelection      = "unstable.nix"
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
	// Latest returns the newest stable channel.
	Latest func() (string, error)

	latest      string
	latestErr   error
	latestAsked bool
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
	if err := w.cloneRepo(); err != nil {
		return "", err
	}
	return w.HostScreen()
}

// cloneRepo asks for a repo URL until one clones into a luxos config, then
// moves it to the config directory. A clone is made beside it, so a failed or
// foreign repo leaves nothing behind.
func (w *Wizard) cloneRepo() error {
	parent := filepath.Dir(w.Paths.Config)
	hostsRel, err := filepath.Rel(w.Paths.Config, w.Paths.Hosts)
	if err != nil {
		return err
	}
	for {
		url, err := w.askNonEmpty(repoPrompt)
		if err != nil {
			return err
		}
		if !isRemoteURL(url) {
			ui.Warn(w.Out, urlHint)
			continue
		}
		if err := config.MkdirAll(parent); err != nil {
			return err
		}
		tmp, err := os.MkdirTemp(parent, clonePrefix)
		if err != nil {
			return err
		}
		if cloneErr := w.Clone(url, tmp); cloneErr != nil {
			if err := os.RemoveAll(tmp); err != nil {
				return errors.Join(cloneErr, err)
			}
			ui.Warn(w.Out, fmt.Sprintf(cloneFailed, url, cloneErr))
			ui.Text(w.Out, "  "+cloneFixHint)
			continue
		}
		hosts, err := config.Hosts(filepath.Join(tmp, hostsRel))
		if err != nil {
			return errors.Join(err, os.RemoveAll(tmp))
		}
		if len(hosts) == 0 {
			if err := os.RemoveAll(tmp); err != nil {
				return err
			}
			ui.Warn(w.Out, fmt.Sprintf(notLuxos, url))
			ui.Text(w.Out, "  "+fmt.Sprintf(putConfigHint, w.Paths.Config))
			continue
		}
		// os.Rename will not replace a directory; Remove takes only an empty one.
		if err := os.Remove(w.Paths.Config); err != nil && !os.IsNotExist(err) {
			return errors.Join(err, os.RemoveAll(tmp))
		}
		if err := os.Rename(tmp, w.Paths.Config); err != nil {
			return errors.Join(err, os.RemoveAll(tmp))
		}
		return nil
	}
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
		if slices.Contains(names, reply) {
			return reply, w.enableUser(reply)
		}
		ui.Warn(w.Out, hostInvalid)
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
	if values.Channel, err = w.askChannel(); err != nil {
		return "", err
	}
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
	if values.Channel, err = w.askChannel(); err != nil {
		return "", err
	}
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
	_, ok, err := modules.FindUser(h.Modules, w.Paths.User)
	if err != nil {
		return err
	}
	if !ok {
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
	added, err := modules.Enable(h, w.Paths.User, false)
	if err != nil {
		return err
	}
	if len(added) > 0 {
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
	m, ok, err := modules.FindUser(ms, w.Paths.User)
	if err != nil {
		return nil, err
	}
	if ok {
		return []string{m.Path}, nil
	}
	pw, err := w.askPassword()
	if err != nil {
		return nil, err
	}
	return w.userLines(), w.writeUser(pw)
}

func (w *Wizard) userPath() string {
	return modules.UserPath(w.Paths.User)
}

// userLines are the selection lines of a user unit setup creates.
func (w *Wizard) userLines() []string {
	return []string{w.userPath()}
}

// askPassword asks the user's password twice through the shared prompt.
func (w *Wizard) askPassword() (string, error) {
	user := w.Paths.User
	return shared.AskNewPassword(w.Secret, fmt.Sprintf(setupPasswordPrompt, user), fmt.Sprintf(setupAgainPrompt, user))
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
	enc, err := password.Seal(hash, secret)
	if err != nil {
		return err
	}
	dir := filepath.Join(w.Paths.Modules, modules.UserPath(user))
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
	file := config.SettingsPath(filepath.Join(w.Paths.Hosts, host), w.Paths.User)
	if err := config.MkdirAll(filepath.Dir(file)); err != nil {
		return err
	}
	_, err := config.CreateFile(file, config.UserGroups(w.Paths.User))
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

// latestChannel asks Latest once; a failure is warned about once and kept.
func (w *Wizard) latestChannel() (string, error) {
	if !w.latestAsked {
		w.latestAsked = true
		w.latest, w.latestErr = w.Latest()
		if w.latestErr != nil {
			ui.Warn(w.Out, fmt.Sprintf(latestFailed, w.latestErr))
		}
	}
	return w.latest, w.latestErr
}

type channelOption struct {
	label, value string
	preferred    bool // Enter picks it
}

// channelOptions lists the channels on offer: the latest stable, the
// running release, unstable; custom is the caller's last row.
func (w *Wizard) channelOptions() []channelOption {
	var opts []channelOption
	latest, err := w.latestChannel()
	if err == nil {
		opts = append(opts, channelOption{fmt.Sprintf(latestLabel, latest), latest, true})
	}
	if current, perr := config.ParseChannel(w.System.Release); perr == nil && !(err == nil && current == latest) {
		opts = append(opts, channelOption{fmt.Sprintf(currentLabel, current), current, true})
	}
	return append(opts, channelOption{unstableChannel, unstableChannel, false})
}

// askChannel asks the base channel: Enter takes the first row when it is the
// latest or the running release.
func (w *Wizard) askChannel() (string, error) {
	opts := w.channelOptions()
	ui.Text(w.Out, "")
	ui.Text(w.Out, channelTitle)
	for i, o := range opts {
		ui.Text(w.Out, fmt.Sprintf(menuLine, strconv.Itoa(i+1), o.label))
	}
	custom := len(opts) + 1
	ui.Text(w.Out, fmt.Sprintf(menuLine, strconv.Itoa(custom), customLabel))
	for {
		reply, err := w.Ask.Ask(channelPrompt)
		if err != nil {
			return "", err
		}
		if reply == "" && opts[0].preferred {
			return opts[0].value, nil
		}
		i, err := strconv.Atoi(reply)
		switch {
		case err != nil:
		case i >= 1 && i <= len(opts):
			return opts[i-1].value, nil
		case i == custom:
			return w.askCustomChannel()
		}
	}
}

func (w *Wizard) askCustomChannel() (string, error) {
	for {
		reply, err := w.askNonEmpty(customPrompt)
		if err != nil {
			return "", err
		}
		channel, err := config.ParseChannel(reply)
		if err == nil {
			return channel, nil
		}
		ui.Warn(w.Out, err.Error())
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
	}
	channelAt := len(rows) + 1
	desktopAt := channelAt + 1
	for {
		ui.Text(w.Out, "")
		for i, r := range rows {
			ui.Text(w.Out, fmt.Sprintf(reviewLine, i+1, r.label, *r.value))
		}
		ui.Text(w.Out, fmt.Sprintf(reviewLine, channelAt, channelRow, v.Channel))
		if d != nil {
			ui.Text(w.Out, fmt.Sprintf(reviewLine, desktopAt, desktopRow, d.name))
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
		case i == channelAt:
			if v.Channel, err = w.askChannel(); err != nil {
				return err
			}
		case d != nil && i == desktopAt:
			chosen, err := w.askDesktop()
			if err != nil {
				return err
			}
			*d = chosen
		}
	}
}
