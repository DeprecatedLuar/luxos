package commands

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DeprecatedLuar/luxos/internal/config"
)

func TestBootGuard(t *testing.T) {
	stdin, err := os.CreateTemp(t.TempDir(), "stdin")
	if err != nil {
		t.Fatal(err)
	}
	defer func(f *os.File) { os.Stdin = f }(os.Stdin)
	os.Stdin = stdin

	p := twoHostScopeFixture(t)
	hostDir := filepath.Join(p.Hosts, "host1")
	machineSel := config.SelectionPath(filepath.Join(p.Machines, "k1"))

	// No modules/machine link: nothing to ask about.
	unlinked := p
	unlinked.Modules = t.TempDir()
	if answered, err := bootGuard(unlinked, hostDir, false); answered || err != nil {
		t.Fatalf("no link: answered=%v err=%v", answered, err)
	}

	// Linked machine selecting ./boot.nix: no prompt.
	if answered, err := bootGuard(p, hostDir, false); answered || err != nil {
		t.Fatalf("boot selected: answered=%v err=%v", answered, err)
	}

	// Machine selection selects ./boot.nix but the file is missing: no prompt.
	bootFile := filepath.Join(p.Machines, "k1", config.ScopeModulesDir, config.BootFile)
	bootSrc := mustReadFile(t, bootFile)
	if err := os.Remove(bootFile); err != nil {
		t.Fatal(err)
	}
	if answered, err := bootGuard(p, hostDir, false); answered || err != nil {
		t.Fatalf("boot file missing: answered=%v err=%v", answered, err)
	}
	write(t, bootFile, bootSrc)

	// Machine selection empty, host selection holds the pre-heal line: no prompt.
	write(t, machineSel, emptyTestSelection)
	write(t, config.SelectionPath(hostDir), selectionWith("../machine/boot.nix"))
	if answered, err := bootGuard(p, hostDir, false); answered || err != nil {
		t.Fatalf("cross-scope line: answered=%v err=%v", answered, err)
	}

	// Nothing selects it: no answer at end of input is no.
	write(t, config.SelectionPath(hostDir), emptyTestSelection)
	_, err = bootGuard(p, hostDir, false)
	if err == nil || !strings.Contains(err.Error(), "--yes") {
		t.Fatalf("boot not selected, no answer: err = %v, want one naming --yes", err)
	}

	// --yes answers it.
	if answered, err := bootGuard(p, hostDir, true); answered || err != nil {
		t.Fatalf("boot not selected, yes: answered=%v err=%v", answered, err)
	}
}
