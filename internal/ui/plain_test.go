package ui

import (
	"strings"
	"testing"
)

func TestKV(t *testing.T) {
	var w strings.Builder
	KV(&w, [][2]string{{"name", "a"}, {"empty", ""}})
	if got := w.String(); got != "name=a\nempty=\n" {
		t.Errorf("KV = %q", got)
	}
}

func TestJSONOneOrMany(t *testing.T) {
	type obj struct {
		N int `json:"n"`
	}
	var one strings.Builder
	if err := JSONOneOrMany(&one, []obj{{1}}); err != nil {
		t.Fatal(err)
	}
	if got := one.String(); got != "{\n  \"n\": 1\n}\n" {
		t.Errorf("one = %q", got)
	}
	var many strings.Builder
	if err := JSONOneOrMany(&many, []obj{{1}, {2}}); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(many.String(), "[") {
		t.Errorf("many = %q, want an array", many.String())
	}
	var none strings.Builder
	if err := JSONOneOrMany(&none, []obj{}); err != nil {
		t.Fatal(err)
	}
	if got := none.String(); got != "[]\n" {
		t.Errorf("none = %q, want an empty array", got)
	}
}
