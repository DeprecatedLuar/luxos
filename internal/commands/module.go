package commands

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/DeprecatedLuar/luxos/internal/commands/help"
	"github.com/DeprecatedLuar/luxos/internal/commands/shared"
	"github.com/DeprecatedLuar/luxos/internal/config"
	"github.com/DeprecatedLuar/luxos/internal/flake"
	"github.com/DeprecatedLuar/luxos/internal/modules"
	"github.com/DeprecatedLuar/luxos/internal/nix"
	"github.com/DeprecatedLuar/luxos/internal/paths"
	"github.com/DeprecatedLuar/luxos/internal/shell"
	"github.com/DeprecatedLuar/luxos/internal/staging"
	"github.com/DeprecatedLuar/luxos/internal/templates"
	"github.com/DeprecatedLuar/luxos/internal/ui"
)

// entrypointName is the file under Paths.Modules that mirrors the active
// host's selection.
const entrypointName = "default.nix"

// localLinkName is the reserved entry at Paths.Modules' own root: the link
// to the active host's local modules directory. A missing link means
// no local units for modules.Walk's purposes.
const localLinkName = "local"

// accountFileName is where a user module's account definition lives;
// `edit` prefers it over entrypointName for a directory unit that has one.
const accountFileName = "account.nix"

const editScratchPattern = "luxos-edit-*.nix"

// moduleSimpleTemplate is the minimal scaffold `module add` writes for a
// non-user module.
const moduleSimpleTemplate = "{ ... }:\n\n{\n}\n"

// moduleUserPlaceholder is the literal replaced with the real account name
// in the embedded user template's account.nix.
const moduleUserPlaceholder = "users.users.user ="

// moduleFrameworkCategory is the reserved top-level category `add`/`remove`/
// `rename` refuse to touch: modules/system is owned by internal/framework.
const moduleFrameworkCategory = "system"

const (
	markerEnabledOnly = "⊕" // enabled, not running
	markerEnabledBoth = "◉" // enabled and running
	markerRunningOnly = "⊘" // running, not enabled
	markerPulled      = "◍" // pulled in by luxos.modules transitively, not directly selected
	markerNeither     = "○" // neither
)

// State words of the plain output, shared by module and flake listings.
const (
	stateWordStaged   = "staged"
	stateWordModified = "modified"
	stateWordActive   = "active"
	stateWordLeftover = "leftover"
	stateWordPulled   = "pulled"
	stateWordOff      = "off"
	stateWordRemoved  = "removed"
)

const flakeMark = "❄"

// bundleModulesDir is the folder of a bundle's submodules.
const bundleModulesDir = "modules"

// nameSep separates a submodule's name from its bundle's.
const nameSep = "/"

const plainColumnSep = "\t"
const plainInputsSep = " "

func Module(args []string) error {
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" {
		return help.Run([]string{"help", "module"})
	}

	p, err := paths.Resolve()
	if err != nil {
		return err
	}

	var verb string
	var rest []string
	if len(args) > 0 {
		verb, rest = args[0], args[1:]
	}

	switch verb {
	case "list", "ls":
		return moduleList(p, rest)
	case "add", "a":
		return moduleAdd(p, rest)
	case "edit", "e":
		return moduleEdit(p, rest)
	case "enable", "1":
		return moduleEnable(p, rest)
	case "disable", "0":
		return moduleDisable(p, rest)
	case "remove", "rm":
		return moduleRemove(p, rest)
	case "rename", "rn":
		return moduleRename(p, rest)
	default:
		h, err := loadActive(p)
		if err != nil {
			return err
		}
		if m, found := h.Find(verb); found && modules.IsBundle(m.Abs) {
			return moduleList(p, args)
		}
		return fmt.Errorf("unknown module command: %s (list|add|edit|enable|1|disable|0|remove|rename, or a bundle name)", verb)
	}
}

//──[state markers]───────────────────────────────────────────────────────

// moduleMarkerRank returns the sort rank for a marker: ⊕, ◉, ⊘, ◍, ○.
// Pulled sorts after the three enabled states and before plain off.
func moduleMarkerRank(marker string) int {
	switch marker {
	case markerEnabledOnly:
		return 0
	case markerEnabledBoth:
		return 1
	case markerRunningOnly:
		return 2
	case markerPulled:
		return 3
	default:
		return 4
	}
}

func markerWord(marker string) string {
	switch marker {
	case markerEnabledOnly:
		return stateWordStaged
	case markerEnabledBoth:
		return stateWordActive
	case markerRunningOnly:
		return stateWordLeftover
	case markerPulled:
		return stateWordPulled
	default:
		return stateWordOff
	}
}

type moduleRow struct {
	category []string
	name     string
	unit     string // qualified unit name; name is its last segment
	marker   string
	rank     int
	pulledBy []string
	children []moduleRow
	note     string
	shadow   bool
	inputs   []string
	status   string
	modified bool // files changed since the running build
	removed  bool // imported by the running generation, no longer a unit in the config

	bundle     bool // has submodules; shown collapsed with its enabled count
	subEnabled int  // direct submodules enabled
	subTotal   int  // direct submodules
}

//──[list]─────────────────────────────────────────────────────────────────

// markerOf is the marker a module state is drawn with.
func markerOf(state modules.State) string {
	switch state {
	case modules.Active:
		return markerEnabledBoth
	case modules.Staged, modules.Modified:
		return markerEnabledOnly
	case modules.Leftover, modules.Removed:
		return markerRunningOnly
	case modules.Pulled:
		return markerPulled
	default:
		return markerNeither
	}
}

// moduleRows builds one moduleRow per status whose path lies under
// categoryPath (every status when categoryPath is ""). category is stored as
// path segments relative to categoryPath, so the filtered subtree renders
// rooted at itself rather than repeating the filter as a nested category.
func moduleRows(sts []modules.Status, categoryPath string) []moduleRow {
	subTotal := map[string]int{}
	subEnabled := map[string]int{}
	for _, st := range sts {
		if parent := modules.Parent(st.Module.Name); parent != "" {
			subTotal[parent]++
			switch st.State {
			case modules.Active, modules.Staged, modules.Modified:
				subEnabled[parent]++
			}
		}
	}

	var rows []moduleRow
	for _, st := range sts {
		m := st.Module
		// A shadow sits where the unit it hides sits, not under local/.
		shown := m.Path
		if m.Shadows != "" {
			shown = m.Shadows
		}
		segs, ok := categorySegments(shown, categoryPath)
		if !ok {
			continue
		}
		marker := markerOf(st.State)
		rows = append(rows, moduleRow{
			category:   segs,
			name:       baseName(m.Name),
			unit:       m.Name,
			marker:     marker,
			rank:       moduleMarkerRank(marker),
			pulledBy:   st.PulledBy,
			shadow:     m.Shadows != "",
			modified:   st.State == modules.Modified,
			removed:    st.State == modules.Removed,
			inputs:     st.Inputs,
			bundle:     subTotal[m.Name] > 0,
			subEnabled: subEnabled[m.Name],
			subTotal:   subTotal[m.Name],
		})
	}
	return rows
}

// categorySegments returns the category segments of a unit path relative to
// categoryPath, and false when the path lies outside categoryPath ("" keeps
// every path).
func categorySegments(shown, categoryPath string) ([]string, bool) {
	var stripSegs []string
	if categoryPath != "" {
		if !strings.HasPrefix(shown, categoryPath+"/") {
			return nil, false
		}
		stripSegs = strings.Split(categoryPath, "/")
	}
	var segs []string
	if cat := filepath.Dir(shown); cat != "." {
		segs = strings.Split(cat, "/")[len(stripSegs):]
	}
	return segs, true
}

// baseName is the last segment of a qualified unit name.
func baseName(unit string) string {
	return unit[strings.LastIndex(unit, nameSep)+1:]
}

// collapseBundles keeps only the rows directly inside the bundle called
// target ("" for the top level): a bundle shows as one row, its submodules
// only when it is the target.
func collapseBundles(rows []moduleRow, target string) []moduleRow {
	var out []moduleRow
	for _, r := range rows {
		if modules.Parent(r.unit) == target {
			out = append(out, r)
		}
	}
	return out
}

func rowWord(r moduleRow) string {
	if r.removed {
		return stateWordRemoved
	}
	if r.modified {
		return stateWordModified
	}
	return markerWord(r.marker)
}

// colorOf is the tint a row is drawn in: blue when modified, else its marker's.
func colorOf(r moduleRow) ui.Color {
	if r.modified {
		return ui.ColorBlue
	}
	return markerColor(r.marker)
}

// markerColor is the tint of a state marker.
func markerColor(marker string) ui.Color {
	switch marker {
	case markerEnabledOnly:
		return ui.ColorTeal
	case markerEnabledBoth:
		return ui.ColorGreen
	case markerRunningOnly:
		return ui.ColorRed
	case markerPulled:
		return ui.ColorPurple
	default:
		return ui.ColorOff
	}
}

// noteColor is the tint of a row note: teal for an available update, off for unknown.
func noteColor(note string) ui.Color {
	if flake.Note(note) == flake.NoteBehind {
		return ui.ColorTeal
	}
	return ui.ColorOff
}

// uiRows maps rows, and their children, to what ui draws.
func uiRows(rows []moduleRow) []ui.Row {
	out := make([]ui.Row, len(rows))
	for i, r := range rows {
		mark := ""
		if len(r.inputs) > 0 {
			mark = flakeMark
		}
		count := ""
		if r.bundle {
			count = fmt.Sprintf("%d/%d", r.subEnabled, r.subTotal)
		}
		out[i] = ui.Row{
			Category:  r.category,
			Name:      r.name,
			Mark:      mark,
			Count:     count,
			Marker:    r.marker,
			Color:     colorOf(r),
			Rank:      r.rank,
			Underline: r.shadow,
			Strike:    r.removed,
			Note:      r.note,
			NoteColor: noteColor(r.note),
			Trailer:   r.pulledBy,
			Children:  uiRows(r.children),
		}
	}
	return out
}

func rowPath(r moduleRow) string {
	if len(r.category) == 0 {
		return r.name
	}
	return strings.Join(r.category, "/") + "/" + r.name
}

type moduleJSONRow struct {
	Path   string   `json:"path"`
	State  string   `json:"state"`
	Inputs []string `json:"inputs"`
}

func moduleRenderJSON(w *strings.Builder, rows []moduleRow) error {
	out := make([]moduleJSONRow, 0, len(rows))
	for _, r := range rows {
		inputs := r.inputs
		if inputs == nil {
			inputs = []string{}
		}
		out = append(out, moduleJSONRow{Path: rowPath(r), State: rowWord(r), Inputs: inputs})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return ui.JSON(w, out)
}

func moduleRenderPlain(w *strings.Builder, rows []moduleRow) {
	sorted := append([]moduleRow(nil), rows...)
	sort.Slice(sorted, func(i, j int) bool { return rowPath(sorted[i]) < rowPath(sorted[j]) })
	for _, r := range sorted {
		fmt.Fprintln(w, strings.Join([]string{rowPath(r), rowWord(r), strings.Join(r.inputs, plainInputsSep)}, plainColumnSep))
	}
}

func moduleList(p paths.Paths, args []string) error {
	opts, positional, err := shared.Parse(moduleListFlagSpec, args)
	if err != nil {
		return err
	}
	flat, raw, asJSON := opts["flat"] != "", opts["raw"] != "", opts["json"] != ""

	switch {
	case asJSON && raw:
		return ui.ErrJSONConflict("--raw")
	case asJSON && flat:
		return ui.ErrJSONConflict("--flat")
	}

	var categoryPath string
	if len(positional) > 0 {
		categoryPath = positional[0]
	}
	categoryPath = strings.TrimSuffix(categoryPath, "/")

	h, err := loadActive(p)
	if err != nil {
		return err
	}

	// A bundle target lists its submodules, with categories relative to its modules/.
	var bundleView string
	if u, ok := h.Find(categoryPath); ok && categoryPath != "" && modules.IsBundle(u.Abs) {
		bundleView = u.Name
		shown := u.Path
		if u.Shadows != "" {
			shown = u.Shadows
		}
		categoryPath = shown + "/" + bundleModulesDir
	} else if categoryPath != "" {
		catDir := filepath.Join(p.Modules, categoryPath)
		info, err := os.Stat(catDir)
		if err != nil || !info.IsDir() {
			return fmt.Errorf("unknown category '%s'", categoryPath)
		}
		if fi, err := os.Stat(filepath.Join(catDir, entrypointName)); err == nil && !fi.IsDir() {
			return fmt.Errorf("'%s' is a module, not a category", categoryPath)
		}
	}

	tty := ui.IsTerminal(os.Stdout)

	if _, err := os.Stat(p.RunningModules); err != nil {
		fmt.Fprintf(os.Stderr, "Note: no running generation found at %s — state unknown until the next switch; every enabled module shows as staged.\n", p.RunningModules)
	}

	var runningPaths []string
	if _, err := os.Stat(p.RunningModules); err == nil {
		runningPaths, err = modules.ReadSelection(p.RunningModules)
		if err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}

	baseline, err := staging.Baseline(p.Staging, p.PreviousStage)
	if err != nil {
		return err
	}
	sts, err := modules.StatusOf(h, runningPaths, baseline)
	if err != nil {
		return err
	}
	rows := moduleRows(sts, categoryPath)

	rootLabel := "modules/"
	switch {
	case bundleView != "":
		rootLabel = bundleView + "/"
	case categoryPath != "":
		rootLabel = categoryPath + "/"
	}

	pal := ui.PaletteFor(os.Stdout)

	var out strings.Builder
	switch {
	case asJSON:
		if err := moduleRenderJSON(&out, rows); err != nil {
			return err
		}
	case raw:
		moduleRenderPlain(&out, rows)
	case flat:
		ui.Flat(&out, uiRows(collapseBundles(rows, bundleView)), pal)
	case tty:
		ui.Tree(&out, uiRows(collapseBundles(rows, bundleView)), rootLabel, pal)
	default:
		moduleRenderPlain(&out, rows)
	}
	fmt.Print(out.String())
	return nil
}

//──[editing]──────────────────────────────────────────────────────────────

// editScratch writes initial to a scratch .nix file, opens it in $EDITOR,
// waits for the editor to exit, and returns the result once it parses.
func editScratch(initial []byte) ([]byte, error) {
	editor := os.Getenv("EDITOR")
	if editor == "" {
		return nil, fmt.Errorf("$EDITOR is not set")
	}

	var edited []byte
	err := nix.WithTemp(editScratchPattern, initial, func(path string) error {
		if err := shell.Run(shell.Cmd{Bin: editor, Args: []string{path}}); err != nil {
			return fmt.Errorf("$EDITOR exited with error: %w", err)
		}
		var err error
		if edited, err = os.ReadFile(path); err != nil {
			return err
		}
		if _, err := nix.Parse(path); err != nil {
			return fmt.Errorf("edited file would fail to parse: %w", err)
		}
		return nil
	})
	return edited, err
}

// editNixFileInPlace opens path's current content in $EDITOR via editScratch,
// then writes back in place (never rename) so ownership and mode survive
// running as root. A no-op edit leaves path untouched.
func editNixFileInPlace(path string) error {
	original, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	edited, err := editScratch(original)
	if err != nil {
		return err
	}
	if bytes.Equal(edited, original) {
		return nil
	}

	return nix.Write(path, edited)
}

//──[add]──────────────────────────────────────────────────────────────────

// moduleAddUser scaffolds modules/<target> from the embedded user template,
// filling in the literal account name. account.nix is opened in $EDITOR as
// a scratch file before anything is written.
func moduleAddUser(modulesRoot, target string) error {
	name := filepath.Base(target)
	dest := filepath.Join(modulesRoot, target)

	account, err := templates.File("starters/user/account.nix")
	if err != nil {
		return err
	}
	if !strings.Contains(string(account), moduleUserPlaceholder) {
		return fmt.Errorf("embedded templates/user/account.nix has no '%s' to fill in", moduleUserPlaceholder)
	}
	filled := strings.ReplaceAll(string(account), moduleUserPlaceholder, "users.users."+name+" =")

	edited, err := editScratch([]byte(filled))
	if err != nil {
		return err
	}

	if err := os.MkdirAll(dest, 0755); err != nil {
		return err
	}

	defaultNix, err := templates.File("starters/user/default.nix")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dest, "default.nix"), defaultNix, 0644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dest, accountFileName), edited, 0644); err != nil {
		return err
	}

	fmt.Printf("Created modules/%s/{default.nix,account.nix}\n", target)
	return nil
}

func moduleAdd(p paths.Paths, args []string) error {
	opts, rest, err := shared.Parse("enable:bool", args)
	if err != nil {
		return err
	}

	var target string
	if len(rest) > 0 {
		target = rest[0]
	}
	if target == "" || strings.HasSuffix(target, "/") || strings.HasPrefix(target, "/") {
		return fmt.Errorf("usage: luxos module add <category/name> [--enable]")
	}

	name := filepath.Base(target)

	h, err := loadActive(p)
	if err != nil {
		return err
	}
	shadowedPath := ""
	if existing, found := h.Find(name); found {
		if !shadowsShared(target, existing.Path) {
			return fmt.Errorf("module name '%s' already exists at modules/%s", name, existing.Path)
		}
		shadowedPath = existing.Path
	}

	if _, err := os.Stat(filepath.Join(p.Modules, target)); err == nil {
		return fmt.Errorf("'modules/%s' already exists", target)
	}
	if _, err := os.Stat(filepath.Join(p.Modules, target+".nix")); err == nil {
		return fmt.Errorf("'modules/%s' already exists", target)
	}

	if err := os.MkdirAll(filepath.Join(p.Modules, filepath.Dir(target)), 0755); err != nil {
		return err
	}

	if strings.HasPrefix(target, "users/") || target == "users" {
		if err := moduleAddUser(p.Modules, target); err != nil {
			return err
		}
	} else {
		edited, err := editScratch([]byte(moduleSimpleTemplate))
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(p.Modules, target+".nix"), edited, 0644); err != nil {
			return err
		}
		fmt.Printf("Created modules/%s.nix\n", target)
	}
	if shadowedPath != "" {
		fmt.Printf("Note: %s shadows modules/%s\n", target, shadowedPath)
	}

	if opts["enable"] != "" {
		return toggleModule(p, name, true)
	}
	return nil
}

//──[edit]─────────────────────────────────────────────────────────────────

func moduleEdit(p paths.Paths, args []string) error {
	var name string
	if len(args) > 0 {
		name = args[0]
	}
	if name == "" {
		return fmt.Errorf("usage: luxos module edit <name>")
	}

	h, err := loadActive(p)
	if err != nil {
		return err
	}
	file, err := moduleEditTarget(h, name)
	if err != nil {
		return err
	}
	return editNixFileInPlace(file)
}

// moduleEditTarget resolves the file `edit` should open for name: the file
// itself for a single-file unit; account.nix for a directory unit that has
// one; entrypointName otherwise.
func moduleEditTarget(h *modules.Host, name string) (string, error) {
	m, found := h.Find(name)
	if !found {
		return "", fmt.Errorf("unknown module '%s'", name)
	}

	target := m.Abs
	fi, err := os.Stat(target)
	if err != nil {
		return "", err
	}
	if !fi.IsDir() {
		return target, nil
	}

	if account := filepath.Join(target, accountFileName); fileExists(account) {
		return account, nil
	}
	return filepath.Join(target, entrypointName), nil
}

func fileExists(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && !fi.IsDir()
}

//──[enable/disable]─────────────────────────────────────────────────────────

// loadActive loads the active host's modules and selection.
func loadActive(p paths.Paths) (*modules.Host, error) {
	host, err := config.ActiveHost(p.Modules)
	if err != nil {
		return nil, err
	}
	return modules.Load(p.Modules, filepath.Join(p.Machines, host))
}

// toggleModule is the shared body for enable/disable: acts only on the
// active host's selection.
func toggleModule(p paths.Paths, name string, enable bool) error {
	h, err := loadActive(p)
	if err != nil {
		return err
	}
	m, found := h.Find(name)
	if !found {
		return fmt.Errorf("unknown module '%s'", name)
	}

	if enable {
		if err := enableBundles(h, name); err != nil {
			return err
		}
		changed, err := modules.Enable(h, name)
		if err != nil {
			return err
		}
		if !changed {
			fmt.Printf("'%s' is already enabled\n", name)
		}
		return nil
	}

	subs, err := disableSubmodules(h, m)
	if err != nil {
		return err
	}
	changed, err := modules.Disable(h, name)
	if err != nil {
		return err
	}
	if !changed && !subs {
		fmt.Printf("'%s' is already disabled\n", name)
	}
	return nil
}

// enableBundles enables every not-yet-enabled bundle that name is inside,
// outermost first.
func enableBundles(h *modules.Host, name string) error {
	var chain []string
	for b := modules.Parent(name); b != ""; b = modules.Parent(b) {
		chain = append([]string{b}, chain...)
	}
	for _, bundle := range chain {
		changed, err := modules.Enable(h, bundle)
		if err != nil {
			return err
		}
		if changed {
			fmt.Printf("enabled '%s' (bundle of '%s')\n", bundle, name)
		}
	}
	return nil
}

// disableSubmodules removes every enabled line for a submodule of m when m is
// a bundle, reporting whether any was removed.
func disableSubmodules(h *modules.Host, m modules.Module) (bool, error) {
	if !modules.IsBundle(m.Abs) {
		return false, nil
	}
	removed := false
	for _, sel := range append([]string(nil), h.Selection...) {
		sub := modules.NameFromPath(sel)
		if !strings.HasPrefix(sub, m.Name+nameSep) {
			continue
		}
		if _, err := modules.Disable(h, sub); err != nil {
			return removed, err
		}
		fmt.Printf("disabled '%s' (submodule of '%s')\n", sub, m.Name)
		removed = true
	}
	return removed, nil
}

func moduleEnable(p paths.Paths, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("module enable requires at least one name")
	}
	for _, name := range args {
		if err := toggleModule(p, name, true); err != nil {
			return err
		}
	}
	return nil
}

// moduleListFlagSpec is the flag spec of the list verbs.
const moduleListFlagSpec = "flat:bool raw:bool json:bool"

// moduleYesSpec is the flag spec for verbs whose only flag is --yes.
const moduleYesSpec = "yes|y:bool"

// errHardwareDisableAborted is returned when disabling hardware is not confirmed.
var errHardwareDisableAborted = errors.New("aborted: nothing changed\n  confirm with: luxos module disable hardware-support --yes")

// disablesEnabledHardware reports whether any name in names is the
// hardware unit and currently enabled on the active host.
func disablesEnabledHardware(p paths.Paths, names []string) (bool, error) {
	h, err := loadActive(p)
	if err != nil {
		return false, err
	}
	for _, name := range names {
		m, found := h.Find(name)
		if !found || m.Path != config.HardwareUnitPath {
			continue
		}
		if _, enabled := h.Selected(name); enabled {
			return true, nil
		}
	}
	return false, nil
}

func moduleDisable(p paths.Paths, args []string) error {
	opts, names, err := shared.Parse(moduleYesSpec, args)
	if err != nil {
		return err
	}
	if len(names) == 0 {
		return fmt.Errorf("usage: luxos module disable <name>... [-y]")
	}

	hardware, err := disablesEnabledHardware(p, names)
	if err != nil {
		return err
	}
	if hardware {
		if opts["yes"] == "" {
			ok, err := shared.ConfirmHardwareOff()
			if err != nil {
				return err
			}
			if !ok {
				return errHardwareDisableAborted
			}
		}
		escalated := append(append([]string{"module", "disable"}, names...), "--yes")
		if err := shared.EnsureRoot(escalated); err != nil {
			return err
		}
	}

	for _, name := range names {
		if err := toggleModule(p, name, false); err != nil {
			return err
		}
	}
	return nil
}

//──[scoping]──────────────────────────────────────────────────────────────

// shadowsShared reports whether creating or renaming to a unit at newPath
// (a "local/..." path or add target) that collides with the unit at
// existingPath is a shadow: the new unit is local and the existing one is
// shared.
func shadowsShared(newPath, existingPath string) bool {
	return strings.HasPrefix(newPath, modules.LocalPrefix) && !strings.HasPrefix(existingPath, modules.LocalPrefix)
}

// printShadowedHosts tells which hosts keep meaning their own local module.
func printShadowedHosts(name string, hosts []string) {
	if len(hosts) > 0 {
		fmt.Printf("'%s' is shadowed on %s; left untouched there\n", name, strings.Join(hosts, ", "))
	}
}

// declined handles a "no": a plain abort, except when shadowed hosts made the
// confirmation mandatory, where it fails naming the flag that answers it.
func declined(skipHosts []string) error {
	if len(skipHosts) > 0 {
		return fmt.Errorf("not confirmed; run again with --yes to proceed")
	}
	fmt.Println("Aborted.")
	return nil
}

//──[remove]───────────────────────────────────────────────────────────────

// printReferencedBy prints label followed by an indented list of files, or
// prints emptyMsg (when non-empty) if files is empty.
func printReferencedBy(label string, files []string, emptyMsg string) {
	if len(files) == 0 {
		if emptyMsg != "" {
			fmt.Println(emptyMsg)
		}
		return
	}
	fmt.Println(label)
	for _, f := range files {
		if f != "" {
			fmt.Printf("  %s\n", f)
		}
	}
}

// errHardwareUnit refuses remove and rename of the computer's hardware folder.
var errHardwareUnit = errors.New("'hardware-support' is this computer's hardware folder, managed by luxos; it cannot be removed or renamed\n  disable it with: luxos module disable hardware-support")

func moduleRemove(p paths.Paths, args []string) error {
	opts, rest, err := shared.Parse(moduleYesSpec, args)
	if err != nil {
		return err
	}

	var name string
	if len(rest) > 0 {
		name = rest[0]
	}
	if name == "" {
		return fmt.Errorf("usage: luxos module remove <name> [-y]")
	}

	h, err := loadActive(p)
	if err != nil {
		return err
	}
	m, found := h.Find(name)
	if !found {
		return fmt.Errorf("unknown module '%s'", name)
	}
	path := m.Path
	if isFrameworkPath(path) {
		return fmt.Errorf("'%s' is a framework module (modules/%s) — not owned by this config", name, path)
	}
	if path == config.HardwareUnitPath {
		return errHardwareUnit
	}

	scope, err := modules.ScopeOf(p.Machines, p.Modules, h.Name, m)
	if err != nil {
		return err
	}
	host, localDirs, skipHosts := scope.Host, scope.LocalDirs, scope.SkipHosts

	importers, err := modules.Importers(p.Machines, name, host)
	if err != nil {
		return err
	}
	printReferencedBy(fmt.Sprintf("'%s' is imported by:", name), importers, fmt.Sprintf("'%s' is not imported by any host.", name))

	dependents, err := modules.Dependents(p.Modules, localDirs, name)
	if err != nil {
		return err
	}
	printReferencedBy(fmt.Sprintf("'%s' is referenced by:", name), dependents, "")

	printShadowedHosts(name, skipHosts)

	if opts["yes"] == "" {
		ok, err := ui.Confirm(fmt.Sprintf("Remove modules/%s and the import line(s)/reference(s) above? [y/N] ", path), false)
		if err != nil {
			return err
		}
		if !ok {
			return declined(skipHosts)
		}
	}

	// modules.RetargetRefs first: it validates every dependent's call shape before
	// writing anything and refuses the whole operation on a bad shape, so a
	// refusal here leaves the tree completely untouched.
	if _, err := modules.RetargetRefs(p.Modules, localDirs, name, ""); err != nil {
		return err
	}
	if _, err := modules.RetargetSelections(p.Machines, name, "", host, skipHosts); err != nil {
		return err
	}
	if modules.IsBundle(filepath.Join(p.Modules, path)) {
		if _, err := modules.RetargetPrefix(p.Machines, path, "", host, skipHosts); err != nil {
			return err
		}
	}
	if err := os.RemoveAll(filepath.Join(p.Modules, path)); err != nil {
		return err
	}

	fmt.Printf("Removed modules/%s\n", path)
	return nil
}

//──[rename]───────────────────────────────────────────────────────────────

// joinCategory joins a category ("" for root) and a basename into a
// modules-relative path.
func joinCategory(category, base string) string {
	if category == "" {
		return base
	}
	return category + "/" + base
}

func moduleRename(p paths.Paths, args []string) error {
	opts, rest, err := shared.Parse(moduleYesSpec, args)
	if err != nil {
		return err
	}

	var oldName, newName string
	if len(rest) > 0 {
		oldName = rest[0]
	}
	if len(rest) > 1 {
		newName = rest[1]
	}
	if oldName == "" || newName == "" {
		return fmt.Errorf("usage: luxos module rename <old> <new> [-y]")
	}

	h, err := loadActive(p)
	if err != nil {
		return err
	}
	old, found := h.Find(oldName)
	if !found {
		return fmt.Errorf("unknown module '%s'", oldName)
	}
	oldPath := old.Path
	if isFrameworkPath(oldPath) {
		return fmt.Errorf("'%s' is a framework module (modules/%s) — not owned by this config", oldName, oldPath)
	}
	if oldPath == config.HardwareUnitPath {
		return errHardwareUnit
	}

	newQualified := newName
	if parent := modules.Parent(oldName); parent != "" {
		if strings.Contains(newName, nameSep) {
			return fmt.Errorf("rename keeps a submodule in its bundle: give only the new name")
		}
		newQualified = parent + nameSep + newName
	}

	shadowedPath := ""
	if existing, found := h.Find(newQualified); found {
		if !shadowsShared(oldPath, existing.Path) {
			return fmt.Errorf("module name '%s' already exists at modules/%s", newQualified, existing.Path)
		}
		shadowedPath = existing.Path
	}

	scope, err := modules.ScopeOf(p.Machines, p.Modules, h.Name, old)
	if err != nil {
		return err
	}
	host, localDirs, skipHosts := scope.Host, scope.LocalDirs, scope.SkipHosts

	dependents, err := modules.Dependents(p.Modules, localDirs, oldName)
	if err != nil {
		return err
	}
	if len(dependents) > 0 || len(skipHosts) > 0 {
		printReferencedBy(fmt.Sprintf("'%s' is referenced by:", oldName), dependents, "")
		printShadowedHosts(oldName, skipHosts)

		if opts["yes"] == "" {
			ok, err := ui.Confirm(fmt.Sprintf("Rename '%s' to '%s' and update the reference(s) above? [y/N] ", oldName, newName), false)
			if err != nil {
				return err
			}
			if !ok {
				return declined(skipHosts)
			}
		}
	}

	oldFull := filepath.Join(p.Modules, oldPath)
	category := filepath.Dir(oldPath)
	if category == "." {
		category = ""
	}

	var newPath string
	if fi, err := os.Stat(oldFull); err == nil && fi.IsDir() {
		newPath = joinCategory(category, newName)
	} else {
		newPath = joinCategory(category, newName+".nix")
	}

	// modules.RetargetRefs first (validates every dependent's call shape before
	// writing anything, refusing the whole operation on a bad shape): a
	// refusal here leaves the unit unmoved and every host entrypoint
	// untouched.
	bundle := modules.IsBundle(oldFull)
	if _, err := modules.RetargetRefs(p.Modules, localDirs, oldName, newQualified); err != nil {
		return err
	}
	if err := os.Rename(oldFull, filepath.Join(p.Modules, newPath)); err != nil {
		return err
	}
	if _, err := modules.RetargetSelections(p.Machines, oldName, newPath, host, skipHosts); err != nil {
		return err
	}
	if bundle {
		if _, err := modules.RetargetPrefix(p.Machines, oldPath, newPath, host, skipHosts); err != nil {
			return err
		}
	}

	if modules.Parent(oldName) == "" && (category == "users" || strings.HasPrefix(category, "users/")) {
		fmt.Printf("Note: the account name in modules/%s/account.nix is still '%s' — rename it there by hand if the actual system user should change too.\n", newPath, oldName)
	}

	fmt.Printf("Renamed modules/%s -> modules/%s\n", oldPath, newPath)
	if shadowedPath != "" {
		fmt.Printf("Note: modules/%s now shadows modules/%s\n", newPath, shadowedPath)
	}
	return nil
}

//──[shared helpers]──────────────────────────────────────────────────────

// isFrameworkPath reports whether a modules-relative path lies under the
// reserved framework category (modules/system), which add/remove/rename
// refuse to touch.
func isFrameworkPath(path string) bool {
	return path == moduleFrameworkCategory || strings.HasPrefix(path, moduleFrameworkCategory+"/")
}
