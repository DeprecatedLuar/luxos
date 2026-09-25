package nixsrc

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// ---- List ----

func TestListImports_MultiLine(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "default.nix")
	write(t, file, "{ ... }:\n{\n  imports = [\n    ./a.nix\n    ./b/c.nix # kept\n  ];\n}\n")

	got, ok, err := ListImports(file)
	if err != nil || !ok {
		t.Fatalf("ListImports: ok=%v err=%v", ok, err)
	}
	want := []string{"a.nix", "b/c.nix"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("List = %v, want %v", got, want)
	}
}

func TestListImports_InlineEmpty(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "default.nix")
	write(t, file, "{ ... }:\n{\n  imports = [];\n}\n")

	got, ok, err := ListImports(file)
	if err != nil || !ok {
		t.Fatalf("ListImports: ok=%v err=%v", ok, err)
	}
	if len(got) != 0 {
		t.Errorf("List = %v, want empty", got)
	}
}

func TestListImports_CommentedLineIgnored(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "default.nix")
	write(t, file, "{ ... }:\n{\n  imports = [\n    ./a.nix\n    # ./b.nix\n  ];\n}\n")

	got, ok, err := ListImports(file)
	if err != nil || !ok {
		t.Fatalf("ListImports: ok=%v err=%v", ok, err)
	}
	want := []string{"a.nix"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("List = %v, want %v", got, want)
	}

	// The comment line itself must be untouched.
	content := mustReadFile(t, file)
	if !strings.Contains(content, "# ./b.nix") {
		t.Errorf("comment line altered: %q", content)
	}
}

// ---- Add ----

func TestAddImport_MultiLine(t *testing.T) {
	skipIfNoNix(t)
	dir := t.TempDir()
	file := filepath.Join(dir, "default.nix")
	write(t, file, "{ ... }:\n{\n  imports = [\n    ./a.nix\n  ];\n}\n")

	if err := AddImport(file, "./b.nix"); err != nil {
		t.Fatalf("Add: %v", err)
	}

	got, ok, err := ListImports(file)
	if err != nil || !ok {
		t.Fatalf("ListImports: ok=%v err=%v", ok, err)
	}
	want := []string{"a.nix", "b.nix"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("List = %v, want %v", got, want)
	}
}

func TestAddImport_InlineEmptyConverts(t *testing.T) {
	skipIfNoNix(t)
	dir := t.TempDir()
	file := filepath.Join(dir, "default.nix")
	write(t, file, "{ ... }:\n{\n  imports = [];\n}\n")

	if err := AddImport(file, "a.nix"); err != nil {
		t.Fatalf("Add: %v", err)
	}

	got, ok, err := ListImports(file)
	if err != nil || !ok {
		t.Fatalf("ListImports: ok=%v err=%v", ok, err)
	}
	if !reflect.DeepEqual(got, []string{"a.nix"}) {
		t.Errorf("List = %v", got)
	}
}

func TestAddImport_ExistingIsNoOp(t *testing.T) {
	skipIfNoNix(t)
	dir := t.TempDir()
	file := filepath.Join(dir, "default.nix")
	content := "{ ... }:\n{\n  imports = [\n    ./a.nix\n  ];\n}\n"
	write(t, file, content)

	if err := AddImport(file, "a.nix"); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if got := mustReadFile(t, file); got != content {
		t.Errorf("file changed on no-op add:\n%q\nwant\n%q", got, content)
	}
}

// ---- Remove ----

func TestRemoveImport_MultiLine(t *testing.T) {
	skipIfNoNix(t)
	dir := t.TempDir()
	file := filepath.Join(dir, "default.nix")
	write(t, file, "{ ... }:\n{\n  imports = [\n    ./a.nix\n    ./b.nix\n  ];\n}\n")

	if err := RemoveImport(file, "a.nix"); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	got, ok, err := ListImports(file)
	if err != nil || !ok {
		t.Fatalf("ListImports: ok=%v err=%v", ok, err)
	}
	if !reflect.DeepEqual(got, []string{"b.nix"}) {
		t.Errorf("List = %v", got)
	}
}

func TestRemoveImport_CommentedUntouched(t *testing.T) {
	skipIfNoNix(t)
	dir := t.TempDir()
	file := filepath.Join(dir, "default.nix")
	write(t, file, "{ ... }:\n{\n  imports = [\n    ./a.nix\n    # ./b.nix\n  ];\n}\n")

	if err := RemoveImport(file, "b.nix"); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	content := mustReadFile(t, file)
	if !strings.Contains(content, "# ./b.nix") {
		t.Errorf("comment removed: %q", content)
	}
}

// ---- Refused shapes ----

func TestAddImport_RefusesTwoBlocks(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "default.nix")
	write(t, file, "{ ... }:\n{\n  imports = [\n    ./a.nix\n  ];\n  x = {\n    imports = [\n      ./b.nix\n    ];\n  };\n}\n")

	if err := AddImport(file, "c.nix"); err == nil {
		t.Fatalf("Add: want error for two imports blocks")
	}
}

func TestAddImport_RefusesItemOnOpeningLine(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "default.nix")
	write(t, file, "{ ... }:\n{\n  imports = [ ./a.nix\n  ];\n}\n")

	if err := AddImport(file, "b.nix"); err == nil {
		t.Fatalf("Add: want error for item on opening line")
	}
}

func TestListImports_RefusesUnrecognizedShape(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "default.nix")
	write(t, file, "{ ... }:\n{\n  imports = [ ./a.nix\n  ];\n}\n")

	if _, ok, err := ListImports(file); err != nil || ok {
		t.Fatalf("ListImports: want ok=false, nil err for unrecognized shape, got ok=%v err=%v", ok, err)
	}
}

// ---- Ownership preservation ----

func TestAddImport_PreservesOwnership(t *testing.T) {
	skipIfNoNix(t)
	dir := t.TempDir()
	file := filepath.Join(dir, "default.nix")
	write(t, file, "{ ... }:\n{\n  imports = [\n    ./a.nix\n  ];\n}\n")

	before, err := os.Stat(file)
	if err != nil {
		t.Fatalf("stat before: %v", err)
	}

	if err := AddImport(file, "b.nix"); err != nil {
		t.Fatalf("Add: %v", err)
	}

	after, err := os.Stat(file)
	if err != nil {
		t.Fatalf("stat after: %v", err)
	}

	if !os.SameFile(before, after) {
		t.Errorf("file was replaced rather than written in place")
	}
}
