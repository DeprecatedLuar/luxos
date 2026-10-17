package main

import (
	"slices"
	"testing"
)

func TestStripDeadlock(t *testing.T) {
	tests := []struct {
		name  string
		args  []string
		rest  []string
		found bool
	}{
		{"absent", []string{"rb", "switch"}, []string{"rb", "switch"}, false},
		{"last", []string{"passwd", "ana", "--deadlock"}, []string{"passwd", "ana"}, true},
		{"first", []string{"--deadlock", "rb", "switch"}, []string{"rb", "switch"}, true},
		{"middle", []string{"rb", "--deadlock", "switch"}, []string{"rb", "switch"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rest, found := stripDeadlock(tt.args)
			if found != tt.found || !slices.Equal(rest, tt.rest) {
				t.Errorf("stripDeadlock(%q) = %q, %v; want %q, %v", tt.args, rest, found, tt.rest, tt.found)
			}
		})
	}
}
