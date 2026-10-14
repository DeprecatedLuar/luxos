package config

import "testing"

func TestLineOfPathOf(t *testing.T) {
	cases := []struct{ scope, path, line string }{
		{ScopeShared, "users/luar", "./users/luar"},
		{ScopeHost, "host/packages.nix", "./packages.nix"},
		{ScopeHost, "users/luar", "../users/luar"},
		{ScopeHost, "machine/boot.nix", "../machine/boot.nix"},
		{ScopeMachine, "machine/eduardo/modules/git.nix", "./eduardo/modules/git.nix"},
		{ScopeMachine, "devices/laptop", "../devices/laptop"},
	}
	for _, c := range cases {
		if got := LineOf(c.scope, c.path); got != c.line {
			t.Errorf("LineOf(%q, %q) = %q, want %q", c.scope, c.path, got, c.line)
		}
		if got := PathOf(c.scope, c.line); got != c.path {
			t.Errorf("PathOf(%q, %q) = %q, want %q", c.scope, c.line, got, c.path)
		}
	}
}
