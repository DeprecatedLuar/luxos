package commands

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/DeprecatedLuar/luxos/internal/commands/help"
	"github.com/DeprecatedLuar/luxos/internal/commands/shared"
	"github.com/DeprecatedLuar/luxos/internal/config"
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
		return help.Run([]string{"help", "package"})
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
// "" when there is nothing to compare against: host is not the active one
// (the staging root only holds the active host) or no luxos stage is there.
func packageBaseline(p paths.Paths, host string) (string, error) {
	active, err := config.ActiveHost(p.Modules)
	if err != nil {
		return "", err
	}
	if host != active {
		return "", nil
	}
	base, err := staging.Baseline(p.Staging)
	if err != nil || base == "" {
		return "", err
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
