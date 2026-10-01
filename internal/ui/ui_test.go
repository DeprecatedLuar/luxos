package ui

import (
	"os"
	"strings"
	"testing"
)

func TestSortRows(t *testing.T) {
	rows := []Row{
		{Name: "zeta", Rank: 4},
		{Name: "delta", Rank: 3},
		{Name: "alpha", Rank: 0},
		{Name: "beta", Rank: 0},
		{Name: "gamma", Rank: 1},
	}
	SortRows(rows)

	want := []string{"alpha", "beta", "gamma", "delta", "zeta"}
	for i, name := range want {
		if rows[i].Name != name {
			t.Fatalf("rows[%d].Name = %q, want %q (order: %+v)", i, rows[i].Name, name, rows)
		}
	}
}

func TestSortRowsRankBeforeName(t *testing.T) {
	rows := []Row{{Name: "aaa", Rank: 3}, {Name: "zzz", Rank: 0}}
	SortRows(rows)
	if rows[0].Name != "zzz" || rows[1].Name != "aaa" {
		t.Fatalf("got order %+v, want zzz before aaa", rows)
	}
}

func TestTreeNestsCategoriesAfterRows(t *testing.T) {
	rows := []Row{
		{Category: []string{"desktop", "shells"}, Name: "ambxst", Marker: "◉", Children: []Row{
			{Name: "axctl", Marker: "◍", Children: []Row{{Name: "deep", Marker: "◍"}}},
			{Name: "nixpkgs", Marker: "◍"},
		}},
		{Name: "nixpkgs", Marker: "⊕"},
		{Name: "luxos", Marker: "◉", Rank: 1},
	}
	var b strings.Builder
	Tree(&b, rows, "inputs/", Palette{})
	want := `inputs/
├── ⊕ nixpkgs
├── ◉ luxos
└── desktop/
    └── shells/
        └── ◉ ambxst
            ├── ◍ axctl
            │   └── ◍ deep
            └── ◍ nixpkgs

`
	if b.String() != want {
		t.Errorf("tree:\n%s\nwant:\n%s", b.String(), want)
	}
}

func TestTreeEmpty(t *testing.T) {
	var b strings.Builder
	Tree(&b, nil, "modules/", Palette{})
	if b.String() != "(no modules)\n" {
		t.Errorf("got %q", b.String())
	}
}

func TestTreeStylesNameMarkNoteAndTrailer(t *testing.T) {
	rows := []Row{
		{Name: "ambxst", Mark: "❄", Marker: "◉", Color: ColorGreen, Underline: true, Note: "↑", NoteColor: ColorTeal, Trailer: []string{"a", "b"}, Count: "1/2"},
		{Name: "debug", Marker: "⊘", Color: ColorRed, Strike: true},
		{Name: "mod", Marker: "⊕", Color: ColorBlue},
	}
	var b strings.Builder
	Tree(&b, rows, "modules/", Colored)
	got := b.String()
	for _, want := range []string{
		codeGreen + "◉ " + codeUnderline + "ambxst❄" + codeReset + "+" + codeLine + " 1/2" + codeReset,
		" " + codeTeal + "↑" + codeReset,
		"  " + codeLine + "← a, b" + codeReset,
		codeRed + "⊘ " + codeStrike + "debug" + codeReset,
		codeBlue + "⊕ mod",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("tree lacks %q:\n%q", want, got)
		}
	}

	b.Reset()
	Tree(&b, rows, "modules/", Palette{})
	if strings.Contains(b.String(), "\x1b") {
		t.Errorf("zero palette emitted escapes: %q", b.String())
	}
}

func TestFlatSortsByPath(t *testing.T) {
	rows := []Row{
		{Category: []string{"b"}, Name: "x", Marker: "○"},
		{Name: "z", Marker: "○"},
		{Category: []string{"a"}, Name: "y", Marker: "○", Trailer: []string{"p"}},
	}
	var b strings.Builder
	Flat(&b, rows, Palette{})
	want := "○ y  ← p\n○ x\n○ z\n"
	if b.String() != want {
		t.Errorf("got %q, want %q", b.String(), want)
	}
	b.Reset()
	Flat(&b, nil, Palette{})
	if b.String() != "(no modules)\n" {
		t.Errorf("empty flat = %q", b.String())
	}
}

func TestPlainAndJSON(t *testing.T) {
	var b strings.Builder
	Plain(&b, [][]string{{"a", "b", ""}, {"c", "d", "e"}})
	if want := "a\tb\t\nc\td\te\n"; b.String() != want {
		t.Errorf("plain = %q, want %q", b.String(), want)
	}

	b.Reset()
	if err := JSON(&b, []string{"x"}); err != nil {
		t.Fatal(err)
	}
	if want := "[\n  \"x\"\n]\n"; b.String() != want {
		t.Errorf("json = %q, want %q", b.String(), want)
	}

	if err := ErrJSONConflict("--raw"); err.Error() != "--json cannot be combined with --raw" {
		t.Errorf("err = %v", err)
	}
}

func TestRenderViewAlignsLabels(t *testing.T) {
	v := View{
		Marker: "◉", Name: "ambxst", Note: "↑",
		Lines: []Line{
			{Label: "source", Value: "github:o/r"},
			{Label: "version", Children: []Line{
				{Label: "current", Value: "1.0"},
				{Label: "latest", Value: "2.0", Dim: "  +3 commits"},
			}},
			{Label: "pulls in", Children: []Line{{Value: "a, b"}}},
		},
	}
	var b strings.Builder
	RenderView(&b, v, Palette{})
	want := `◉ ambxst ↑
├── source    github:o/r
├── version
│   ├── current  1.0
│   └── latest   2.0  +3 commits
└── pulls in
    └── a, b
`
	if b.String() != want {
		t.Errorf("got:\n%s\nwant:\n%s", b.String(), want)
	}
}

func TestIsYes(t *testing.T) {
	for reply, want := range map[string]bool{"y": true, "Y": true, "yes": false, "n": false, "": false} {
		if got := IsYes(reply); got != want {
			t.Errorf("IsYes(%q) = %v, want %v", reply, got, want)
		}
	}
}

func TestLogo(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "out")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	plain := Logo(f)
	if strings.Contains(plain, "\x1b") || !strings.HasPrefix(plain, "\n██╗") || !strings.HasSuffix(plain, "made by me <3 (luar)\n\n") {
		t.Errorf("plain logo = %q", plain)
	}
	gradient := gradientLogo()
	if !strings.Contains(gradient, "\x1b[38;2;204;243;145m██╗") || !strings.Contains(gradient, "\x1b[38;2;181;166;250m╚══") {
		t.Errorf("gradient endpoints missing: %q", gradient)
	}
	if got := Italic(f, "bye"); got != "bye" {
		t.Errorf("Italic on a non-terminal = %q", got)
	}
}
