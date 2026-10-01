package flake

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/DeprecatedLuar/luxos/internal/modules"
	"github.com/DeprecatedLuar/luxos/internal/nix"
)

type fakeUpstream struct {
	tips    map[string]string
	tip     string
	tipErr  error
	tags    map[string]string
	tagsErr error
	ahead   int
	shas    []string
	cmpErr  error
}

func (f fakeUpstream) Tip(ref nix.LockRef) (string, error) {
	if f.tips != nil {
		return f.tips[ref.Repo], f.tipErr
	}
	return f.tip, f.tipErr
}
func (f fakeUpstream) Tags(nix.LockRef) (map[string]string, error) { return f.tags, f.tagsErr }
func (f fakeUpstream) Compare(nix.LockRef, string, string) (int, []string, error) {
	return f.ahead, f.shas, f.cmpErr
}

const (
	lockedRev = "1e9592a000000000000000000000000000000000"
	tipRevSHA = "2a704c4300000000000000000000000000000000"
	tag138    = "c62a7ac000000000000000000000000000000000"
	midRev    = "3333333000000000000000000000000000000000"
)

func node(rev string) nix.LockNode {
	return nix.LockNode{
		Original: nix.LockRef{Type: "github", Owner: "o", Repo: "r"},
		Locked:   nix.LockRef{Type: "github", Owner: "o", Repo: "r", Rev: rev},
	}
}

func TestNotesUnsupportedIsUnknown(t *testing.T) {
	lock := nix.Lock{
		Nodes: map[string]nix.LockNode{
			nix.LockRootNode: {Inputs: map[string]string{"a": "a", "flake-file": "ff"}},
			"a":              {Original: nix.LockRef{Type: "path"}, Locked: nix.LockRef{Type: "path", Rev: "abc"}},
			"ff":             {Original: nix.LockRef{Type: "path"}, Locked: nix.LockRef{Rev: "abc"}},
		},
	}
	notes := Notes(GitHub{}, lock)
	if notes["a"] != NoteUnknown {
		t.Errorf("a = %q, want %q", notes["a"], NoteUnknown)
	}
	if _, ok := notes["ff"]; ok {
		t.Error("flake-file must not be checked")
	}
}

func TestNotesBehindCurrentAndTransitive(t *testing.T) {
	lock := nix.Lock{
		Nodes: map[string]nix.LockNode{
			nix.LockRootNode: {Inputs: map[string]string{"old": "old", "new": "new"}},
			"old":            {Inputs: map[string]string{"dep": "dep"}, Original: nix.LockRef{Type: "github", Repo: "old"}, Locked: nix.LockRef{Rev: "r1"}},
			"new":            {Original: nix.LockRef{Type: "github", Repo: "new"}, Locked: nix.LockRef{Rev: "r2"}},
			"dep":            {Original: nix.LockRef{Type: "github", Repo: "dep"}, Locked: nix.LockRef{Rev: "r3"}},
			"unlocked":       {},
		},
	}
	up := fakeUpstream{tips: map[string]string{"old": "other", "new": "r2", "dep": "r3"}}
	got := Notes(up, lock)
	want := map[string]Note{"old": NoteBehind}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("notes = %v, want %v", got, want)
	}
}

func TestVersionBehindWithTagAndCommits(t *testing.T) {
	up := fakeUpstream{
		tip:   tipRevSHA,
		tags:  map[string]string{lockedRev: "1.3.4", tag138: "1.3.8"},
		ahead: 55,
		shas:  []string{"x", tag138, midRev, tipRevSHA},
	}
	got := Version(up, node(lockedRev))
	want := VersionInfo{Current: "1.3.4", Latest: "1.3.8", Ahead: 55, HasAhead: true, Checked: true, Note: NoteBehind}
	if got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestVersionUntagged(t *testing.T) {
	up := fakeUpstream{tip: tipRevSHA, ahead: 7853, shas: []string{midRev, tipRevSHA}}
	got := Version(up, node(lockedRev))
	want := VersionInfo{Current: "1e9592a", Latest: "2a704c4", Ahead: 7853, HasAhead: true, Checked: true, Note: NoteBehind}
	if got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestVersionUpToDate(t *testing.T) {
	got := Version(fakeUpstream{tip: lockedRev}, node(lockedRev))
	want := VersionInfo{Current: "1e9592a", Checked: true}
	if got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestVersionFailuresAreUnknown(t *testing.T) {
	boom := errors.New("boom")

	got := Version(fakeUpstream{tipErr: boom}, node(lockedRev))
	if want := (VersionInfo{Current: "1e9592a", Latest: string(NoteUnknown), Checked: true, Note: NoteUnknown}); got != want {
		t.Errorf("tip failure: got %+v, want %+v", got, want)
	}

	got = Version(fakeUpstream{tip: tipRevSHA, cmpErr: boom}, node(lockedRev))
	if want := (VersionInfo{Current: "1e9592a", Latest: string(NoteUnknown), Checked: true, Note: NoteBehind}); got != want {
		t.Errorf("compare failure: got %+v, want %+v", got, want)
	}

	got = Version(fakeUpstream{tip: tipRevSHA, tagsErr: boom, ahead: 3, shas: []string{tipRevSHA}}, node(lockedRev))
	if want := (VersionInfo{Current: "1e9592a", Latest: string(NoteUnknown), Ahead: 3, HasAhead: true, Checked: true, Note: NoteBehind}); got != want {
		t.Errorf("tags failure: got %+v, want %+v", got, want)
	}
}

func TestVersionUnsupportedCompareUsesTagOnTip(t *testing.T) {
	up := fakeUpstream{tip: tipRevSHA, tags: map[string]string{tipRevSHA: "v2"}, cmpErr: ErrUnsupported}
	got := Version(up, node(lockedRev))
	want := VersionInfo{Current: "1e9592a", Latest: "v2", Checked: true, Note: NoteBehind}
	if got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestVersionOffline(t *testing.T) {
	got := Version(nil, node(lockedRev))
	if want := (VersionInfo{Current: "1e9592a"}); got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestStatusOf(t *testing.T) {
	cases := []struct {
		locked, online bool
		note           Note
		want           Status
	}{
		{false, true, "", StatusUnknown},
		{true, false, "", StatusUnknown},
		{true, true, NoteUnknown, StatusUnknown},
		{true, true, NoteBehind, StatusBehind},
		{true, true, "", StatusCurrent},
	}
	for _, c := range cases {
		if got := StatusOf(c.locked, c.online, c.note); got != c.want {
			t.Errorf("StatusOf(%v, %v, %q) = %q, want %q", c.locked, c.online, c.note, got, c.want)
		}
	}
}

func TestNestedPaths(t *testing.T) {
	lock := nix.Lock{
		Nodes: map[string]nix.LockNode{
			nix.LockRootNode: {Inputs: map[string]string{"a": "a", "b": "b", "flake-file": "ff"}},
			"a":              {Inputs: map[string]string{"nixpkgs": "np1"}},
			"b":              {Inputs: map[string]string{"nixpkgs": "np2", "x": "x"}},
			"x":              {Inputs: map[string]string{"nixpkgs": "np3"}},
			"ff":             {Inputs: map[string]string{"nixpkgs": "np4"}},
		},
	}
	got := NestedPaths(lock, "nixpkgs")
	want := []string{"a/nixpkgs", "b/nixpkgs", "b/x/nixpkgs"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("NestedPaths = %v, want %v", got, want)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestDeclarations(t *testing.T) {
	if _, err := exec.LookPath("nix-instantiate"); err != nil {
		t.Skip("nix-instantiate not on PATH")
	}
	root := t.TempDir()
	modulesDir := filepath.Join(root, "modules")
	hostDir := filepath.Join(root, ".local", "machines", "box")
	writeFile(t, filepath.Join(modulesDir, "apps", "shell.nix"), "{ ... }: {\n  flake-file.inputs.ambxst.url = \"github:o/ambxst\";\n}\n")
	writeFile(t, filepath.Join(hostDir, "modules", "vpn.nix"), "{ ... }: {\n  flake-file.inputs.wg.url = \"github:o/wg\";\n}\n")
	writeFile(t, filepath.Join(hostDir, "machine.nix"), "{\n  flake-file.inputs.nixpkgs.url = \"github:NixOS/nixpkgs/nixos-25.11\";\n}\n")
	writeFile(t, filepath.Join(hostDir, "modules.nix"), "{ ... }:\n{\n  imports = [\n    ./apps/shell.nix\n    ./local/vpn.nix\n  ];\n}\n")

	h, err := modules.Load(modulesDir, hostDir)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Declarations(h)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string][]Decl{
		"ambxst":  {{Unit: "apps/shell.nix", File: "modules/apps/shell.nix", Line: 2, URL: "github:o/ambxst"}},
		"wg":      {{Unit: "local/vpn.nix", File: "local/modules/vpn.nix", Line: 2, URL: "github:o/wg"}},
		"nixpkgs": {{File: ".local/machines/box/machine.nix", Line: 2, URL: "github:NixOS/nixpkgs/nixos-25.11"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Declarations = %+v, want %+v", got, want)
	}
	units := Units(got)
	if !reflect.DeepEqual(units["ambxst"], []string{"apps/shell.nix"}) || len(units["nixpkgs"]) != 0 {
		t.Errorf("Units = %v", units)
	}
}
