package commands

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/DeprecatedLuar/luxos/internal/commands/help"
	"github.com/DeprecatedLuar/luxos/internal/commands/shared"
	"github.com/DeprecatedLuar/luxos/internal/nix"
	"github.com/DeprecatedLuar/luxos/internal/packages"
	"github.com/DeprecatedLuar/luxos/internal/paths"
	"github.com/DeprecatedLuar/luxos/internal/staging"
	"github.com/DeprecatedLuar/luxos/internal/ui"
)

const packageFlagSpec = "machine:value config|C:value raw:bool json:bool"

const packageListFlagSpec = packageFlagSpec + " flat:bool"

const packageTmpPattern = "luxos-packages-"

const (
	packageViewSep     = "\n"
	packageViewListSep = " "

	packageMissingName = "missing package name\n  usage: luxos package <name>... [--machine <name>] [--config|-C <dir>] [--raw|--json]"
	packageUnknownName = "no package '%s' for %s\n  list them with: luxos packages"
)

const (
	packagesLabel    = "packages/"
	packagesEmpty    = "(no packages)\n"
	packageBaseInput = "nixpkgs" // the base channel, by convention
	packageUnknown   = "unknown"
	packagePathSep   = "/"

	packageNoBaseline = "Note: no luxos system for %s in %s — nothing to compare against; every package shows as staged.\n"
)

func Package(args []string) error {
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" {
		return help.Run([]string{"help", "package"})
	}

	switch args[0] {
	case "list", "ls":
		return packageList(args[1:])
	default:
		if strings.HasPrefix(args[0], "-") {
			return help.Run([]string{"help", "package"})
		}
		return packageShow(args)
	}
}

// packagesOf evaluates the host's config, staged into a temporary directory,
// and its running system when there is one, and returns their packages
// stated, with the host.
func packagesOf(opts map[string]string) ([]packages.Package, string, error) {
	p, host, _, err := shared.ResolveHost(opts)
	if err != nil {
		return nil, "", err
	}

	var current []nix.PackageDefs
	err = withTempStage(p, host, packageTmpPattern, func(dir string) error {
		defs, err := nix.Packages(dir, host)
		current = defs
		return err
	})
	if err != nil {
		return nil, "", err
	}

	baseDir, err := packageBaseline(p, host)
	if err != nil {
		return nil, "", err
	}
	var base []nix.PackageDefs
	if baseDir == "" {
		fmt.Fprintf(os.Stderr, packageNoBaseline, host, p.Staging)
	} else if base, err = nix.Packages(baseDir, host); err != nil {
		return nil, "", err
	}
	return packages.Diff(base, current), host, nil
}

// packageBaseline returns the staging root holding host's running system, or
// "" when there is nothing to compare against: no luxos stage is there, or it
// was built for another host (a stage defines exactly one).
func packageBaseline(p paths.Paths, host string) (string, error) {
	base, err := staging.Baseline(p.Staging)
	if err != nil || base == "" {
		return "", err
	}
	hosts, err := nix.Hosts(p.Staging)
	if err != nil {
		return "", err
	}
	if !slices.Contains(hosts, host) {
		return "", nil
	}
	return p.Staging, nil
}

//──[list]─────────────────────────────────────────────────────────────────

func packageList(args []string) error {
	opts, _, err := shared.Parse(packageListFlagSpec, args)
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

	pkgs, _, err := packagesOf(opts)
	if err != nil {
		return err
	}

	pal := ui.PaletteFor(os.Stdout)
	var out strings.Builder
	switch {
	case asJSON:
		if err := packageRenderListJSON(&out, pkgs); err != nil {
			return err
		}
	case raw:
		packageRenderPlain(&out, pkgs)
	case flat:
		packageRenderFlat(&out, pkgs, pal)
	case ui.IsTerminal(os.Stdout):
		packageRenderTree(&out, pkgs, pal)
	default:
		packageRenderPlain(&out, pkgs)
	}
	fmt.Print(out.String())
	return nil
}

// packageMarker is the state marker a package is drawn with.
func packageMarker(s packages.State) string {
	switch s {
	case packages.Active:
		return markerEnabledBoth
	case packages.Staged:
		return markerEnabledOnly
	default:
		return markerRunningOnly
	}
}

// packageRow is a package as a leaf; ❄ marks one from an input other than
// the base channel.
func packageRow(pkg packages.Package) ui.Row {
	marker := packageMarker(pkg.State)
	row := ui.Row{Name: pkg.Label(), Marker: marker, Color: markerColor(marker), Rank: moduleMarkerRank(marker)}
	if pkg.Source != "" && pkg.Source != packageBaseInput {
		row.Marks = []ui.Mark{{Glyph: flakeMark, Color: ui.ColorNix}}
	}
	return row
}

// packageTreeRows nests each package under every file declaring it, each file
// under its directories.
func packageTreeRows(pkgs []packages.Package) []ui.Row {
	files := map[string]*ui.Row{}
	var order []string
	for _, pkg := range pkgs {
		for _, f := range pkg.Files {
			r, ok := files[f]
			if !ok {
				r = &ui.Row{Name: filepath.Base(f)}
				if dir := filepath.Dir(f); dir != "." {
					r.Category = strings.Split(dir, packagePathSep)
				}
				files[f] = r
				order = append(order, f)
			}
			r.Children = append(r.Children, packageRow(pkg))
		}
	}
	rows := make([]ui.Row, 0, len(order))
	for _, f := range order {
		rows = append(rows, *files[f])
	}
	return rows
}

func packageRenderTree(w *strings.Builder, pkgs []packages.Package, pal ui.Palette) {
	if len(pkgs) == 0 {
		w.WriteString(packagesEmpty)
		return
	}
	ui.Tree(w, packageTreeRows(pkgs), packagesLabel, pal)
}

// packageRenderFlat lists each package once, its files after it.
func packageRenderFlat(w *strings.Builder, pkgs []packages.Package, pal ui.Palette) {
	if len(pkgs) == 0 {
		w.WriteString(packagesEmpty)
		return
	}
	rows := make([]ui.Row, 0, len(pkgs))
	for _, pkg := range pkgs {
		r := packageRow(pkg)
		r.Trailer = pkg.Files
		rows = append(rows, r)
	}
	ui.Flat(w, rows, pal)
}

// packageDecl is one file declaring one package.
type packageDecl struct {
	file string
	pkg  packages.Package
}

// packageDecls is one entry per file and package, sorted by file, then label.
func packageDecls(pkgs []packages.Package) []packageDecl {
	var decls []packageDecl
	for _, pkg := range pkgs {
		for _, f := range pkg.Files {
			decls = append(decls, packageDecl{f, pkg})
		}
	}
	sort.SliceStable(decls, func(i, j int) bool {
		if decls[i].file != decls[j].file {
			return decls[i].file < decls[j].file
		}
		return decls[i].pkg.Label() < decls[j].pkg.Label()
	})
	return decls
}

// packageUnknownOr is s, or "unknown" when Nix gave no value.
func packageUnknownOr(s string) string {
	if s == "" {
		return packageUnknown
	}
	return s
}

// packageNullable is s, or nil (JSON null) when Nix gave no value.
func packageNullable(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func packageRenderPlain(w *strings.Builder, pkgs []packages.Package) {
	decls := packageDecls(pkgs)
	lines := make([][]string, 0, len(decls))
	for _, d := range decls {
		lines = append(lines, []string{
			d.file, d.pkg.Label(), markerWord(packageMarker(d.pkg.State)),
			packageUnknownOr(d.pkg.Source), packageUnknownOr(d.pkg.Version),
		})
	}
	ui.Plain(w, lines)
}

type packageListJSONRow struct {
	File    string  `json:"file"`
	Name    string  `json:"name"`
	State   string  `json:"state"`
	Source  *string `json:"source"`
	Version *string `json:"version"`
}

func packageRenderListJSON(w *strings.Builder, pkgs []packages.Package) error {
	decls := packageDecls(pkgs)
	rows := make([]packageListJSONRow, 0, len(decls))
	for _, d := range decls {
		rows = append(rows, packageListJSONRow{
			File: d.file, Name: d.pkg.Label(), State: markerWord(packageMarker(d.pkg.State)),
			Source: packageNullable(d.pkg.Source), Version: packageNullable(d.pkg.Version),
		})
	}
	return ui.JSON(w, rows)
}

//──[show]─────────────────────────────────────────────────────────────────

func packageShow(args []string) error {
	opts, names, err := shared.Parse(packageFlagSpec, args)
	if err != nil {
		return err
	}
	if len(names) == 0 {
		return errors.New(packageMissingName)
	}
	if opts["json"] != "" && opts["raw"] != "" {
		return ui.ErrJSONConflict("--raw")
	}

	pkgs, host, err := packagesOf(opts)
	if err != nil {
		return err
	}
	views, err := packageViews(pkgs, names, host)
	if err != nil {
		return err
	}

	var out strings.Builder
	if opts["json"] != "" {
		if err := packageRenderViewsJSON(&out, views); err != nil {
			return err
		}
		fmt.Print(out.String())
		return nil
	}

	tty := ui.IsTerminal(os.Stdout) && opts["raw"] == ""
	pal := ui.PaletteFor(os.Stdout)
	for i, v := range views {
		if i > 0 {
			out.WriteString(packageViewSep)
		}
		if tty {
			packageRenderView(&out, v, pal)
		} else {
			packageRenderViewPlain(&out, v)
		}
	}
	fmt.Print(out.String())
	return nil
}

// packageViews returns the packages each name labels, in argument order.
func packageViews(pkgs []packages.Package, names []string, host string) ([]packages.Package, error) {
	var views []packages.Package
	for _, name := range names {
		found := packages.Named(pkgs, name)
		if len(found) == 0 {
			return nil, fmt.Errorf(packageUnknownName, name, host)
		}
		views = append(views, found...)
	}
	return views, nil
}

func packageRenderView(w *strings.Builder, pkg packages.Package, pal ui.Palette) {
	declared := ui.Line{Label: "declared"}
	if len(pkg.Files) == 1 {
		declared.Value = pkg.Files[0]
	} else {
		for _, f := range pkg.Files {
			declared.Children = append(declared.Children, ui.Line{Value: f})
		}
	}
	marker := packageMarker(pkg.State)
	ui.RenderView(w, ui.View{
		Marker: marker, Color: markerColor(marker), Name: pkg.Label(),
		Lines: []ui.Line{
			{Label: "source", Value: packageUnknownOr(pkg.Source)},
			declared,
			{Label: "version", Value: packageUnknownOr(pkg.Version)},
		},
	}, pal)
}

func packageRenderViewPlain(w *strings.Builder, pkg packages.Package) {
	for _, kv := range [][2]string{
		{"name", pkg.Label()}, {"state", markerWord(packageMarker(pkg.State))},
		{"source", packageUnknownOr(pkg.Source)}, {"declared", strings.Join(pkg.Files, packageViewListSep)},
		{"version", packageUnknownOr(pkg.Version)},
	} {
		fmt.Fprintf(w, "%s=%s\n", kv[0], kv[1])
	}
}

type packageViewJSON struct {
	Name     string   `json:"name"`
	State    string   `json:"state"`
	Source   *string  `json:"source"`
	Declared []string `json:"declared"`
	Version  *string  `json:"version"`
}

// packageRenderViewsJSON writes one object for one view, an array for several.
func packageRenderViewsJSON(w *strings.Builder, views []packages.Package) error {
	objs := make([]packageViewJSON, 0, len(views))
	for _, pkg := range views {
		objs = append(objs, packageViewJSON{
			Name: pkg.Label(), State: markerWord(packageMarker(pkg.State)),
			Source: packageNullable(pkg.Source), Declared: append([]string{}, pkg.Files...),
			Version: packageNullable(pkg.Version),
		})
	}
	if len(objs) == 1 {
		return ui.JSON(w, objs[0])
	}
	return ui.JSON(w, objs)
}
