package commands

import "testing"

func TestModuleMarker(t *testing.T) {
	cases := []struct {
		enabled, running bool
		want             string
	}{
		{true, true, markerEnabledBoth},
		{true, false, markerEnabledOnly},
		{false, true, markerRunningOnly},
		{false, false, markerNeither},
	}
	for _, c := range cases {
		if got := moduleMarker(c.enabled, c.running); got != c.want {
			t.Errorf("moduleMarker(%v, %v) = %q, want %q", c.enabled, c.running, got, c.want)
		}
	}
}

func TestModuleMarkerRank(t *testing.T) {
	cases := []struct {
		marker string
		want   int
	}{
		{markerEnabledOnly, 0},
		{markerEnabledBoth, 1},
		{markerRunningOnly, 2},
		{markerPulled, 3},
		{markerNeither, 4},
	}
	for _, c := range cases {
		if got := moduleMarkerRank(c.marker); got != c.want {
			t.Errorf("moduleMarkerRank(%q) = %d, want %d", c.marker, got, c.want)
		}
	}
}

func TestModuleSortRows(t *testing.T) {
	rows := []moduleRow{
		{name: "zeta", marker: markerNeither, rank: 4},
		{name: "delta", marker: markerPulled, rank: 3},
		{name: "alpha", marker: markerEnabledOnly, rank: 0},
		{name: "beta", marker: markerEnabledOnly, rank: 0},
		{name: "gamma", marker: markerEnabledBoth, rank: 1},
	}
	moduleSortRows(rows)

	want := []string{"alpha", "beta", "gamma", "delta", "zeta"}
	for i, name := range want {
		if rows[i].name != name {
			t.Fatalf("rows[%d].name = %q, want %q (order: %+v)", i, rows[i].name, name, rows)
		}
	}
}

func TestModuleSortRows_RankBeforeName(t *testing.T) {
	// A lower-ranked "zzz" must sort before a higher-ranked "aaa".
	rows := []moduleRow{
		{name: "aaa", rank: 3},
		{name: "zzz", rank: 0},
	}
	moduleSortRows(rows)
	if rows[0].name != "zzz" || rows[1].name != "aaa" {
		t.Fatalf("got order %+v, want zzz before aaa", rows)
	}
}

func TestUserTarget(t *testing.T) {
	cases := []struct{ in, want string }{
		{"foo", "users/foo"},
		{"users/foo", "users/foo"},
	}
	for _, c := range cases {
		if got := userTarget(c.in); got != c.want {
			t.Errorf("userTarget(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestUserCategory(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", "users"},
		{"desktop", "users/desktop"},
	}
	for _, c := range cases {
		if got := userCategory(c.in); got != c.want {
			t.Errorf("userCategory(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestIsFrameworkPath(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{"system", true},
		{"system/desktop", true},
		{"users/luar", false},
		{"misc/foo", false},
	}
	for _, c := range cases {
		if got := isFrameworkPath(c.path); got != c.want {
			t.Errorf("isFrameworkPath(%q) = %v, want %v", c.path, got, c.want)
		}
	}
}

func TestJoinCategory(t *testing.T) {
	cases := []struct {
		category, base, want string
	}{
		{"", "foo", "foo"},
		{"misc", "foo", "misc/foo"},
		{"users", "foo.nix", "users/foo.nix"},
	}
	for _, c := range cases {
		if got := joinCategory(c.category, c.base); got != c.want {
			t.Errorf("joinCategory(%q, %q) = %q, want %q", c.category, c.base, got, c.want)
		}
	}
}
