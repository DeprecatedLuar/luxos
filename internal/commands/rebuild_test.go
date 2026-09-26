package commands

import "testing"

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
