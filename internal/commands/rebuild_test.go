package commands

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/DeprecatedLuar/luxos/internal/commands/shared"
	"github.com/DeprecatedLuar/luxos/internal/paths"
)

func TestRebuildAction(t *testing.T) {
	cases := map[string][]string{
		"switch": {"--foo", "switch"},
		"build":  {"build"},
		"":       {"--foo"},
	}
	for want, in := range cases {
		if got := rebuildAction(in); got != want {
			t.Errorf("rebuildAction(%v) = %q, want %q", in, got, want)
		}
	}
	if got := rebuildAction(nil); got != "" {
		t.Errorf("rebuildAction(nil) = %q", got)
	}
}

func TestInterrupted(t *testing.T) {
	sigs := make(chan os.Signal, 1)
	if err := interrupted(sigs); err != nil {
		t.Fatalf("no signal: %v", err)
	}
	sigs <- os.Interrupt
	if err := interrupted(sigs); err == nil {
		t.Fatal("signal received but not reported")
	}
}

func TestWithMachine(t *testing.T) {
	opts := map[string]string{}
	got := withMachine(opts, []string{"switch", "--yes"}, "cathode")
	if want := []string{"switch", "--yes", "--machine", "cathode"}; !reflect.DeepEqual(got, want) {
		t.Errorf("args = %v, want %v", got, want)
	}
	if opts["machine"] != "cathode" {
		t.Errorf("opts[machine] = %q", opts["machine"])
	}
	if esc := escalationArgs(got, false); esc[len(esc)-2] != "--machine" || esc[len(esc)-1] != "cathode" {
		t.Errorf("escalation args lose the machine: %v", esc)
	}
}

func TestRebuild_EmptyConfigWithoutTerminal(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("setup refuses root before checking the terminal")
	}
	t.Setenv(paths.ConfigDirEnv, "") // --config sets it; restored after the test
	cfg := t.TempDir()
	stdin, err := os.CreateTemp(t.TempDir(), "stdin")
	if err != nil {
		t.Fatal(err)
	}
	defer func(f *os.File) { os.Stdin = f }(os.Stdin)
	os.Stdin = stdin

	err = Rebuild([]string{"--config", cfg, "switch"})
	if err == nil || err.Error() != shared.NoConfigError(cfg).Error() {
		t.Errorf("err = %v, want the no-config error", err)
	}

	err = Rebuild([]string{"--config", cfg, "--machine", "ghost", "switch"})
	if err == nil || err.Error() != shared.NoMachineError(cfg, filepath.Join(cfg, ".local/machines"), "ghost").Error() {
		t.Errorf("explicit machine: err = %v, want the no-machine error", err)
	}
}
