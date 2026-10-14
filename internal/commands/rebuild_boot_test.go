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
	machineSel := config.SelectionPath(filepath.Join(p.Machines, "k1"))

	// No modules/machine link: nothing to ask about.
	unlinked := t.TempDir()
	if answered, err := bootGuard(unlinked, false); answered || err != nil {
		t.Fatalf("no link: answered=%v err=%v", answered, err)
	}

	// Linked machine selecting ./boot.nix: no prompt.
	if answered, err := bootGuard(p.Modules, false); answered || err != nil {
		t.Fatalf("boot selected: answered=%v err=%v", answered, err)
	}

	// Linked machine without it: no answer at end of input is no.
	write(t, machineSel, emptyTestSelection)
	_, err = bootGuard(p.Modules, false)
	if err == nil || !strings.Contains(err.Error(), "--yes") {
		t.Fatalf("boot not selected, no answer: err = %v, want one naming --yes", err)
	}

	// --yes answers it.
	if answered, err := bootGuard(p.Modules, true); answered || err != nil {
		t.Fatalf("boot not selected, yes: answered=%v err=%v", answered, err)
	}
}
