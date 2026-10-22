package commands

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
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
	"github.com/DeprecatedLuar/luxos/internal/ui"
)

// entrypointName is the file that turns a folder into a unit.
const entrypointName = config.DefaultFile

// accountFileName is where a user module's account definition lives;
// `edit` prefers it over entrypointName for a directory unit that has one.
const accountFileName = config.AccountFile

const editScratchPattern = "luxos-edit-*.nix"

// moduleSimpleTemplate is the minimal scaffold `module add` writes for a
// non-user module.
const moduleSimpleTemplate = "{ ... }:\n\n{\n}\n"

// moduleListFlagSpec is the flag spec of the list verbs.
const moduleListFlagSpec = "flat:bool raw:bool json:bool"

// moduleYesSpec is the flag spec for verbs whose only flag is --yes.
const moduleYesSpec = "yes|y:bool"

const (
	markerEnabledOnly = "⊕" // enabled, not running
	markerEnabledBoth = "◉" // enabled and running
	markerRunningOnly = "⊘" // running, not enabled
	markerPulled      = "◍" // pulled in by luxos.modules transitively, not directly selected
	markerNeither     = "○" // neither
	markerBundle      = "◈" // a bundle, in its state's color
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

// Marks drawn after a module's name.
const (
	flakeMark  = "❄" // declares flake inputs
	brokenMark = "!" // a file fails to evaluate

	settingsMark = "⚙" // declares options; yellow when its options or settings break a rule
)

// Words of the plain ok column.
const (
	plainOK     = "ok"
	plainBroken = "broken"
)

// bundleModulesDir is the folder of a bundle's submodules.
const bundleModulesDir = "modules"

// nameSep separates a submodule's name from its bundle's.
const moduleCommand = "module"

var (
	errMissingName  = errors.New("missing module name")
	errMissingNames = errors.New("missing module names")
	errRenameArgs   = errors.New("want <old> <new>")
)

const nameSep = "/"

const plainInputsSep = " "

func Module(args []string) error {
	if len(args) == 0 {
		return help.Misuse(os.Stderr, moduleCommand, nil)
	}
	if isHelpFlag(args[0]) {
		return help.Command(os.Stdout, moduleCommand)
	}

	p, err := paths.Resolve()
	if err != nil {
		return err
	}

	verb, rest := args[0], args[1:]
	switch verb {
	case "list", "ls":
		return moduleList(p, rest)
	case "add", "a":
		return moduleAdd(p, rest)
	case "edit", "e":
		return moduleEdit(p, rest)
	case "configure", "config":
		return moduleConfigure(p, rest)
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
		return fmt.Errorf("unknown module command: %s (list|add|edit|configure|enable|1|disable|0|remove|rename, or a bundle name)", verb)
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
	pulledBy []string
	children []moduleRow
	note     string
	shadow   bool
	inputs   []string
	broken   bool
	settings modules.SettingsState
	status   string
	modified bool // files changed since the running build
	removed  bool // imported by the running generation, no longer a unit in the config

	bundle     bool // has submodules; shown collapsed with its enabled count
	subEnabled int  // direct submodules enabled
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
		segs, ok := categorySegments(m.StagedPath(), categoryPath)
		if !ok {
			continue
		}
		marker := markerOf(st.State)
		rows = append(rows, moduleRow{
			category:   segs,
			name:       baseName(m.Name),
			unit:       m.Name,
			marker:     marker,
			pulledBy:   st.PulledBy,
			shadow:     m.Shadows != "",
			modified:   st.State == modules.Modified,
			removed:    st.State == modules.Removed,
			inputs:     st.Inputs,
			broken:     st.Broken,
			settings:   st.Settings,
			bundle:     subTotal[m.Name] > 0,
			subEnabled: subEnabled[m.Name],
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

// stateColor is the tint a module state is drawn in, as colorOf draws its row.
func stateColor(state modules.State) ui.Color {
	if state == modules.Modified {
		return ui.ColorBlue
	}
	return markerColor(markerOf(state))
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

// inBuild reports whether a marker's module is part of the build: selected or pulled in.
func inBuild(marker string) bool {
	switch marker {
	case markerEnabledBoth, markerEnabledOnly, markerPulled:
		return true
	}
	return false
}

// marksOf is the marks drawn after a row's name, in the line color outside the build.
func marksOf(r moduleRow) []ui.Mark {
	var marks []ui.Mark
	if len(r.inputs) > 0 {
		marks = append(marks, ui.Mark{Glyph: flakeMark, Color: ui.ColorNix})
	}
	if r.broken {
		marks = append(marks, ui.Mark{Glyph: brokenMark, Color: ui.ColorYellow})
	}
	switch r.settings {
	case modules.SettingsOK:
		marks = append(marks, ui.Mark{Glyph: settingsMark, Color: ui.ColorOff})
	case modules.SettingsBroken:
		marks = append(marks, ui.Mark{Glyph: settingsMark, Color: ui.ColorYellow})
	}
	if !inBuild(r.marker) {
		for i := range marks {
			marks[i].Color = ui.ColorLine
		}
	}
	return marks
}

// uiRows maps rows, and their children, to what ui draws.
func uiRows(rows []moduleRow) []ui.Row {
	out := make([]ui.Row, len(rows))
	for i, r := range rows {
		marker, count := r.marker, ""
		if r.bundle {
			marker, count = markerBundle, fmt.Sprint(r.subEnabled)
		}
		out[i] = ui.Row{
			Category:  r.category,
			Name:      r.name,
			Marks:     marksOf(r),
			Count:     count,
			Marker:    marker,
			Color:     colorOf(r),
			Rank:      moduleMarkerRank(r.marker),
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

func okWord(r moduleRow) string {
	if r.broken {
		return plainBroken
	}
	return plainOK
}

// settingsWord is the plain settings column: none, ok or broken.
func settingsWord(r moduleRow) string {
	if r.settings == "" {
		return string(modules.SettingsNone)
	}
	return string(r.settings)
}

type moduleJSONRow struct {
	Path     string   `json:"path"`
	State    string   `json:"state"`
	Inputs   []string `json:"inputs"`
	OK       bool     `json:"ok"`
	Settings string   `json:"settings"`
}

func moduleRenderJSON(w *strings.Builder, rows []moduleRow) error {
	out := make([]moduleJSONRow, 0, len(rows))
	for _, r := range rows {
		inputs := r.inputs
		if inputs == nil {
			inputs = []string{}
		}
		out = append(out, moduleJSONRow{Path: rowPath(r), State: rowWord(r), Inputs: inputs, OK: !r.broken, Settings: settingsWord(r)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return ui.JSON(w, out)
}

func moduleRenderPlain(w *strings.Builder, rows []moduleRow) {
	sorted := append([]moduleRow(nil), rows...)
	sort.Slice(sorted, func(i, j int) bool { return rowPath(sorted[i]) < rowPath(sorted[j]) })
	lines := make([][]string, 0, len(sorted))
	for _, r := range sorted {
		lines = append(lines, []string{rowPath(r), rowWord(r), strings.Join(r.inputs, plainInputsSep), okWord(r), settingsWord(r)})
	}
	ui.Plain(w, lines)
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
		categoryPath = u.StagedPath() + "/" + bundleModulesDir
	} else if categoryPath != "" {
		catDir := filepath.Join(p.Modules, categoryPath)
		info, err := os.Stat(catDir)
		if err != nil || !info.IsDir() {
			return fmt.Errorf("unknown category '%s'", categoryPath)
		}
		if fileExists(filepath.Join(catDir, entrypointName)) {
			return fmt.Errorf("'%s' is a module, not a category", categoryPath)
		}
	}

	tty := ui.IsTerminal(os.Stdout)

	sts, found, err := moduleStatuses(p, h)
	if err != nil {
		return err
	}
	if !found {
		ui.Warn(os.Stderr, fmt.Sprintf("no running generation found at %s — state unknown until the next switch; every enabled module shows as staged.", p.RunningSelections))
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
	ui.Print(os.Stdout, out.String())
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

// moduleAddUser scaffolds modules/<target> as a user unit.
func moduleAddUser(modulesRoot, target string) error {
	name := filepath.Base(target)
	if err := config.WriteUser(filepath.Join(modulesRoot, target)); err != nil {
		return err
	}
	ui.Done(os.Stdout, fmt.Sprintf("Created modules/%s/{default.nix,account.nix,options.nix,modules/default.nix,modules/packages.nix}", target))
	ui.Text(os.Stdout, fmt.Sprintf("Set its password, then enable it: luxos user passwd %s && luxos user enable %s", name, name))
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
		return help.Misuse(os.Stderr, moduleCommand, errMissingName)
	}

	name := filepath.Base(target)

	h, err := loadActive(p)
	if err != nil {
		return err
	}
	shadowedPath := ""
	if existing, found := h.Find(name); found {
		if !modules.CanShadow(target, existing) {
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

	if target == modules.UserPath(name) {
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
		ui.Done(os.Stdout, fmt.Sprintf("Created modules/%s.nix", target))
	}
	if shadowedPath != "" {
		ui.Warn(os.Stdout, fmt.Sprintf("%s shadows modules/%s", target, shadowedPath))
	}

	if opts["enable"] != "" {
		h, err := loadActive(p)
		if err != nil {
			return err
		}
		return toggleModules(p, h, []string{name}, true, false)
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
		return help.Misuse(os.Stderr, moduleCommand, errMissingName)
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
	m, err := h.Lookup(name)
	if err != nil {
		return "", err
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

//──[configure]────────────────────────────────────────────────────────────

// moduleConfigure syncs the settings file in effect for a unit (this
// computer's with --machine) and opens it in $EDITOR. A broken unit's
// existing file is still opened, so it can be fixed.
func moduleConfigure(p paths.Paths, args []string) error {
	opts, rest, err := shared.Parse("machine:bool", args)
	if err != nil {
		return err
	}
	if len(rest) != 1 {
		return help.Misuse(os.Stderr, moduleCommand, errMissingName)
	}
	name := rest[0]

	h, err := loadActive(p)
	if err != nil {
		return err
	}
	m, err := h.Lookup(name)
	if err != nil {
		return err
	}
	file, rep, err := modules.SyncUnitSettings(h, m, opts["machine"] != "")
	if err != nil {
		return err
	}
	out := ui.NewProgress(os.Stdout)
	printSettingsReport(out, rep)
	for _, u := range rep.Broken {
		for _, problem := range u.Problems {
			out.Warnf("%s", problem)
		}
	}
	if err := out.Err(); err != nil {
		return err
	}
	if len(rep.Broken) > 0 && !fileExists(file) {
		return fmt.Errorf("module '%s' breaks the settings rules; fix its %s first", name, modules.OptionsFile)
	}
	ui.Text(os.Stdout, fmt.Sprintf("Settings: %s", file))
	return editNixFileInPlace(file)
}

func fileExists(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && !fi.IsDir()
}

//──[enable/disable]─────────────────────────────────────────────────────────

// loadActive loads the active host with this computer's machine folder.
func loadActive(p paths.Paths) (*modules.Host, error) {
	host, err := shared.ActiveHost(p)
	if err != nil {
		return nil, err
	}
	return loadHost(p, filepath.Join(p.Hosts, host))
}

// loadHost loads the host at hostDir with this computer's machine folder.
func loadHost(p paths.Paths, hostDir string) (*modules.Host, error) {
	machineDir, err := config.LinkedMachineDir(p.Modules)
	if err != nil {
		return nil, err
	}
	return modules.Load(p.Modules, hostDir, machineDir)
}

// toggleModules enables or disables names on the selections of h, the loaded
// active host, then prints the state of every name it touched. toMachine
// sends a shared unit to the machine selection on enable.
func toggleModules(p paths.Paths, h *modules.Host, names []string, enable, toMachine bool) error {
	var touched []string
	for _, name := range names {
		var changed []string
		var err error
		if enable {
			m, err := h.Lookup(name)
			if err != nil {
				return err
			}
			if err := requirePassword(m); err != nil {
				return err
			}
			changed, err = modules.Enable(h, name, toMachine)
			if err != nil {
				return err
			}
		} else if changed, err = modules.Disable(h, name); err != nil {
			return err
		}
		touched = append(touched, changed...)
		if !slices.Contains(changed, name) {
			touched = append(touched, name)
		}
	}
	return printStates(p, touched)
}

// printStates prints each named module's state as `list` draws it, read from
// the active host as the command left it.
func printStates(p paths.Paths, names []string) error {
	h, err := loadActive(p)
	if err != nil {
		return err
	}
	sts, _, err := moduleStatuses(p, h)
	if err != nil {
		return err
	}
	byName := make(map[string]modules.Status, len(sts))
	for _, st := range sts {
		byName[st.Module.Name] = st
	}
	for _, name := range names {
		state := byName[name].State
		ui.Marked(os.Stdout, stateColor(state), markerOf(state), name)
	}
	return nil
}

// moduleStatuses is the state of every module of h against the running
// generation and the baseline tree; found is false without a running generation.
func moduleStatuses(p paths.Paths, h *modules.Host) ([]modules.Status, bool, error) {
	running, found, err := modules.ReadRunning(p.RunningSelections)
	if err != nil {
		return nil, false, err
	}
	baseline, err := staging.Baseline(p.Staging)
	if err != nil {
		return nil, false, err
	}
	sts, err := modules.StatusOf(h, running, baseline)
	return sts, found, err
}

func moduleEnable(p paths.Paths, args []string) error {
	opts, names, err := shared.Parse("machine:bool", args)
	if err != nil {
		return err
	}
	if len(names) == 0 {
		return fmt.Errorf("module enable requires at least one name")
	}
	h, err := loadActive(p)
	if err != nil {
		return err
	}
	if err := toggleModules(p, h, names, true, opts["machine"] != ""); err != nil {
		return err
	}
	if h, err = loadActive(p); err != nil {
		return err
	}
	rep, err := modules.SyncSettings(h)
	if err != nil {
		return err
	}
	printSettingsReport(ui.NewProgress(os.Stdout), rep)
	return nil
}

// errBootDisableAborted is returned when disabling boot is not confirmed.
var errBootDisableAborted = errors.New("aborted: nothing changed\n  confirm with: luxos module disable boot --yes")

// disablesBoot reports whether any name in names is the machine's boot unit
// and it is currently selected.
func disablesBoot(h *modules.Host, names []string) bool {
	if !h.BootSelected() {
		return false
	}
	for _, name := range names {
		if m, found := h.Find(name); found && m.Role() == modules.RoleBoot {
			return true
		}
	}
	return false
}

func moduleDisable(p paths.Paths, args []string) error {
	opts, names, err := shared.Parse(moduleYesSpec, args)
	if err != nil {
		return err
	}
	if len(names) == 0 {
		return help.Misuse(os.Stderr, moduleCommand, errMissingNames)
	}

	h, err := loadActive(p)
	if err != nil {
		return err
	}
	if opts["yes"] == "" && disablesBoot(h, names) {
		ok, err := shared.ConfirmBootOff()
		if err != nil {
			return err
		}
		if !ok {
			return errBootDisableAborted
		}
	}
	return toggleModules(p, h, names, false, false)
}

//──[scoping]──────────────────────────────────────────────────────────────

// printShadowed tells which folders keep meaning their own module.
func printShadowed(name string, folders []string) {
	if len(folders) > 0 {
		ui.Text(os.Stdout, fmt.Sprintf("'%s' is shadowed in %s; left untouched there", name, strings.Join(folders, ", ")))
	}
}

// declined handles a "no": a plain abort, except when shadowing folders made
// the confirmation mandatory, where it fails naming the flag that answers it.
func declined(skipped []string) error {
	if len(skipped) > 0 {
		return fmt.Errorf("not confirmed; run again with --yes to proceed")
	}
	ui.Text(os.Stdout, "Aborted.")
	return nil
}

//──[remove]───────────────────────────────────────────────────────────────

// printReferencedBy prints label followed by an indented list of files, or
// prints emptyMsg (when non-empty) if files is empty.
func printReferencedBy(label string, files []string, emptyMsg string) {
	if len(files) == 0 {
		if emptyMsg != "" {
			ui.Text(os.Stdout, emptyMsg)
		}
		return
	}
	ui.Text(os.Stdout, label)
	for _, f := range files {
		if f != "" {
			ui.Text(os.Stdout, "  "+f)
		}
	}
}

func moduleRemove(p paths.Paths, args []string) error {
	opts, rest, err := shared.Parse(moduleYesSpec, args)
	if err != nil {
		return err
	}
	if len(rest) == 0 || rest[0] == "" {
		return help.Misuse(os.Stderr, moduleCommand, errMissingName)
	}
	name := rest[0]

	h, err := loadActive(p)
	if err != nil {
		return err
	}
	plan, err := modules.PlanRemove(h, p.Hosts, p.Machines, name)
	if err != nil {
		return err
	}
	printReferencedBy(fmt.Sprintf("'%s' is imported by:", name), plan.Importers, fmt.Sprintf("'%s' is not imported by any host.", name))
	printReferencedBy(fmt.Sprintf("'%s' is referenced by:", name), plan.Dependents, "")
	printShadowed(name, plan.Skipped)

	if opts["yes"] == "" {
		ok, err := ui.Confirm(fmt.Sprintf("Remove modules/%s and the import line(s)/reference(s) above? [y/N] ", plan.Unit.Path), false)
		if err != nil {
			return err
		}
		if !ok {
			return declined(plan.Skipped)
		}
	}
	if _, err := plan.Apply(); err != nil {
		return err
	}
	ui.Done(os.Stdout, fmt.Sprintf("Removed modules/%s", plan.Unit.Path))
	return nil
}

//──[rename]───────────────────────────────────────────────────────────────

func moduleRename(p paths.Paths, args []string) error {
	opts, rest, err := shared.Parse(moduleYesSpec, args)
	if err != nil {
		return err
	}
	if len(rest) < 2 || rest[0] == "" || rest[1] == "" {
		return help.Misuse(os.Stderr, moduleCommand, errRenameArgs)
	}
	oldName, newName := rest[0], rest[1]

	h, err := loadActive(p)
	if err != nil {
		return err
	}
	plan, err := modules.PlanRename(h, p.Hosts, p.Machines, oldName, newName)
	if err != nil {
		return err
	}
	if plan.Unit.Scope == config.ScopeShared || len(plan.Dependents) > 0 {
		printReferencedBy(fmt.Sprintf("'%s' is imported by:", oldName), plan.Importers, "")
		printReferencedBy(fmt.Sprintf("'%s' is referenced by:", oldName), plan.Dependents, "")
		printShadowed(oldName, plan.Skipped)
		if opts["yes"] == "" {
			ok, err := ui.Confirm(fmt.Sprintf("Rename '%s' to '%s' and update the line(s)/reference(s) above? [y/N] ", oldName, newName), false)
			if err != nil {
				return err
			}
			if !ok {
				return declined(plan.Skipped)
			}
		}
	}
	kept, err := plan.Apply()
	if err != nil {
		return err
	}
	for _, f := range kept {
		ui.Warn(os.Stdout, fmt.Sprintf("%s kept in place: settings for '%s' already exist there", f, plan.NewName))
	}
	ui.Done(os.Stdout, fmt.Sprintf("Renamed modules/%s -> modules/%s", plan.Unit.Path, plan.NewPath))
	if plan.Shadows != "" {
		ui.Warn(os.Stdout, fmt.Sprintf("modules/%s now shadows modules/%s", plan.NewPath, plan.Shadows))
	}
	return nil
}
