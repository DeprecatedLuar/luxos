package nixsrc

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func skipIfNoNix(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("nix-instantiate"); err != nil {
		t.Skip("nix-instantiate not on PATH")
	}
}

func mustMkdirAll(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
}

func write(t *testing.T, path, content string) string {
	t.Helper()
	mustMkdirAll(t, filepath.Dir(path))
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}

func mustReadFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

func sortedJoin(ss []string) string {
	cp := append([]string(nil), ss...)
	sort.Strings(cp)
	return strings.Join(cp, "\n")
}

//============================================================================
// Suite: tests/parse.sh — refs.Paths / refs.DynamicPaths, what
// nix-instantiate --parse exposes about paths.
//============================================================================

func TestPaths_ParseSuite(t *testing.T) {
	skipIfNoNix(t)

	F := t.TempDir()
	realDir := filepath.Join(F, "real")
	D := filepath.Join(realDir, "cat")
	mustMkdirAll(t, D)

	caseFile := filepath.Join(D, "case.nix")

	run := func(label, body, expected string) {
		t.Run(label, func(t *testing.T) {
			write(t, caseFile, body)
			got, err := Paths(caseFile)
			if err != nil {
				t.Fatalf("Paths: %v", err)
			}
			gotSorted := sortedJoin(got)
			wantSorted := sortedJoin(strings.Split(expected, "\n"))
			if expected == "" {
				wantSorted = ""
			}
			if gotSorted != wantSorted {
				t.Errorf("Paths(%q) = %v, want [%s]", body, got, expected)
			}
		})
	}

	t.Run("path spellings the parser resolves", func(t *testing.T) {
		run("imports sibling", `{ imports = [ ./x.nix ]; }`, D+"/x.nix")
		run("imports parent escape", `{ imports = [ ../system/w.nix ]; }`, realDir+"/system/w.nix")
		run("imports directory", `{ imports = [ ./unit ]; }`, D+"/unit")
		run("value import", `let t = import ../theme.nix; in { }`, realDir+"/theme.nix")
		run("readFile non-nix", `{ x = builtins.readFile ../wall.png; }`, realDir+"/wall.png")
		run("interpolated in string", `{ x = "${../wall.png}"; }`, realDir+"/wall.png")
		run("indented string interp", "{ x = ''${../wall.png}''; }", realDir+"/wall.png")
		run("dot path", `{ x = ./.; }`, D)
		run("absolute literal", `{ imports = [ /etc/nixos/x.nix ]; }`, "/etc/nixos/x.nix")
		run("multi-line + comments", "{\n  imports = [\n    # ../commented.nix\n    ./a.nix # ../trailing.nix\n  ];\n}", D+"/a.nix")
	})

	t.Run("non-path references (must yield nothing)", func(t *testing.T) {
		run("flake input module", `{ inputs, ... }: { imports = [ inputs.foo.nixosModules.default ]; }`, "")
		run("luxos.modules call", `{ luxos, ... }: { imports = luxos.modules [ "wayland" ]; }`, "")
		run("path-looking string", `{ description = "see ../x.nix and (/etc/foo)"; }`, "")
		run("update operator //", `{ x = { a = 1; } // { b = 2; }; }`, "")
		run("division", `{ x = 4 / 2; }`, "")
		run("angle bracket <nixpkgs>", `{ x = <nixpkgs>; }`, "")
	})

	t.Run("dynamic spellings: Paths sees only the static base", func(t *testing.T) {
		run("path + string suffix", `{ imports = [ (./. + "/../escape.nix") ]; }`, D)
		run("interpolated path", `let n = "x"; in { imports = [ ./${n}.nix ]; }`, D+"/")
		run("root + string", `{ imports = [ (/. + "home/escape.nix") ]; }`, "")
	})

	t.Run("symlinks: lexical vs physical resolution", func(t *testing.T) {
		link := filepath.Join(F, "link")
		if err := os.Symlink(realDir, link); err != nil {
			t.Fatal(err)
		}
		symFile := write(t, filepath.Join(realDir, "cat", "sym.nix"), `{ imports = [ ../system/w.nix ]; }`)
		_ = symFile
		got, err := Paths(filepath.Join(link, "cat", "sym.nix"))
		if err != nil {
			t.Fatalf("Paths: %v", err)
		}
		want1 := link + "/system/w.nix"
		want2 := realDir + "/system/w.nix"
		if len(got) != 1 || (got[0] != want1 && got[0] != want2) {
			t.Errorf("unexpected symlink resolution: %v", got)
		}
	})

	t.Run("symlinked directory mid-path: .. resolution", func(t *testing.T) {
		fwMods := filepath.Join(F, "fw", "mods")
		cfgModules := filepath.Join(F, "cfg", "modules")
		mustMkdirAll(t, fwMods)
		mustMkdirAll(t, cfgModules)
		if err := os.Symlink(fwMods, filepath.Join(cfgModules, "system")); err != nil {
			t.Fatal(err)
		}
		write(t, filepath.Join(fwMods, "wayland.nix"), `{ imports = [ ../desktop.nix ]; }`)
		got, err := Paths(filepath.Join(cfgModules, "system", "wayland.nix"))
		if err != nil {
			t.Fatalf("Paths: %v", err)
		}
		lexical := filepath.Join(F, "cfg", "modules", "desktop.nix")
		physical := filepath.Join(F, "fw", "desktop.nix")
		if len(got) != 1 || (got[0] != lexical && got[0] != physical) {
			t.Errorf("unexpected .. resolution: %v", got)
		}
	})

	t.Run("relative file arg refused", func(t *testing.T) {
		if _, err := Paths("sym.nix"); err == nil {
			t.Errorf("Paths(relative): want error")
		}
	})
}

func TestDynamicPaths_ParseSuite(t *testing.T) {
	skipIfNoNix(t)
	D := t.TempDir()
	file := filepath.Join(D, "dyn.nix")

	dyn := func(label, body string, wantDynamic bool) {
		t.Run(label, func(t *testing.T) {
			write(t, file, body)
			got, err := DynamicPaths(file)
			if err != nil {
				t.Fatalf("DynamicPaths: %v", err)
			}
			if wantDynamic && len(got) == 0 {
				t.Errorf("expected dynamic paths, got none")
			}
			if !wantDynamic && len(got) != 0 {
				t.Errorf("expected no dynamic paths, got %v", got)
			}
		})
	}

	dyn("path + string suffix", `{ imports = [ (./. + "/../escape.nix") ]; }`, true)
	dyn("interpolated path", `let n = "x"; in { imports = [ ./${n}.nix ]; }`, true)
	dyn("interpolated escape", `{ imports = [ ./sub/${"../../../escape"}.nix ]; }`, true)
	dyn("root + string", `{ imports = [ (/. + "home/escape.nix") ]; }`, true)
	dyn("static path (not dynamic)", `{ imports = [ ./a.nix ../b.nix ]; }`, false)
	dyn("number addition", `{ x = 1 + 2; }`, false)
	dyn("string concat", `{ x = "a" + "b"; }`, false)
	dyn("list concat ++", `{ x = [ ./a.nix ] ++ [ ./b.nix ]; }`, false)
	dyn(`string containing "./x + "`, `{ x = "(./x + y)"; }`, false)
}

//============================================================================
// Suite: tests/names.sh — refs.Names call-shape recognition (#28), covering
// the additional shapes the Phase 6 task calls out beyond boundary/parse.
//============================================================================

func TestNames_CallShapeSuite(t *testing.T) {
	skipIfNoNix(t)
	D := t.TempDir()
	file := filepath.Join(D, "case.nix")

	ok := func(label, body, expected string) {
		t.Run(label, func(t *testing.T) {
			write(t, file, body)
			got, err := Names(file)
			if err != nil {
				t.Fatalf("Names: unexpected error: %v", err)
			}
			gotSorted := sortedJoin(got)
			var wantSorted string
			if expected != "" {
				wantSorted = sortedJoin(strings.Split(expected, "\n"))
			}
			if gotSorted != wantSorted {
				t.Errorf("Names(%q) = %v, want [%s]", body, got, expected)
			}
		})
	}

	bad := func(label, body, wantSubstr string) {
		t.Run(label, func(t *testing.T) {
			write(t, file, body)
			_, err := Names(file)
			if err == nil {
				t.Fatalf("Names: want error")
			}
			if !strings.Contains(err.Error(), wantSubstr) {
				t.Errorf("error %q does not contain %q", err.Error(), wantSubstr)
			}
		})
	}

	t.Run("recognized shape: literal list of string literals", func(t *testing.T) {
		ok("single name", `{ luxos, ... }: { imports = luxos.modules [ "wayland" ]; }`, "wayland")
		ok("multiple names", `{ luxos, ... }: { imports = luxos.modules [ "wayland" "x11" ]; }`, "wayland\nx11")
		ok("empty list", `{ luxos, ... }: { imports = luxos.modules [ ]; }`, "")
		ok("dashed name", `{ luxos, ... }: { imports = luxos.modules [ "ly-greeter" ]; }`, "ly-greeter")
		ok("not used at all", `{ ... }: { imports = [ ./a.nix ]; }`, "")
		ok("two separate calls", `{ luxos, ... }: { a = luxos.modules [ "x" ]; b = luxos.modules [ "y" ]; }`, "x\ny")
		ok("formals with other args", `{ config, lib, luxos, ... }: { imports = luxos.modules [ "a" ]; }`, "a")
		ok("luxos.modules not in imports", `{ luxos, ... }: { x = luxos.modules [ "a" ]; }`, "a")
	})

	t.Run("hard errors: any other use of luxos (#28)", func(t *testing.T) {
		bad("non-literal-list argument (bare undeclared variable)",
			`{ luxos, ... }: { imports = luxos.modules names; }`, "failed to parse")
		bad("non-literal-list argument (bare string, no list)",
			`{ luxos, ... }: { imports = luxos.modules "a"; }`, "outside a recognized")
		bad("non-literal-list argument (declared variable)",
			`{ luxos, ... }: let names = [ "a" ]; in { imports = luxos.modules names; }`, "outside a recognized")
		bad("non-string-literal list element",
			`{ luxos, ... }: let a = "x"; in { imports = luxos.modules [ a ]; }`, "literal list of string literals")
		bad("with luxos;",
			`{ luxos, ... }: with luxos; { imports = modules [ "a" ]; }`, "outside a recognized")
		bad("args.luxos.modules",
			`{ args, ... }: { imports = args.luxos.modules [ "a" ]; }`, "outside a recognized")
		bad("luxos referenced bare",
			`{ luxos, ... }: { x = luxos; }`, "outside a recognized")
		bad("luxos.modules with list concat argument",
			`{ luxos, ... }: { imports = luxos.modules ([ "a" ] ++ [ "b" ]); }`, "outside a recognized")
	})

	t.Run("parse failure is a hard error too", func(t *testing.T) {
		bad("syntactically invalid file", `{ luxos, ... }: { imports = [ ; }`, "failed to parse")
	})
}

func TestWrite_PreservesTargetThroughSymlink(t *testing.T) {
	skipIfNoNix(t)
	D := t.TempDir()
	target := write(t, filepath.Join(D, "real", "target.nix"), `{ a = 1; }`)
	link := filepath.Join(D, "link.nix")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}

	if err := Write(link, []byte("{ a = 2; }\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}

	fi, err := os.Lstat(link)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode()&os.ModeSymlink == 0 {
		t.Errorf("link was replaced by a regular file")
	}
	if got := mustReadFile(t, target); got != "{ a = 2; }\n" {
		t.Errorf("target content = %q", got)
	}
}

func TestWrite_RefusesUnparsable(t *testing.T) {
	skipIfNoNix(t)
	D := t.TempDir()
	const original = `{ a = 1; }`
	file := write(t, filepath.Join(D, "f.nix"), original)

	err := Write(file, []byte("{ a = ; \n"))
	if err == nil {
		t.Fatalf("Write: want error for unparsable content")
	}
	if !strings.Contains(err.Error(), "would fail to parse") {
		t.Errorf("error %q lacks 'would fail to parse'", err)
	}
	if got := mustReadFile(t, file); got != original {
		t.Errorf("file changed: %q", got)
	}
}
