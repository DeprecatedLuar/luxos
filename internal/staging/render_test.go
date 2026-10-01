package staging

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"testing"
	"text/template"

	"github.com/DeprecatedLuar/luxos/internal/nix"
	"github.com/DeprecatedLuar/luxos/internal/templates"
)

var update = flag.Bool("update", false, "rewrite golden files")

// assertGolden compares got against testdata/<name>.golden, rewriting the
// golden file instead when -update is passed.
func assertGolden(t *testing.T, name string, got []byte) {
	t.Helper()
	path := filepath.Join("testdata", name+".golden")

	if *update {
		if err := os.WriteFile(path, got, 0644); err != nil {
			t.Fatalf("write golden %s: %v", path, err)
		}
		return
	}

	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v", path, err)
	}
	if string(got) != string(want) {
		t.Errorf("%s mismatch\n--- got ---\n%s\n--- want ---\n%s", name, got, want)
	}
}

// assertNixParses writes content to a temp .nix file and checks it parses
// with nix-instantiate --parse.
func assertNixParses(t *testing.T, content []byte) {
	t.Helper()
	skipIfNoNix(t)

	dir := t.TempDir()
	file := filepath.Join(dir, "check.nix")
	if err := os.WriteFile(file, content, 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := nix.Parse(file); err != nil {
		t.Errorf("nix-instantiate --parse failed: %v", err)
	}
}

func TestRenderConfiguration_Golden(t *testing.T) {
	out, err := configuration("paraloid")
	if err != nil {
		t.Fatalf("configuration: %v", err)
	}
	assertGolden(t, "configuration", out)
	assertNixParses(t, out)
}

func TestRenderConfiguration_Deterministic(t *testing.T) {
	a, err := configuration("paraloid")
	if err != nil {
		t.Fatalf("configuration: %v", err)
	}
	b, err := configuration("paraloid")
	if err != nil {
		t.Fatalf("configuration: %v", err)
	}
	if string(a) != string(b) {
		t.Errorf("Configuration is not deterministic")
	}
}

func TestRenderFlakeBootstrap_Golden(t *testing.T) {
	out, err := flakeBootstrap("paraloid")
	if err != nil {
		t.Fatalf("flakeBootstrap: %v", err)
	}
	assertGolden(t, "bootstrap", out)
	assertNixParses(t, out)
}

func TestRenderFlakeBootstrap_Deterministic(t *testing.T) {
	a, err := flakeBootstrap("paraloid")
	if err != nil {
		t.Fatalf("flakeBootstrap: %v", err)
	}
	b, err := flakeBootstrap("paraloid")
	if err != nil {
		t.Fatalf("flakeBootstrap: %v", err)
	}
	if string(a) != string(b) {
		t.Errorf("FlakeBootstrap is not deterministic")
	}
}

func TestRenderFlakeBootstrap_EmptyHostErrors(t *testing.T) {
	if _, err := flakeBootstrap(""); err == nil {
		t.Fatalf("FlakeBootstrap: want error for empty host")
	}
}

func TestStaticFlakeNix_Parses(t *testing.T) {
	raw, err := templates.File("render/flake.nix.tmpl")
	if err != nil {
		t.Fatalf("framework.File(flake.nix): %v", err)
	}
	tmpl, err := template.New("flake.nix").Parse(string(raw))
	if err != nil {
		t.Fatalf("parse template: %v", err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, struct{ Inputs string }{Inputs: "    nixpkgs.url = \"github:NixOS/nixpkgs/nixos-25.11\";\n"}); err != nil {
		t.Fatalf("execute template: %v", err)
	}
	assertNixParses(t, buf.Bytes())
}

func TestSystemNix_Parses(t *testing.T) {
	content, err := templates.File("framework/system.nix")
	if err != nil {
		t.Fatalf("framework.File(system.nix): %v", err)
	}
	assertNixParses(t, content)
}
