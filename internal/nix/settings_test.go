package nix

import (
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestReadSettings_LeavesAndLines(t *testing.T) {
	skipIfNoNix(t)
	dir := t.TempDir()
	f := writeNix(t, dir, "s.nix", `{
  laptop.mode = "performance";
  laptop.list = [
    { a = "x"; }
  ];
  other.thing = 1;
}
`)
	got, broken, err := ReadSettings([]SettingsRequest{{File: f, Depth: 2}})
	if err != nil || len(broken) > 0 {
		t.Fatalf("ReadSettings: %v %v", err, broken)
	}
	want := []SettingLeaf{
		{Path: []string{"laptop", "mode"}, Line: 2},
		{Path: []string{"laptop", "list"}, Line: 3},
		{Path: []string{"other", "thing"}, Line: 6},
	}
	if !reflect.DeepEqual(got[f], want) {
		t.Errorf("leaves = %+v, want %+v", got[f], want)
	}
}

func TestReadSettings_ThroughSymlink(t *testing.T) {
	skipIfNoNix(t)
	dir := t.TempDir()
	writeNix(t, dir, "s.nix", "{\n  laptop.a = 1;\n  laptop.b = 2;\n}\n")
	link := dir + "/link"
	if err := os.Symlink(dir, link); err != nil {
		t.Fatal(err)
	}
	f := link + "/s.nix"
	got, broken, err := ReadSettings([]SettingsRequest{{File: f, Depth: 2}})
	if err != nil || len(broken) > 0 {
		t.Fatalf("ReadSettings: %v %v", err, broken)
	}
	want := []SettingLeaf{
		{Path: []string{"laptop", "a"}, Line: 2},
		{Path: []string{"laptop", "b"}, Line: 3},
	}
	if !reflect.DeepEqual(got[f], want) {
		t.Errorf("leaves = %+v, want %+v", got[f], want)
	}
}

func TestReadSettings_FunctionFile(t *testing.T) {
	skipIfNoNix(t)
	dir := t.TempDir()
	f := writeNix(t, dir, "s.nix", "{ pkgs, ... }: {\n  eduardo.git.pkg = pkgs.hello;\n}\n")
	got, broken, err := ReadSettings([]SettingsRequest{{File: f, Depth: 3}})
	if err != nil || len(broken) > 0 {
		t.Fatalf("ReadSettings: %v %v", err, broken)
	}
	if want := []SettingLeaf{{Path: []string{"eduardo", "git", "pkg"}, Line: 2}}; !reflect.DeepEqual(got[f], want) {
		t.Errorf("leaves = %+v, want %+v", got[f], want)
	}
}

func TestReadSettings_UnparsableIsBroken(t *testing.T) {
	skipIfNoNix(t)
	dir := t.TempDir()
	f := writeNix(t, dir, "s.nix", "{ laptop.a = ; }\n")
	_, broken, err := ReadSettings([]SettingsRequest{{File: f, Depth: 2}})
	if err != nil {
		t.Fatalf("ReadSettings: %v", err)
	}
	if broken[f] == "" {
		t.Errorf("broken = %v, want %s", broken, f)
	}
}

func TestRenderSettings(t *testing.T) {
	got := string(RenderSettings([]Setting{
		{Path: []string{"laptop", "limit"}, Value: float64(80)},
		{Path: []string{"laptop", "mode"}, Value: "balanced"},
	}))
	want := "{\n  laptop.limit = 80;\n  laptop.mode = \"balanced\";\n}\n"
	if got != want {
		t.Errorf("RenderSettings = %q, want %q", got, want)
	}
}

func TestRenderSettings_Escapes(t *testing.T) {
	skipIfNoNix(t)
	dir := t.TempDir()
	value := `a"b\c${d}`
	f := writeNix(t, dir, "s.nix", string(RenderSettings([]Setting{{Path: []string{"x", "s"}, Value: value}})))
	raw, err := os.ReadFile(f)
	if err != nil {
		t.Fatal(err)
	}
	got, err := EvalJSON("(import "+f+").x.s", nil)
	if err != nil {
		t.Fatalf("eval %s: %v", raw, err)
	}
	if strings.TrimSpace(string(got)) != `"a\"b\\c${d}"` {
		t.Errorf("evaluated %s from %s", got, raw)
	}
}

func TestAppendSettings(t *testing.T) {
	skipIfNoNix(t)
	dir := t.TempDir()
	f := writeNix(t, dir, "s.nix", "{\n  laptop.mode = \"a\";\n}\n")
	if err := AppendSettings(f, []Setting{{Path: []string{"laptop", "limit"}, Value: float64(80)}}); err != nil {
		t.Fatalf("AppendSettings: %v", err)
	}
	got, _ := os.ReadFile(f)
	if want := "{\n  laptop.mode = \"a\";\n  laptop.limit = 80;\n}\n"; string(got) != want {
		t.Errorf("file = %q, want %q", got, want)
	}
}

func TestCommentSettings_MultiLineValue(t *testing.T) {
	skipIfNoNix(t)
	dir := t.TempDir()
	f := writeNix(t, dir, "s.nix", "{\n  laptop.list = [\n    1\n  ];\n  laptop.mode = \"a\";\n  laptop.old = {\n    x = 1;\n  };\n}\n")
	if err := CommentSettings(f, 2, [][]string{{"laptop", "list"}, {"laptop", "old"}}); err != nil {
		t.Fatalf("CommentSettings: %v", err)
	}
	got, _ := os.ReadFile(f)
	want := "{\n  # laptop.list = [\n  #   1\n  # ];\n  laptop.mode = \"a\";\n  # laptop.old = {\n  #   x = 1;\n  # };\n}\n"
	if string(got) != want {
		t.Errorf("file =\n%s\nwant\n%s", got, want)
	}
}

func TestRenderSettings_Comment(t *testing.T) {
	got := string(RenderSettings([]Setting{
		{Path: []string{"laptop", "mode"}, Value: "a", Comment: "a | b\n  or c"},
		{Path: []string{"laptop", "limit"}, Value: float64(80)},
	}))
	want := "{\n  laptop.mode = \"a\"; # a | b or c\n  laptop.limit = 80;\n}\n"
	if got != want {
		t.Errorf("RenderSettings = %q, want %q", got, want)
	}
}

func TestAppendSettings_Comment(t *testing.T) {
	skipIfNoNix(t)
	f := writeNix(t, t.TempDir(), "s.nix", "{\n  laptop.mode = \"a\";\n}\n")
	if err := AppendSettings(f, []Setting{{Path: []string{"laptop", "limit"}, Value: float64(80), Comment: "0-100"}}); err != nil {
		t.Fatalf("AppendSettings: %v", err)
	}
	got, _ := os.ReadFile(f)
	if want := "{\n  laptop.mode = \"a\";\n  laptop.limit = 80; # 0-100\n}\n"; string(got) != want {
		t.Errorf("file = %q, want %q", got, want)
	}
}

func TestCommentSettingLines(t *testing.T) {
	skipIfNoNix(t)
	f := writeNix(t, t.TempDir(), "s.nix", "{\n  laptop.a = \"x\"; # stale\n  laptop.b = \"y\";\n  laptop.c = \"p; # q\";\n  laptop.d = 1; # drop me\n  laptop.e = \"keep\"; # same\n}\n")
	leaves := []SettingLeaf{
		{Path: []string{"laptop", "a"}, Line: 2},
		{Path: []string{"laptop", "b"}, Line: 3},
		{Path: []string{"laptop", "c"}, Line: 4},
		{Path: []string{"laptop", "d"}, Line: 5},
		{Path: []string{"laptop", "e"}, Line: 6},
	}
	comments := map[string]string{"laptop.a": "new", "laptop.b": "added", "laptop.c": "ignored", "laptop.e": "same"}
	if err := CommentSettingLines(f, leaves, comments); err != nil {
		t.Fatalf("CommentSettingLines: %v", err)
	}
	got, _ := os.ReadFile(f)
	want := "{\n  laptop.a = \"x\"; # new\n  laptop.b = \"y\"; # added\n  laptop.c = \"p; # q\";\n  laptop.d = 1;\n  laptop.e = \"keep\"; # same\n}\n"
	if string(got) != want {
		t.Errorf("file =\n%s\nwant\n%s", got, want)
	}
}

func TestCommentSettingLines_NoChangeNoWrite(t *testing.T) {
	skipIfNoNix(t)
	f := writeNix(t, t.TempDir(), "s.nix", "{\n  laptop.a = 1; # same\n}\n")
	old := time.Unix(0, 0)
	if err := os.Chtimes(f, old, old); err != nil {
		t.Fatal(err)
	}
	if err := CommentSettingLines(f, []SettingLeaf{{Path: []string{"laptop", "a"}, Line: 2}}, map[string]string{"laptop.a": "same"}); err != nil {
		t.Fatal(err)
	}
	after, _ := os.Stat(f)
	if !after.ModTime().Equal(old) {
		t.Error("file rewritten although nothing changed")
	}
}
