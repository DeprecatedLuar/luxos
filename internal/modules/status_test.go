package modules

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// statusFixture builds a host selecting the named modules (paths relative to
// modules/), with every path in files written as an empty module.
func statusFixture(t *testing.T, selected []string, files ...string) *Host {
	t.Helper()
	root := t.TempDir()
	modulesDir := filepath.Join(root, "modules")
	hostDir := filepath.Join(root, ".local", "machines", "box")
	for _, f := range files {
		mustWriteFile(t, filepath.Join(modulesDir, f), emptyModule)
	}
	var items []string
	for _, s := range selected {
		items = append(items, "./"+s)
	}
	mustWriteFile(t, filepath.Join(hostDir, "modules.nix"), selectionOf(items...))
	h, err := Load(modulesDir, hostDir)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func statesOf(sts []Status) map[string]State {
	out := map[string]State{}
	for _, s := range sts {
		out[s.Module.Name] = s.State
	}
	return out
}

func TestStatusOfStates(t *testing.T) {
	skipIfNoNix(t)
	h := statusFixture(t, []string{"a.nix", "b.nix"}, "a.nix", "b.nix", "c.nix", "d.nix")
	sts, err := StatusOf(h, []string{"a.nix", "c.nix"}, "")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]State{"a": Active, "b": Staged, "c": Leftover, "d": Off}
	if got := statesOf(sts); !reflect.DeepEqual(got, want) {
		t.Errorf("states = %v, want %v", got, want)
	}
}

func TestStatusOfPulledOverridesLeftover(t *testing.T) {
	skipIfNoNix(t)
	h := statusFixture(t, []string{"a.nix"}, "b.nix", "c.nix")
	mustWriteFile(t, filepath.Join(h.ModulesDir, "a.nix"), "{ luxos, ... }:\n{\n  imports = luxos.modules [ \"b\" \"c\" ];\n}\n")
	h, err := Load(h.ModulesDir, h.HostDir)
	if err != nil {
		t.Fatal(err)
	}
	sts, err := StatusOf(h, []string{"b.nix"}, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range sts {
		if s.Module.Name == "b" || s.Module.Name == "c" {
			if s.State != Pulled || !reflect.DeepEqual(s.PulledBy, []string{"a"}) {
				t.Errorf("%s = %+v, want pulled by a", s.Module.Name, s)
			}
		}
	}
}

func TestStatusOfModifiedNeedsEnabledAndRunning(t *testing.T) {
	skipIfNoNix(t)
	h := statusFixture(t, []string{"a.nix", "b.nix"}, "a.nix", "b.nix", "c.nix")
	baseline := t.TempDir()
	for _, f := range []string{"a.nix", "b.nix", "c.nix"} {
		mustWriteFile(t, filepath.Join(baseline, f), "different")
	}
	sts, err := StatusOf(h, []string{"a.nix", "c.nix"}, baseline)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]State{"a": Modified, "b": Staged, "c": Leftover}
	if got := statesOf(sts); !reflect.DeepEqual(got, want) {
		t.Errorf("states = %v, want %v", got, want)
	}
}

func TestStatusOfRemoved(t *testing.T) {
	skipIfNoNix(t)
	h := statusFixture(t, nil, "alive.nix")
	running := []string{
		"local/debug.nix", "desktop/shells/ambxst", "gone.nix", "alive.nix",
		"x/dup.nix", "y/dup.nix", "desktop/apps/foo/default.nix",
	}
	sts, err := StatusOf(h, running, "")
	if err != nil {
		t.Fatal(err)
	}
	removed := map[string]string{}
	for _, s := range sts {
		if s.State == Removed {
			removed[s.Module.Name] = s.Module.Path
		}
	}
	want := map[string]string{
		"debug": "local/debug.nix", "ambxst": "desktop/shells/ambxst", "gone": "gone.nix",
		"dup": "x/dup.nix", "foo": "desktop/apps/foo",
	}
	if !reflect.DeepEqual(removed, want) {
		t.Errorf("removed = %v, want %v", removed, want)
	}
	if got := statesOf(sts)["alive"]; got != Leftover {
		t.Errorf("alive = %v, want leftover: an existing module gets no removed row", got)
	}
}

func TestStatusOfInputs(t *testing.T) {
	skipIfNoNix(t)
	h := statusFixture(t, nil, "none.nix")
	mustWriteFile(t, filepath.Join(h.ModulesDir, "file.nix"), "{ ... }: {\n  flake-file.inputs.zed.url = \"github:o/zed\";\n  flake-file.inputs.abc.url = \"github:o/abc\";\n}\n")
	mustWriteFile(t, filepath.Join(h.ModulesDir, "folder", "default.nix"), emptyModule)
	mustWriteFile(t, filepath.Join(h.ModulesDir, "folder", "sub", "inner.nix"), "{ ... }: {\n  flake-file.inputs.deep.url = \"github:o/deep\";\n}\n")
	h, err := Load(h.ModulesDir, h.HostDir)
	if err != nil {
		t.Fatal(err)
	}
	sts, err := StatusOf(h, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, s := range sts {
		got[s.Module.Name] = strings.Join(s.Inputs, ",")
	}
	want := map[string]string{"file": "abc,zed", "folder": "deep", "none": ""}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("inputs = %v, want %v", got, want)
	}
}

func TestChangedFrom(t *testing.T) {
	write := func(root, rel, data string) {
		t.Helper()
		mustWriteFile(t, filepath.Join(root, rel), data)
	}
	mods, staged := t.TempDir(), t.TempDir()
	check := func(name string, m Module, baseline string, want bool) {
		t.Helper()
		m.Abs = filepath.Join(mods, m.Path)
		got, err := m.changedFrom(baseline)
		if err != nil || got != want {
			t.Errorf("%s: changed=%v err=%v, want %v", name, got, err, want)
		}
	}

	file := Module{Name: "f", Path: "cat/f.nix"}
	write(mods, "cat/f.nix", "a")
	write(staged, "cat/f.nix", "a")
	check("equal file", file, staged, false)
	write(mods, "cat/f.nix", "b")
	check("changed file", file, staged, true)
	check("missing staged", file, t.TempDir(), true)

	dir := Module{Name: "d", Path: "d"}
	write(mods, "d/default.nix", "x")
	write(staged, "d/default.nix", "x")
	check("equal dir", dir, staged, false)
	write(mods, "d/default.nix", "y")
	check("dir changed file", dir, staged, true)
	write(mods, "d/default.nix", "x")
	write(mods, "d/extra.nix", "e")
	check("dir added file", dir, staged, true)
	if err := os.Remove(filepath.Join(mods, "d/extra.nix")); err != nil {
		t.Fatal(err)
	}
	write(staged, "d/gone.nix", "g")
	check("dir removed file", dir, staged, true)

	bundle := Module{Name: "b", Path: "b"}
	write(mods, "b/default.nix", "x")
	write(mods, "b/modules/sub.nix", "s1")
	write(staged, "b/default.nix", "x")
	write(staged, "b/modules/sub.nix", "s1")
	check("bundle equal", bundle, staged, false)
	write(mods, "b/modules/sub.nix", "s2")
	check("bundle ignores submodule change", bundle, staged, false)
	write(mods, "b/default.nix", "y")
	check("bundle plumbing change", bundle, staged, true)

	shadow := Module{Name: "s", Path: "local/s.nix", Shadows: "cat2/s.nix"}
	write(mods, "local/s.nix", "s")
	write(staged, "cat2/s.nix", "s")
	check("shadow", shadow, staged, false)

	real := filepath.Join(t.TempDir(), "real.nix")
	if err := os.WriteFile(real, []byte("l"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, filepath.Join(mods, "link.nix")); err != nil {
		t.Fatal(err)
	}
	write(staged, "link.nix", "l")
	check("symlink", Module{Name: "link", Path: "link.nix"}, staged, false)
}

func TestStatusOfBroken(t *testing.T) {
	skipIfNoNix(t)
	h := statusFixture(t, nil, "fine.nix")
	mustWriteFile(t, filepath.Join(h.ModulesDir, "flaky.nix"), "{ ... }: {\n  flake-file.inputs.zed.url = \"github:o/zed\";\n}\n")
	mustWriteFile(t, filepath.Join(h.ModulesDir, "folder", "default.nix"), emptyModule)
	mustWriteFile(t, filepath.Join(h.ModulesDir, "folder", "sub", "inner.nix"), "{ x = ; }\n")
	h, err := Load(h.ModulesDir, h.HostDir)
	if err != nil {
		t.Fatal(err)
	}
	sts, err := StatusOf(h, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	broken := map[string]bool{}
	inputs := map[string]string{}
	for _, s := range sts {
		broken[s.Module.Name] = s.Broken
		inputs[s.Module.Name] = strings.Join(s.Inputs, ",")
	}
	if want := map[string]bool{"fine": false, "flaky": false, "folder": true}; !reflect.DeepEqual(broken, want) {
		t.Errorf("broken = %v, want %v", broken, want)
	}
	if inputs["flaky"] != "zed" {
		t.Errorf("flaky inputs = %q, want zed despite a broken neighbour", inputs["flaky"])
	}
}
