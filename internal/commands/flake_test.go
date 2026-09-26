package commands

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/DeprecatedLuar/luxos/internal/staging"
	"github.com/DeprecatedLuar/luxos/internal/upstream"
)

func testLockGraph() staging.LockGraph {
	return staging.LockGraph{
		Root: []string{"ambxst", "flake-file", "luxos", "old", "shared"},
		Nodes: map[string]staging.LockNode{
			staging.LockRootNode: {Inputs: map[string]string{
				"ambxst": "ambxst", "flake-file": "flake-file", "luxos": "luxos", "old": "old", "shared": "shared_2",
			}},
			"ambxst":    {Inputs: map[string]string{"axctl": "axctl", "nixpkgs": "nixpkgs_2"}},
			"axctl":     {Inputs: map[string]string{"deep": "deep"}},
			"deep":      {Inputs: map[string]string{"back": "ambxst"}},
			"nixpkgs_2": {},
			"luxos":     {},
			"old":       {},
			"shared_2":  {},
		},
	}
}

func rowByName(rows []moduleRow, name string) (moduleRow, bool) {
	for _, r := range rows {
		if r.name == name {
			return r, true
		}
	}
	return moduleRow{}, false
}

func TestFlakeBuildRowsMarkersAndPlacement(t *testing.T) {
	decls := map[string][]string{
		"ambxst":     {"desktop/shells/ambxst"},
		"shared":     {"a.nix", "b/c.nix"},
		"newone":     {"local/x.nix"},
		"flake-file": {"y.nix"},
		"nixpkgs":    {"nixpkgs.nix"},
	}
	rows := flakeBuildRows(decls, testLockGraph(), nil)

	cases := []struct {
		name, marker string
		category     []string
	}{
		{"ambxst", markerEnabledBoth, []string{"desktop", "shells"}},
		{"shared", markerEnabledBoth, nil},
		{"newone", markerEnabledOnly, []string{"local"}},
		{"old", markerRunningOnly, nil},
		{"luxos", markerEnabledBoth, nil},
		{"nixpkgs", markerEnabledOnly, nil},
	}
	for _, c := range cases {
		r, ok := rowByName(rows, c.name)
		if !ok {
			t.Errorf("row %q missing", c.name)
			continue
		}
		if r.marker != c.marker {
			t.Errorf("%s marker = %q, want %q", c.name, r.marker, c.marker)
		}
		if !reflect.DeepEqual(r.category, c.category) {
			t.Errorf("%s category = %v, want %v", c.name, r.category, c.category)
		}
	}
	if _, ok := rowByName(rows, "flake-file"); ok {
		t.Error("flake-file must never be listed")
	}
	if len(rows) != len(cases) {
		t.Errorf("got %d rows, want %d", len(rows), len(cases))
	}
}

func TestFlakeBuildRowsBuiltinsWithoutDeclarationsOrLock(t *testing.T) {
	rows := flakeBuildRows(nil, staging.LockGraph{}, nil)
	for _, name := range []string{"luxos", "nixpkgs"} {
		r, ok := rowByName(rows, name)
		if !ok || r.marker != markerEnabledOnly || len(r.category) != 0 {
			t.Errorf("%s = %+v, want root %s", name, r, markerEnabledOnly)
		}
	}
}

func TestFlakeBuildRowsTransitive(t *testing.T) {
	rows := flakeBuildRows(map[string][]string{"ambxst": {"a.nix"}}, testLockGraph(), nil)
	r, _ := rowByName(rows, "ambxst")

	axctl, ok := rowByName(r.children, "axctl")
	if !ok || axctl.marker != markerPulled {
		t.Fatalf("axctl child = %+v", axctl)
	}
	if _, ok := rowByName(r.children, "nixpkgs"); !ok {
		t.Error("nixpkgs child missing")
	}
	deep, ok := rowByName(axctl.children, "deep")
	if !ok {
		t.Fatal("grandchild deep missing (no depth limit)")
	}
	if len(deep.children) != 0 {
		t.Errorf("cycle back to ambxst must be cut, got %+v", deep.children)
	}
}

func TestFlakeRenderTreeAndPlain(t *testing.T) {
	rows := flakeBuildRows(map[string][]string{"ambxst": {"desktop/shells/ambxst"}}, testLockGraph(), nil)

	var tty strings.Builder
	moduleRenderTTY(&tty, rows, flakeInputsLabel, treePalette{})
	wantTTY := `inputs/
├── ⊕ nixpkgs
├── ◉ luxos
├── ⊘ old
├── ⊘ shared
└── desktop/
    └── shells/
        └── ◉ ambxst
            ├── ◍ axctl
            │   └── ◍ deep
            └── ◍ nixpkgs

`
	if tty.String() != wantTTY {
		t.Errorf("tree:\n%s\nwant:\n%s", tty.String(), wantTTY)
	}

	var plain strings.Builder
	flakeRenderPlain(&plain, rows)
	wantPlain := "ambxst/axctl\tpulled\tunknown\nambxst/axctl/deep\tpulled\tunknown\nambxst/nixpkgs\tpulled\tunknown\n" +
		"desktop/shells/ambxst\tactive\tunknown\nluxos\tactive\tunknown\nnixpkgs\tstaged\tunknown\nold\tleftover\tunknown\nshared\tleftover\tunknown\n"
	if plain.String() != wantPlain {
		t.Errorf("plain:\n%q\nwant:\n%q", plain.String(), wantPlain)
	}
}

func TestFlakeBuildRowsNotesRenderAfterName(t *testing.T) {
	notes := map[string]string{"ambxst": flakeNoteBehind, "axctl": flakeNoteUnknown}
	rows := flakeBuildRows(map[string][]string{"ambxst": {"a.nix"}}, testLockGraph(), notes)

	var tty strings.Builder
	moduleRenderTTY(&tty, rows, flakeInputsLabel, treePalette{})
	for _, want := range []string{"◉ ambxst ↑\n", "◍ axctl ?\n"} {
		if !strings.Contains(tty.String(), want) {
			t.Errorf("tree missing %q:\n%s", want, tty.String())
		}
	}

	var plain strings.Builder
	flakeRenderPlain(&plain, rows)
	if strings.ContainsAny(plain.String(), "↑?") {
		t.Errorf("plain output must stay parseable:\n%s", plain.String())
	}
	if !strings.Contains(plain.String(), "ambxst/axctl\tpulled\tunknown\n") ||
		!strings.Contains(plain.String(), "ambxst\tactive\tbehind\n") && !strings.Contains(plain.String(), "/ambxst\tactive\tbehind\n") {
		t.Errorf("statuses:\n%s", plain.String())
	}
}

func TestFlakeUpstreamNotesUnsupportedIsUnknown(t *testing.T) {
	graph := staging.LockGraph{
		Nodes: map[string]staging.LockNode{
			staging.LockRootNode: {Inputs: map[string]string{"a": "a", "flake-file": "ff"}},
			"a":                  {Original: staging.LockRef{Type: "path"}, Locked: staging.LockRef{Type: "path", Rev: "abc"}},
			"ff":                 {Original: staging.LockRef{Type: "path"}, Locked: staging.LockRef{Rev: "abc"}},
		},
	}
	notes := flakeUpstreamNotes(graph)
	if notes["a"] != flakeNoteUnknown {
		t.Errorf("a = %q, want %q", notes["a"], flakeNoteUnknown)
	}
	if _, ok := notes["ff"]; ok {
		t.Error("flake-file must not be checked")
	}
}

//──[show]─────────────────────────────────────────────────────────────────

const (
	showLocked = "1e9592a000000000000000000000000000000000"
	showTip    = "2a704c4300000000000000000000000000000000"
	showTag138 = "c62a7ac000000000000000000000000000000000"
	showMid    = "3333333000000000000000000000000000000000"
)

func showGraph() staging.LockGraph {
	gh := func(rev string) staging.LockRef {
		return staging.LockRef{Type: "github", Owner: "Axenide", Repo: "Ambxst", Rev: rev}
	}
	return staging.LockGraph{
		Root: []string{"ambxst", "old", "unstable"},
		Nodes: map[string]staging.LockNode{
			staging.LockRootNode: {Inputs: map[string]string{"ambxst": "ambxst", "old": "old", "unstable": "unstable"}},
			"ambxst":             {Inputs: map[string]string{"nixpkgs": "nixpkgs_2", "axctl": "axctl"}, Original: staging.LockRef{Type: "github", Owner: "Axenide", Repo: "Ambxst"}, Locked: gh(showLocked)},
			"axctl":              {Original: staging.LockRef{Type: "github", Owner: "Axenide", Repo: "axctl"}, Locked: gh(showLocked)},
			"nixpkgs_2":          {},
			"old":                {Original: staging.LockRef{Type: "git", URL: "https://example.org/x.git", Ref: "main"}, Locked: staging.LockRef{Rev: showLocked}},
			"unstable":           {Original: staging.LockRef{Type: "github", Owner: "NixOS", Repo: "nixpkgs", Ref: "nixpkgs-unstable"}, Locked: gh(showLocked)},
		},
	}
}

func showSites() map[string][]flakeDecl {
	return map[string][]flakeDecl{
		"ambxst": {{unit: "desktop/shells/ambxst", file: "modules/desktop/shells/ambxst/module.nix", line: 8, url: "github:Axenide/Ambxst"}},
		"unstable": {
			{unit: "desktop/compositors/hyprland", file: "modules/desktop/compositors/hyprland.nix", line: 3},
			{unit: "services/docker", file: "modules/services/docker.nix", line: 4},
		},
		"newone": {{unit: "local/x", file: "local/modules/x.nix", line: 2, url: "github:o/newone"}},
	}
}

func showRender(t *testing.T, name string, fetch func(staging.LockRef, string) *flakeUpstream) string {
	t.Helper()
	v, err := flakeBuildView(name, "h", showSites(), showGraph(), fetch)
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	flakeRenderView(&b, v, treePalette{})
	return b.String()
}

func TestFlakeShowBehindWithTagAndCommits(t *testing.T) {
	fetch := func(staging.LockRef, string) *flakeUpstream {
		return &flakeUpstream{
			tip:   showTip,
			tags:  map[string]string{showLocked: "1.3.4", showTag138: "1.3.8"},
			ahead: 55,
			shas:  []string{"x", showTag138, showMid, showTip},
		}
	}
	want := `◉ ambxst ↑
├── source    github:Axenide/Ambxst (default branch)
├── declared  modules/desktop/shells/ambxst/module.nix:8
├── version
│   ├── current  1.3.4
│   └── latest   1.3.8  +55 commits
└── pulls in
    └── axctl, nixpkgs
`
	if got := showRender(t, "ambxst", fetch); got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestFlakeShowUntaggedAndMultipleDeclarations(t *testing.T) {
	fetch := func(staging.LockRef, string) *flakeUpstream {
		return &flakeUpstream{tip: showTip, ahead: 7853, shas: []string{showMid, showTip}}
	}
	want := `◉ unstable ↑
├── source    github:NixOS/nixpkgs/nixpkgs-unstable
├── declared
│   ├── modules/desktop/compositors/hyprland.nix:3
│   └── modules/services/docker.nix:4
└── version
    ├── current  1e9592a
    └── latest   2a704c4  +7853 commits
`
	if got := showRender(t, "unstable", fetch); got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestFlakeShowUpToDate(t *testing.T) {
	fetch := func(staging.LockRef, string) *flakeUpstream { return &flakeUpstream{tip: showLocked} }
	want := `◍ axctl
├── source    github:Axenide/axctl (default branch)
├── declared  pulled in by ambxst
└── version
    └── current  1e9592a  up to date
`
	if got := showRender(t, "ambxst/axctl", fetch); got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestFlakeShowFailuresRenderQuestionMark(t *testing.T) {
	boom := errors.New("boom")
	tip := func(staging.LockRef, string) *flakeUpstream { return &flakeUpstream{tipErr: boom} }
	got := showRender(t, "unstable", tip)
	if !strings.Contains(got, "unstable ?\n") || !strings.Contains(got, "latest   ?\n") {
		t.Errorf("tip failure:\n%s", got)
	}

	cmp := func(staging.LockRef, string) *flakeUpstream { return &flakeUpstream{tip: showTip, compareErr: boom} }
	got = showRender(t, "unstable", cmp)
	if !strings.Contains(got, "unstable ↑\n") || !strings.Contains(got, "latest   ?\n") || strings.Contains(got, "commits") {
		t.Errorf("compare failure:\n%s", got)
	}

	tags := func(staging.LockRef, string) *flakeUpstream {
		return &flakeUpstream{tip: showTip, tagsErr: boom, ahead: 3, shas: []string{showTip}}
	}
	got = showRender(t, "unstable", tags)
	if !strings.Contains(got, "current  1e9592a\n") || !strings.Contains(got, "latest   ?\n") {
		t.Errorf("tags failure:\n%s", got)
	}
}

func TestFlakeShowUnsupportedCompareUsesTagOnTip(t *testing.T) {
	fetch := func(staging.LockRef, string) *flakeUpstream {
		return &flakeUpstream{tip: showTip, tags: map[string]string{showTip: "v2"}, compareErr: upstream.ErrUnsupported}
	}
	got := showRender(t, "old", fetch)
	if !strings.Contains(got, "source    https://example.org/x.git\n") || !strings.Contains(got, "latest   v2\n") || strings.Contains(got, "commits") {
		t.Errorf("got:\n%s", got)
	}
}

func TestFlakeShowOfflineAndUnlocked(t *testing.T) {
	got := showRender(t, "ambxst", nil)
	if !strings.Contains(got, "current  1e9592a\n") || strings.Contains(got, "latest") || strings.Contains(got, "↑") {
		t.Errorf("offline:\n%s", got)
	}

	want := `⊕ newone
├── source    github:o/newone
├── declared  local/modules/x.nix:2
└── version
    └── current  not locked yet (the next rebuild locks it)
`
	if got := showRender(t, "newone", nil); got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}

	if got := showRender(t, "luxos", nil); !strings.Contains(got, "declared  built in\n") || strings.Contains(got, "source") {
		t.Errorf("built in:\n%s", got)
	}
	if got := showRender(t, "old", nil); !strings.Contains(got, "⊘ old\n") || !strings.Contains(got, "no longer declared") {
		t.Errorf("gone:\n%s", got)
	}
}

func TestFlakeShowUnknownInput(t *testing.T) {
	for _, name := range []string{"nope", "flake-file", "ambxst/nope", "newone/x", "nope/x"} {
		_, err := flakeBuildView(name, "h", showSites(), showGraph(), nil)
		if err == nil || !strings.Contains(err.Error(), "no input '"+name+"' for h\n  list them with: luxos flakes") {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func plainView(t *testing.T, name string, fetch func(staging.LockRef, string) *flakeUpstream) string {
	t.Helper()
	v, err := flakeBuildView(name, "h", showSites(), showGraph(), fetch)
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	flakeRenderViewPlain(&b, v.data)
	return b.String()
}

func TestFlakeShowPlainBehindCurrentOffline(t *testing.T) {
	behind := func(staging.LockRef, string) *flakeUpstream {
		return &flakeUpstream{
			tip:   showTip,
			tags:  map[string]string{showLocked: "1.3.4", showTag138: "1.3.8"},
			ahead: 55,
			shas:  []string{"x", showTag138, showMid, showTip},
		}
	}
	want := "name=ambxst\nstate=active\nstatus=behind\nsource=github:Axenide/Ambxst\n" +
		"declared=modules/desktop/shells/ambxst/module.nix:8\ncurrent=1.3.4\nlatest=1.3.8\ncommits=55\npulls=axctl nixpkgs\n"
	if got := plainView(t, "ambxst", behind); got != want {
		t.Errorf("behind:\n%s\nwant:\n%s", got, want)
	}

	current := func(staging.LockRef, string) *flakeUpstream { return &flakeUpstream{tip: showLocked} }
	want = "name=ambxst/axctl\nstate=pulled\nstatus=current\nsource=github:Axenide/axctl\n" +
		"declared=\ncurrent=1e9592a\nlatest=\ncommits=0\npulls=\n"
	if got := plainView(t, "ambxst/axctl", current); got != want {
		t.Errorf("current:\n%s\nwant:\n%s", got, want)
	}

	want = "name=ambxst\nstate=active\nstatus=unknown\nsource=github:Axenide/Ambxst\n" +
		"declared=modules/desktop/shells/ambxst/module.nix:8\ncurrent=1e9592a\nlatest=\ncommits=\npulls=axctl nixpkgs\n"
	if got := plainView(t, "ambxst", nil); got != want {
		t.Errorf("offline:\n%s\nwant:\n%s", got, want)
	}
}

func TestFlakeListJSON(t *testing.T) {
	rows := flakeBuildRows(map[string][]string{}, testLockGraph(), nil)
	var b strings.Builder
	if err := flakeRenderListJSON(&b, rows); err != nil {
		t.Fatal(err)
	}
	var got []map[string]string
	if err := json.Unmarshal([]byte(b.String()), &got); err != nil {
		t.Fatal(err)
	}
	if len(got) == 0 || got[0]["path"] == "" || got[0]["state"] == "" || got[0]["status"] == "" {
		t.Errorf("unexpected list JSON:\n%s", b.String())
	}
	for i := 1; i < len(got); i++ {
		if got[i-1]["path"] > got[i]["path"] {
			t.Errorf("not byte-sorted:\n%s", b.String())
		}
	}
}

func showViews(t *testing.T, fetch func(staging.LockRef, string) *flakeUpstream, names ...string) []flakeView {
	t.Helper()
	var views []flakeView
	for _, name := range names {
		v, err := flakeBuildView(name, "h", showSites(), showGraph(), fetch)
		if err != nil {
			t.Fatal(err)
		}
		views = append(views, v)
	}
	return views
}

func TestFlakeViewJSONSingleBehind(t *testing.T) {
	behind := func(staging.LockRef, string) *flakeUpstream {
		return &flakeUpstream{tip: showTip, tags: map[string]string{showLocked: "1.3.4", showTag138: "1.3.8"},
			ahead: 55, shas: []string{"x", showTag138, showMid, showTip}}
	}
	var b strings.Builder
	if err := flakeRenderViewsJSON(&b, showViews(t, behind, "ambxst")); err != nil {
		t.Fatal(err)
	}
	want := `{
  "name": "ambxst",
  "state": "active",
  "status": "behind",
  "source": "github:Axenide/Ambxst",
  "declared": [
    "modules/desktop/shells/ambxst/module.nix:8"
  ],
  "pulls": [
    "axctl",
    "nixpkgs"
  ],
  "current": "1.3.4",
  "latest": "1.3.8",
  "commits": 55
}
`
	if b.String() != want {
		t.Errorf("got:\n%s\nwant:\n%s", b.String(), want)
	}
}

func TestFlakeViewJSONSeveralAndNulls(t *testing.T) {
	var b strings.Builder
	if err := flakeRenderViewsJSON(&b, showViews(t, nil, "ambxst/axctl", "unstable")); err != nil {
		t.Fatal(err)
	}
	var got []map[string]any
	if err := json.Unmarshal([]byte(b.String()), &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0]["latest"] != nil || got[0]["commits"] != nil || got[1]["name"] != "unstable" {
		t.Errorf("unexpected array:\n%s", b.String())
	}
	if d, ok := got[0]["declared"].([]any); !ok || len(d) != 0 {
		t.Errorf("declared must be []:\n%s", b.String())
	}
}
