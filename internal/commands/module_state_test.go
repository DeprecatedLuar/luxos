package commands

import (
	"path/filepath"
	"testing"

	"github.com/DeprecatedLuar/luxos/internal/config"
	"github.com/DeprecatedLuar/luxos/internal/paths"
)

const laptopSelection = "../devices/laptop.nix"

func laptopFixture(t *testing.T) paths.Paths {
	t.Helper()
	p := twoHostScopeFixture(t)
	write(t, filepath.Join(p.Modules, "devices", "laptop.nix"), "{ }\n")
	return p
}

func selectHost(t *testing.T, p paths.Paths, lines ...string) {
	t.Helper()
	write(t, config.SelectionPath(filepath.Join(p.Hosts, "host1")), selectionWith(lines...))
}

func runHost(t *testing.T, p paths.Paths, lines ...string) {
	t.Helper()
	write(t, filepath.Join(p.RunningSelections, config.ScopeHost, "default.nix"), selectionWith(lines...))
}

func wantOutput(t *testing.T, got, want string) {
	t.Helper()
	if got != want {
		t.Errorf("output = %q, want %q", got, want)
	}
}

func TestEnableShowsStagedGlyph(t *testing.T) {
	skipIfNoNix(t)
	p := laptopFixture(t)
	out := captureStdout(t, func() {
		if err := moduleEnable(p, []string{"laptop"}); err != nil {
			t.Fatal(err)
		}
	})
	wantOutput(t, out, markerEnabledOnly+" laptop\n")
}

func TestEnableAgainShowsSameGlyph(t *testing.T) {
	skipIfNoNix(t)
	p := laptopFixture(t)
	selectHost(t, p, laptopSelection)
	out := captureStdout(t, func() {
		if err := moduleEnable(p, []string{"laptop"}); err != nil {
			t.Fatal(err)
		}
	})
	wantOutput(t, out, markerEnabledOnly+" laptop\n")
}

func TestDisableRunningShowsLeftoverGlyph(t *testing.T) {
	skipIfNoNix(t)
	p := laptopFixture(t)
	selectHost(t, p, laptopSelection)
	runHost(t, p, laptopSelection)
	out := captureStdout(t, func() {
		if err := moduleDisable(p, []string{"laptop", "-y"}); err != nil {
			t.Fatal(err)
		}
	})
	wantOutput(t, out, markerRunningOnly+" laptop\n")
}

func TestDisablePulledShowsPulledGlyph(t *testing.T) {
	skipIfNoNix(t)
	p := laptopFixture(t)
	write(t, filepath.Join(p.Modules, "a.nix"), `{ luxos, ... }: { imports = luxos.modules [ "b" ]; }`+"\n")
	write(t, filepath.Join(p.Modules, "b.nix"), "{ }\n")
	selectHost(t, p, "../a.nix", "../b.nix")
	out := captureStdout(t, func() {
		if err := moduleDisable(p, []string{"b", "-y"}); err != nil {
			t.Fatal(err)
		}
	})
	wantOutput(t, out, markerPulled+" b\n")
}

func TestEnableSubmoduleShowsBundleThenItself(t *testing.T) {
	skipIfNoNix(t)
	p := bundleFixture(t)
	out := captureStdout(t, func() {
		if err := toggleModules(p, []string{subName}, true, false); err != nil {
			t.Fatal(err)
		}
	})
	wantOutput(t, out, markerEnabledOnly+" "+bundleName+"\n"+markerEnabledOnly+" "+subName+"\n")
}

func TestDisableBundleShowsSubmodulesThenBundle(t *testing.T) {
	skipIfNoNix(t)
	p := bundleFixture(t)
	if err := toggleModules(p, []string{subName}, true, false); err != nil {
		t.Fatal(err)
	}
	out := captureStdout(t, func() {
		if err := toggleModules(p, []string{bundleName}, false, false); err != nil {
			t.Fatal(err)
		}
	})
	wantOutput(t, out, markerNeither+" "+subName+"\n"+markerNeither+" "+bundleName+"\n")
}
