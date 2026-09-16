package shared

import (
	"reflect"
	"testing"
)

const testSpec = "verbose|v:bool force:bool output|o:value"

func TestParse_Bool(t *testing.T) {
	opts, rest, err := Parse(testSpec, []string{"--verbose", "positional"})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if opts["verbose"] != "1" {
		t.Fatalf("opts[verbose] = %q, want 1", opts["verbose"])
	}
	if !reflect.DeepEqual(rest, []string{"positional"}) {
		t.Fatalf("rest = %v", rest)
	}
}

func TestParse_Value(t *testing.T) {
	opts, _, err := Parse(testSpec, []string{"--output", "file.txt"})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if opts["output"] != "file.txt" {
		t.Fatalf("opts[output] = %q, want file.txt", opts["output"])
	}

	opts, _, err = Parse(testSpec, []string{"--output=file.txt"})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if opts["output"] != "file.txt" {
		t.Fatalf("opts[output] = %q, want file.txt", opts["output"])
	}
}

func TestParse_ShortAlias(t *testing.T) {
	opts, _, err := Parse(testSpec, []string{"-v", "-o", "out.txt"})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if opts["verbose"] != "1" {
		t.Fatalf("opts[verbose] = %q, want 1", opts["verbose"])
	}
	if opts["output"] != "out.txt" {
		t.Fatalf("opts[output] = %q, want out.txt", opts["output"])
	}
}

func TestParse_UnknownFlagErrors(t *testing.T) {
	_, _, err := Parse(testSpec, []string{"--bogus"})
	if err == nil {
		t.Fatal("expected error for unknown flag in strict mode")
	}
}

func TestParsePassthrough_UnknownFlagForwarded(t *testing.T) {
	opts, rest, err := ParsePassthrough(testSpec, []string{"--verbose", "--bogus", "value"})
	if err != nil {
		t.Fatalf("ParsePassthrough: %v", err)
	}
	if opts["verbose"] != "1" {
		t.Fatalf("opts[verbose] = %q, want 1", opts["verbose"])
	}
	if !reflect.DeepEqual(rest, []string{"--bogus", "value"}) {
		t.Fatalf("rest = %v", rest)
	}
}

func TestParse_DoubleDashStopsParsing(t *testing.T) {
	opts, rest, err := Parse(testSpec, []string{"--verbose", "--", "--force", "positional"})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if opts["verbose"] != "1" {
		t.Fatalf("opts[verbose] = %q, want 1", opts["verbose"])
	}
	if _, ok := opts["force"]; ok {
		t.Fatalf("opts[force] should not be set, --force came after --")
	}
	if !reflect.DeepEqual(rest, []string{"--force", "positional"}) {
		t.Fatalf("rest = %v", rest)
	}
}

func TestParse_MissingValueErrors(t *testing.T) {
	_, _, err := Parse(testSpec, []string{"--output"})
	if err == nil {
		t.Fatal("expected error for missing value")
	}
}
