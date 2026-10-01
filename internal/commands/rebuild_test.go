package commands

import (
	"os"
	"testing"
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
