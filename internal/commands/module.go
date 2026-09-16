package commands

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/DeprecatedLuar/luxos/internal/commands/help"
	"github.com/DeprecatedLuar/luxos/internal/commands/shared"
	"github.com/DeprecatedLuar/luxos/internal/framework"
	"github.com/DeprecatedLuar/luxos/internal/imports"
	"github.com/DeprecatedLuar/luxos/internal/paths"
	"github.com/DeprecatedLuar/luxos/internal/refs"
	"github.com/DeprecatedLuar/luxos/internal/units"
)

// entrypointName is the file name under Paths.Modules that mirrors the
// active host's selection (implementation-plan.md #18) — the file
// list/enable/disable act on.
const entrypointName = "default.nix"

// moduleSimpleTemplate is the minimal scaffold `module add` writes for a
// non-user module, ported verbatim from bash's MODULE_SIMPLE_TEMPLATE.
const moduleSimpleTemplate = "{ ... }:\n\n{\n}\n"

// moduleUserPlaceholder is the literal replaced with the real account name
// in the embedded user template's account.nix, matching bash's
// MODULE_USER_PLACEHOLDER.
const moduleUserPlaceholder = "users.users.user ="

// moduleFrameworkCategory is the reserved top-level category `add`/`remove`/
// `rename` refuse to touch: modules/system is owned by internal/framework,
// not by CONFIG_DIR.
const moduleFrameworkCategory = "system"

// State markers for `module list`, per implementation-plan.md Phase 5's
// state table.
const (
	markerEnabledOnly = "⊕" // enabled, not running
	markerEnabledBoth = "◉" // enabled and running
	markerRunningOnly = "⊘" // running, not enabled
	markerNeither     = "○" // neither
)

// Module implements `luxos module`, dispatching to one unexported function
// per verb over CONFIG_DIR/modules, ported from
// bin/lib/lux/commands/module.sh.
func Module(args []string) error {
	if len(args) > 0 && (args[0] == "--help" || args[0] == "-h") {
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
	case "list":
		return moduleList(p, rest)
	case "add":
		return moduleAdd(p, rest)
	case "edit":
		return moduleEdit(p, rest)
	case "enable":
		return moduleEnable(p, rest)
	case "disable":
		return moduleDisable(p, rest)
	case "remove", "rm":
		return moduleRemove(p, rest)
	case "rename", "rn":
		return moduleRename(p, rest)
	default:
		return fmt.Errorf("unknown module command '%s'\n  Usage: luxos module <list|add|edit|enable|disable|remove|rename> ...", verb)
	}
}

//──[state markers]───────────────────────────────────────────────────────

// moduleMarker returns the one marker character for (enabled?, running?),
// per Phase 5's state-marker table. Pure.
func moduleMarker(enabled, running bool) string {
	switch {
	case enabled && running:
		return markerEnabledBoth
	case enabled && !running:
		return markerEnabledOnly
	case !enabled && running:
		return markerRunningOnly
	default:
		return markerNeither
	}
}

// moduleMarkerRank returns the sort rank for a marker (lower sorts first),
// per Phase 5's "ordered by that state order" (⊕, ◉, ⊘, ○). Pure.
func moduleMarkerRank(marker string) int {
	switch marker {
	case markerEnabledOnly:
		return 0
	case markerEnabledBoth:
		return 1
	case markerRunningOnly:
		return 2
	default:
		return 3
	}
}

// moduleRow is one rendered line's worth of data for `module list`.
type moduleRow struct {
	category string // "." for a root unit
	name     string
	marker   string
	rank     int
}

// moduleSortRows sorts rows by rank then name (byte order), matching bash's
// _module_sort_rows. Pure, stable.
func moduleSortRows(rows []moduleRow) {
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].rank != rows[j].rank {
			return rows[i].rank < rows[j].rank
		}
		return rows[i].name < rows[j].name
	})
}

//──[list]─────────────────────────────────────────────────────────────────

// nameSet reads file's active import paths (or an empty set if file doesn't
// exist) and returns the set of unit names they name.
func nameSet(file string) (map[string]bool, error) {
	set := map[string]bool{}
	if _, err := os.Stat(file); err != nil {
		if os.IsNotExist(err) {
			return set, nil
		}
		return nil, err
	}

	itemPaths, err := imports.List(file)
	if err != nil {
		return nil, err
	}
	for _, p := range itemPaths {
		set[imports.NameFromPath(p)] = true
	}
	return set, nil
}

// moduleBuildRows builds one moduleRow per unit in us whose Path lies under
// categoryPath (or every unit, when categoryPath is ""), using enabled/
// running name sets. Pure given its inputs.
func moduleBuildRows(us []units.Unit, enabled, running map[string]bool, categoryPath string) []moduleRow {
	var rows []moduleRow
	prefix := ""
	if categoryPath != "" {
		prefix = categoryPath + "/"
	}
	for _, u := range us {
		if prefix != "" && !strings.HasPrefix(u.Path, prefix) {
			continue
		}
		cat := filepath.Dir(u.Path)
		marker := moduleMarker(enabled[u.Name], running[u.Name])
		rows = append(rows, moduleRow{
			category: cat,
			name:     u.Name,
			marker:   marker,
			rank:     moduleMarkerRank(marker),
		})
	}
	return rows
}

// moduleRenderTTY writes rows to w in TTY form: root units first (no
// header), then every other category present, headers sorted, each group
// ordered by moduleSortRows.
func moduleRenderTTY(w *strings.Builder, rows []moduleRow) {
	if len(rows) == 0 {
		w.WriteString("(no modules)\n")
		return
	}

	var root []moduleRow
	catSet := map[string]bool{}
	for _, r := range rows {
		if r.category == "." {
			root = append(root, r)
		} else {
			catSet[r.category] = true
		}
	}

	moduleSortRows(root)
	for _, r := range root {
		fmt.Fprintf(w, "%s %s\n", r.marker, r.name)
	}

	cats := make([]string, 0, len(catSet))
	for c := range catSet {
		cats = append(cats, c)
	}
	sort.Strings(cats)

	for _, cat := range cats {
		fmt.Fprintf(w, "%s:\n", cat)
		var group []moduleRow
		for _, r := range rows {
			if r.category == cat {
				group = append(group, r)
			}
		}
		moduleSortRows(group)
		for _, r := range group {
			fmt.Fprintf(w, "  %s %s\n", r.marker, r.name)
		}
	}
}

// moduleRenderPlain writes rows to w in piped form: one "category/name" per
// line ("name" for root units), no headers, no markers, byte-sorted.
func moduleRenderPlain(w *strings.Builder, rows []moduleRow) {
	if len(rows) == 0 {
		return
	}
	lines := make([]string, 0, len(rows))
	for _, r := range rows {
		if r.category == "." {
			lines = append(lines, r.name)
		} else {
			lines = append(lines, r.category+"/"+r.name)
		}
	}
	sort.Strings(lines)
	for _, l := range lines {
		fmt.Fprintln(w, l)
	}
}

// moduleList implements `module list [category-path]`.
func moduleList(p paths.Paths, args []string) error {
	var categoryPath string
	if len(args) > 0 {
		categoryPath = args[0]
	}
	categoryPath = strings.TrimSuffix(categoryPath, "/")

	if categoryPath != "" {
		catDir := filepath.Join(p.Modules, categoryPath)
		info, err := os.Stat(catDir)
		if err != nil || !info.IsDir() {
			return fmt.Errorf("unknown category '%s'", categoryPath)
		}
		if fi, err := os.Stat(filepath.Join(catDir, entrypointName)); err == nil && !fi.IsDir() {
			return fmt.Errorf("'%s' is a module, not a category", categoryPath)
		}
	}

	entrypoint := filepath.Join(p.Modules, entrypointName)

	tty := false
	if fi, err := os.Stdout.Stat(); err == nil {
		tty = fi.Mode()&os.ModeCharDevice != 0
	}

	if _, err := os.Stat(p.RunningModules); err != nil {
		fmt.Fprintf(os.Stderr, "Note: no running generation found at %s — state unknown until the next switch; every enabled module shows as staged.\n", p.RunningModules)
	}

	us, err := units.Walk(p.Modules)
	if err != nil {
		return err
	}

	enabled, err := nameSet(entrypoint)
	if err != nil {
		return err
	}
	running, err := nameSet(p.RunningModules)
	if err != nil {
		return err
	}

	rows := moduleBuildRows(us, enabled, running, categoryPath)

	var out strings.Builder
	if tty {
		moduleRenderTTY(&out, rows)
	} else {
		moduleRenderPlain(&out, rows)
	}
	fmt.Print(out.String())
	return nil
}

//──[add]──────────────────────────────────────────────────────────────────

// moduleAddUser scaffolds modules/<target> from the embedded user template,
// filling in the literal account name.
func moduleAddUser(modulesRoot, target string) error {
	name := filepath.Base(target)
	dest := filepath.Join(modulesRoot, target)

	if err := os.MkdirAll(dest, 0755); err != nil {
		return err
	}

	defaultNix, err := framework.File("templates/user/default.nix")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dest, "default.nix"), defaultNix, 0644); err != nil {
		return err
	}

	account, err := framework.File("templates/user/account.nix")
	if err != nil {
		return err
	}
	if !strings.Contains(string(account), moduleUserPlaceholder) {
		return fmt.Errorf("embedded templates/user/account.nix has no '%s' to fill in", moduleUserPlaceholder)
	}
	filled := strings.ReplaceAll(string(account), moduleUserPlaceholder, "users.users."+name+" =")
	if err := os.WriteFile(filepath.Join(dest, "account.nix"), []byte(filled), 0644); err != nil {
		return err
	}

	fmt.Printf("Created modules/%s/{default.nix,account.nix}\n", target)
	return nil
}

// moduleAdd implements `module add <category/name> [--enable]`.
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

	existingPath, found, err := resolveUnitPath(p.Modules, name)
	if err != nil {
		return err
	}
	if found {
		return fmt.Errorf("module name '%s' already exists at modules/%s", name, existingPath)
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
		if err := os.WriteFile(filepath.Join(p.Modules, target+".nix"), []byte(moduleSimpleTemplate), 0644); err != nil {
			return err
		}
		fmt.Printf("Created modules/%s.nix\n", target)
	}

	if opts["enable"] != "" {
		return toggleModule(p, name, true)
	}
	return nil
}

//──[edit]─────────────────────────────────────────────────────────────────

// moduleEdit implements `module edit <name>`.
func moduleEdit(p paths.Paths, args []string) error {
	var name string
	if len(args) > 0 {
		name = args[0]
	}
	if name == "" {
		return fmt.Errorf("usage: luxos module edit <name>")
	}

	path, found, err := resolveUnitPath(p.Modules, name)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("unknown module '%s'", name)
	}

	target := filepath.Join(p.Modules, path)
	file := target
	if fi, err := os.Stat(target); err == nil && fi.IsDir() {
		file = filepath.Join(target, entrypointName)
	}

	editor := os.Getenv("EDITOR")
	if editor == "" {
		return fmt.Errorf("$EDITOR is not set")
	}

	cmd := exec.Command(editor, file)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

//──[enable/disable]─────────────────────────────────────────────────────────

// enabledPathForName returns the bare import path (as actually written,
// possibly stale) for name in file, or ("", false, nil) if it isn't
// enabled there (or file doesn't exist).
func enabledPathForName(file, name string) (string, bool, error) {
	if _, err := os.Stat(file); err != nil {
		if os.IsNotExist(err) {
			return "", false, nil
		}
		return "", false, err
	}
	itemPaths, err := imports.List(file)
	if err != nil {
		return "", false, err
	}
	for _, path := range itemPaths {
		if imports.NameFromPath(path) == name {
			return path, true, nil
		}
	}
	return "", false, nil
}

// toggleModule is the shared body for enable/disable: acts only on the
// active host's entrypoint (CONFIG_DIR/modules/default.nix, #18).
func toggleModule(p paths.Paths, name string, enable bool) error {
	resolvedPath, found, err := resolveUnitPath(p.Modules, name)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("unknown module '%s'", name)
	}

	entrypoint := filepath.Join(p.Modules, entrypointName)
	if _, err := os.Stat(entrypoint); err != nil {
		return fmt.Errorf("no active host entrypoint at %s — run setup or nixos-rebuild first", entrypoint)
	}

	existingPath, isEnabled, err := enabledPathForName(entrypoint, name)
	if err != nil {
		return err
	}

	if enable {
		if isEnabled {
			fmt.Printf("'%s' is already enabled\n", name)
			return nil
		}
		return imports.Add(entrypoint, resolvedPath)
	}

	if !isEnabled {
		fmt.Printf("'%s' is already disabled\n", name)
		return nil
	}
	return imports.Remove(entrypoint, existingPath)
}

func moduleEnable(p paths.Paths, args []string) error {
	var name string
	if len(args) > 0 {
		name = args[0]
	}
	if name == "" {
		return fmt.Errorf("usage: luxos module enable <name>")
	}
	return toggleModule(p, name, true)
}

func moduleDisable(p paths.Paths, args []string) error {
	var name string
	if len(args) > 0 {
		name = args[0]
	}
	if name == "" {
		return fmt.Errorf("usage: luxos module disable <name>")
	}
	return toggleModule(p, name, false)
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

// moduleRemove implements `module remove|rm <name> [-y]`.
func moduleRemove(p paths.Paths, args []string) error {
	opts, rest, err := shared.Parse("y|y:bool", args)
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

	path, found, err := resolveUnitPath(p.Modules, name)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("unknown module '%s'", name)
	}
	if isFrameworkPath(path) {
		return fmt.Errorf("'%s' is a framework module (modules/%s) — not owned by this config", name, path)
	}

	importers, err := imports.Importers(p.Local, name)
	if err != nil {
		return err
	}
	printReferencedBy(fmt.Sprintf("'%s' is imported by:", name), importers, fmt.Sprintf("'%s' is not imported by any host.", name))

	dependents, err := refs.Dependents(p.Modules, name)
	if err != nil {
		return err
	}
	printReferencedBy(fmt.Sprintf("'%s' is referenced by:", name), dependents, "")

	if opts["y"] == "" {
		ok, err := shared.Confirm(fmt.Sprintf("Remove modules/%s and the import line(s)/reference(s) above? [y/N] ", path), false)
		if err != nil {
			return err
		}
		if !ok {
			fmt.Println("Aborted.")
			return nil
		}
	}

	// refs.Retarget first: it validates every dependent's call shape before
	// writing anything and refuses the whole operation on a bad shape, so a
	// refusal here leaves the tree completely untouched.
	if _, err := refs.Retarget(p.Modules, name, ""); err != nil {
		return err
	}
	if _, err := imports.Retarget(p.Local, name, ""); err != nil {
		return err
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

// moduleRename implements `module rename|rn <old> <new> [-y]`.
func moduleRename(p paths.Paths, args []string) error {
	opts, rest, err := shared.Parse("y|y:bool", args)
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

	oldPath, found, err := resolveUnitPath(p.Modules, oldName)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("unknown module '%s'", oldName)
	}
	if isFrameworkPath(oldPath) {
		return fmt.Errorf("'%s' is a framework module (modules/%s) — not owned by this config", oldName, oldPath)
	}

	if existingPath, found, err := resolveUnitPath(p.Modules, newName); err != nil {
		return err
	} else if found {
		return fmt.Errorf("module name '%s' already exists at modules/%s", newName, existingPath)
	}

	dependents, err := refs.Dependents(p.Modules, oldName)
	if err != nil {
		return err
	}
	if len(dependents) > 0 {
		printReferencedBy(fmt.Sprintf("'%s' is referenced by:", oldName), dependents, "")

		if opts["y"] == "" {
			ok, err := shared.Confirm(fmt.Sprintf("Rename '%s' to '%s' and update the reference(s) above? [y/N] ", oldName, newName), false)
			if err != nil {
				return err
			}
			if !ok {
				fmt.Println("Aborted.")
				return nil
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

	// refs.Retarget first (validates every dependent's call shape before
	// writing anything, refusing the whole operation on a bad shape): a
	// refusal here leaves the unit unmoved and every host entrypoint
	// untouched.
	if _, err := refs.Retarget(p.Modules, oldName, newName); err != nil {
		return err
	}
	if err := os.Rename(oldFull, filepath.Join(p.Modules, newPath)); err != nil {
		return err
	}
	if _, err := imports.Retarget(p.Local, oldName, newPath); err != nil {
		return err
	}

	if category == "users" || strings.HasPrefix(category, "users/") {
		fmt.Printf("Note: the account name in modules/%s/account.nix is still '%s' — rename it there by hand if the actual system user should change too.\n", newPath, oldName)
	}

	fmt.Printf("Renamed modules/%s -> modules/%s\n", oldPath, newPath)
	return nil
}

//──[shared helpers]──────────────────────────────────────────────────────

// resolveUnitPath walks modulesDir and resolves name to its path.
func resolveUnitPath(modulesDir, name string) (string, bool, error) {
	us, err := units.Walk(modulesDir)
	if err != nil {
		return "", false, err
	}
	path, ok := units.Resolve(us, name)
	return path, ok, nil
}

// isFrameworkPath reports whether a modules-relative path lies under the
// reserved framework category (modules/system), which add/remove/rename
// refuse to touch.
func isFrameworkPath(path string) bool {
	return path == moduleFrameworkCategory || strings.HasPrefix(path, moduleFrameworkCategory+"/")
}
