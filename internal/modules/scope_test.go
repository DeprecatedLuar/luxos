package modules

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/DeprecatedLuar/luxos/internal/config"
)

// planFixture is a shared tree with apps/foo.nix selected by host h1 and
// referenced by apps/bar, plus a machine folder k1 selecting boot.
func planFixture(t *testing.T) (h *Host, hostsDir, machinesDir string) {
	t.Helper()
	root := t.TempDir()
	modulesDir := filepath.Join(root, "modules")
	hostsDir = filepath.Join(root, "hosts")
	machinesDir = filepath.Join(root, "machines")
	hostDir := filepath.Join(hostsDir, "h1")
	machineDir := filepath.Join(machinesDir, "k1")
	mustWriteFile(t, filepath.Join(modulesDir, "apps", "foo.nix"), "{ }\n")
	mustWriteFile(t, filepath.Join(modulesDir, "apps", "bar", "default.nix"), "{ luxos, ... }: { imports = luxos.modules [ \"foo\" ]; }\n")
	mustWriteFile(t, filepath.Join(modulesDir, "system", "core.nix"), "{ }\n")
	mustWriteFile(t, config.SelectionPath(hostDir), "{ ... }:\n{\n  imports = [\n    ../apps/foo.nix\n  ];\n}\n")
	mustWriteFile(t, filepath.Join(machineDir, "modules", "boot.nix"), "{ }\n")
	mustWriteFile(t, config.SelectionPath(machineDir), "{ ... }:\n{\n  imports = [\n    ./boot.nix\n  ];\n}\n")
	h, err := Load(modulesDir, hostDir, machineDir)
	if err != nil {
		t.Fatal(err)
	}
	return h, hostsDir, machinesDir
}

func TestPlanRemoveWritesNothing(t *testing.T) {
	skipIfNoNix(t)
	h, hostsDir, machinesDir := planFixture(t)
	before := mustReadFile(t, h.Selections[0].File)

	p, err := PlanRemove(h, hostsDir, machinesDir, "foo")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{h.Selections[0].File}; !reflect.DeepEqual(p.Importers, want) {
		t.Errorf("Importers = %v, want %v", p.Importers, want)
	}
	if want := []string{filepath.Join(h.ModulesDir, "apps", "bar", "default.nix")}; !reflect.DeepEqual(p.Dependents, want) {
		t.Errorf("Dependents = %v, want %v", p.Dependents, want)
	}
	if got := mustReadFile(t, h.Selections[0].File); got != before {
		t.Errorf("selection changed by a plan:\n%s", got)
	}
	if _, err := os.Stat(filepath.Join(h.ModulesDir, "apps", "foo.nix")); err != nil {
		t.Errorf("unit gone after a plan: %v", err)
	}
}

func TestPlanRefusesFrameworkAndBoot(t *testing.T) {
	h, hostsDir, machinesDir := planFixture(t)
	if _, err := PlanRemove(h, hostsDir, machinesDir, "core"); err == nil || !strings.Contains(err.Error(), "framework") {
		t.Errorf("PlanRemove(core) err = %v, want a framework refusal", err)
	}
	if _, err := PlanRename(h, hostsDir, machinesDir, "boot", "loader"); err != errBootUnit {
		t.Errorf("PlanRename(boot) err = %v, want errBootUnit", err)
	}
}

func TestPlanFailsOnUnrecognizableSelection(t *testing.T) {
	skipIfNoNix(t)
	h, hostsDir, machinesDir := planFixture(t)
	other := config.SelectionPath(filepath.Join(hostsDir, "h2"))
	mustWriteFile(t, other, "{ ... }:\n{ imports = [ ../apps/foo.nix ]; imports2 = [ ]; } // { imports = [ ]; }\n")

	_, err := PlanRemove(h, hostsDir, machinesDir, "foo")
	if err == nil || !strings.Contains(err.Error(), other) {
		t.Fatalf("PlanRemove err = %v, want one naming %s", err, other)
	}
}

func TestPlanRenameDerivesPathAndRefusesTakenPath(t *testing.T) {
	skipIfNoNix(t)
	h, hostsDir, machinesDir := planFixture(t)
	p, err := PlanRename(h, hostsDir, machinesDir, "foo", "baz")
	if err != nil {
		t.Fatal(err)
	}
	if p.NewName != "baz" || p.NewPath != "apps/baz.nix" {
		t.Errorf("NewName, NewPath = %q, %q", p.NewName, p.NewPath)
	}

	mustWriteFile(t, filepath.Join(h.ModulesDir, "apps", "qux.nix", "stray"), "")
	if _, err := PlanRename(h, hostsDir, machinesDir, "foo", "qux"); err == nil || !strings.Contains(err.Error(), "apps/qux.nix") {
		t.Errorf("PlanRename onto a taken path err = %v", err)
	}
}

func TestApplyRemoveAndRename(t *testing.T) {
	skipIfNoNix(t)
	h, hostsDir, machinesDir := planFixture(t)
	bar := filepath.Join(h.ModulesDir, "apps", "bar", "default.nix")

	p, err := PlanRename(h, hostsDir, machinesDir, "foo", "baz")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Apply(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(h.ModulesDir, "apps", "baz.nix")); err != nil {
		t.Errorf("renamed file missing: %v", err)
	}
	if got := mustReadFile(t, bar); !strings.Contains(got, `"baz"`) {
		t.Errorf("reference not rewritten:\n%s", got)
	}
	if got, _ := ReadSelection(h.Selections[0].File, config.ScopeHost); !reflect.DeepEqual(got, []string{"apps/baz.nix"}) {
		t.Errorf("selection = %v", got)
	}

	h, err = Load(h.ModulesDir, h.HostDir, h.MachineDir)
	if err != nil {
		t.Fatal(err)
	}
	p, err = PlanRemove(h, hostsDir, machinesDir, "baz")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Apply(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(h.ModulesDir, "apps", "baz.nix")); !os.IsNotExist(err) {
		t.Errorf("removed file still there: %v", err)
	}
	if got := mustReadFile(t, bar); strings.Contains(got, `"baz"`) {
		t.Errorf("reference kept:\n%s", got)
	}
}

func TestPlanRefusesUnrecognizedDependent(t *testing.T) {
	skipIfNoNix(t)
	h, hostsDir, machinesDir := planFixture(t)
	bad := filepath.Join(h.ModulesDir, "apps", "baz.nix")
	mustWriteFile(t, bad, "{ luxos, ... }: { imports = luxos.modules [ \"foo\" ]; extra = luxos; }\n")

	if _, err := PlanRemove(h, hostsDir, machinesDir, "foo"); err == nil || !strings.Contains(err.Error(), bad) {
		t.Errorf("PlanRemove err = %v, want one naming %s", err, bad)
	}
}

func TestPlanRenameRefusesPathInNewName(t *testing.T) {
	skipIfNoNix(t)
	h, hostsDir, machinesDir := planFixture(t)
	bar := filepath.Join(h.ModulesDir, "apps", "bar", "default.nix")
	beforeBar := mustReadFile(t, bar)
	beforeSel := mustReadFile(t, h.Selections[0].File)

	for _, newName := range []string{"bar/baz", "../../x"} {
		if _, err := PlanRename(h, hostsDir, machinesDir, "foo", newName); err == nil || !strings.Contains(err.Error(), newName) {
			t.Errorf("PlanRename(foo, %s) err = %v, want one naming %s", newName, err, newName)
		}
	}
	if _, err := os.Stat(filepath.Join(h.ModulesDir, "apps", "foo.nix")); err != nil {
		t.Errorf("unit moved: %v", err)
	}
	if got := mustReadFile(t, bar); got != beforeBar {
		t.Errorf("dependent changed:\n%s", got)
	}
	if got := mustReadFile(t, h.Selections[0].File); got != beforeSel {
		t.Errorf("selection changed:\n%s", got)
	}
}
