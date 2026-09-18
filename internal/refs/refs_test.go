package refs

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/DeprecatedLuar/luxos/internal/units"
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
// Suite: tests/boundary.sh — refs.Validate against a fixture tree shaped
// like the real CONFIG_DIR (config dir itself a symlink, modules/system a
// symlink into the framework).
//============================================================================

func TestValidate_Boundary(t *testing.T) {
	skipIfNoNix(t)

	F := t.TempDir()
	mustMkdirAll(t, filepath.Join(F, "real", "modules"))
	mustMkdirAll(t, filepath.Join(F, "fw", "mods"))
	if err := os.Symlink(filepath.Join(F, "real"), filepath.Join(F, "cfg")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(F, "fw", "mods"), filepath.Join(F, "real", "modules", "system")); err != nil {
		t.Fatal(err)
	}
	M := filepath.Join(F, "real", "modules")
	FW := filepath.Join(F, "fw", "mods")

	write(t, filepath.Join(M, "default.nix"), `{ imports = [ ./desktop/compositors/hyprland.nix ../elsewhere.nix ]; }`)
	write(t, filepath.Join(FW, "desktop.nix"), `{ }`)
	write(t, filepath.Join(FW, "wayland.nix"), `{ imports = [ ./desktop.nix ]; }`)
	write(t, filepath.Join(FW, "bundle", "default.nix"), `{ imports = [ ./parts.nix ]; }`)
	write(t, filepath.Join(FW, "bundle", "parts.nix"), `{ }`)
	write(t, filepath.Join(M, "desktop/compositors/hyprland.nix"), `{ imports = [ ../../system/wayland.nix ]; }`)
	write(t, filepath.Join(M, "desktop/compositors/niri.nix"), `{ luxos, ... }: { imports = luxos.modules [ "wayland" ]; }`)
	write(t, filepath.Join(M, "desktop/shells/ambxst.nix"), `{ inputs, ... }: { imports = [ inputs.ambxst.nixosModules.default ]; }`)
	write(t, filepath.Join(M, "users/luar/default.nix"), `{ imports = [ ./account.nix ./nested/deep.nix ]; }`)
	write(t, filepath.Join(M, "users/luar/account.nix"), `{ x = builtins.readFile ./key.pub; }`)
	write(t, filepath.Join(M, "users/luar/key.pub"), `ssh-ed25519 AAAA`)
	write(t, filepath.Join(M, "users/luar/nested/deep.nix"), `{ imports = [ ../account.nix ]; y = ./.; }`)
	write(t, filepath.Join(M, "users/luar/leak.nix"), `{ imports = [ ../../desktop/shells/ambxst.nix ]; }`)
	write(t, filepath.Join(M, "users/bob/default.nix"), `{ imports = [ ../luar/account.nix ]; }`)
	write(t, filepath.Join(M, "foo/default.nix"), `{ imports = [ ../foobar/x.nix ]; }`)
	write(t, filepath.Join(M, "foobar/x.nix"), `{ }`)
	write(t, filepath.Join(M, "theme/single.nix"), `{ x = builtins.readFile ../wall.png; }`)
	write(t, filepath.Join(M, "theme/uses-dir.nix"), `{ imports = [ ../foo ]; }`)
	write(t, filepath.Join(M, "dyn/default.nix"), `let n = "x"; in { imports = [ ./${n}.nix ]; }`)
	write(t, filepath.Join(M, "abs.nix"), `{ imports = [ /etc/nixos/x.nix ]; }`)
	write(t, filepath.Join(M, "_hidden.nix"), `{ }`)
	write(t, filepath.Join(M, "hidden-user.nix"), `{ imports = [ ./_hidden.nix ]; }`)

	// relfile -> kind ("outside"/"dynamic"), the exact violation set
	// expected. Every path counts (#25): no narrower "nix files only"
	// scope.
	expected := map[string]string{
		"abs.nix":                           "outside",
		"desktop/compositors/hyprland.nix":  "outside",
		"dyn/default.nix":                   "dynamic",
		"foo/default.nix":                   "outside",
		"hidden-user.nix":                   "outside",
		"system/wayland.nix":                "outside",
		"theme/single.nix":                  "outside",
		"theme/uses-dir.nix":                "outside",
		"users/bob/default.nix":             "outside",
		"users/luar/leak.nix":               "outside",
	}

	violationsFor := func(modulesDir string) map[string]string {
		us, err := units.Walk(modulesDir, "")
		if err != nil {
			t.Fatalf("units.Walk(%s): %v", modulesDir, err)
		}
		vs, err := Validate(modulesDir, us, allUnitPaths(us))
		if err != nil {
			t.Fatalf("Validate(%s): %v", modulesDir, err)
		}
		got := make(map[string]string)
		for _, v := range vs {
			switch {
			case strings.HasPrefix(v.Message, "references"):
				got[v.File] = "outside"
			case strings.HasPrefix(v.Message, "uses a dynamic path"):
				got[v.File] = "dynamic"
			}
		}
		return got
	}

	t.Run("walked through the config symlink", func(t *testing.T) {
		got := violationsFor(filepath.Join(F, "cfg", "modules"))
		if len(got) != len(expected) {
			t.Errorf("violation count = %d, want %d (got=%v)", len(got), len(expected), got)
		}
		for f, kind := range expected {
			if got[f] != kind {
				t.Errorf("%s: got kind %q, want %q", f, got[f], kind)
			}
		}
		for f, kind := range got {
			if expected[f] != kind {
				t.Errorf("unexpected violation %s: %s", f, kind)
			}
		}
	})

	t.Run("clean files produce no violations", func(t *testing.T) {
		got := violationsFor(filepath.Join(F, "cfg", "modules"))
		clean := []string{
			"system/desktop.nix", "system/bundle/default.nix", "system/bundle/parts.nix",
			"desktop/compositors/niri.nix", "desktop/shells/ambxst.nix",
			"users/luar/default.nix", "users/luar/account.nix", "users/luar/nested/deep.nix", "foobar/x.nix",
		}
		for _, f := range clean {
			if _, flagged := got[f]; flagged {
				t.Errorf("%s flagged, want clean", f)
			}
		}
	})

	t.Run("same result walking the physical path", func(t *testing.T) {
		a := violationsFor(filepath.Join(F, "cfg", "modules"))
		b := violationsFor(filepath.Join(F, "real", "modules"))
		if len(a) != len(b) {
			t.Fatalf("symlink vs real differ: %v vs %v", a, b)
		}
		for f, kind := range a {
			if b[f] != kind {
				t.Errorf("%s: symlink=%s real=%s", f, kind, b[f])
			}
		}
	})

	t.Run("clean tree: no violations", func(t *testing.T) {
		clean := t.TempDir()
		mods := filepath.Join(clean, "modules")
		write(t, filepath.Join(mods, "default.nix"), `{ imports = [ ./a.nix ]; }`)
		write(t, filepath.Join(mods, "a.nix"), `{ }`)
		us, err := units.Walk(mods, "")
		if err != nil {
			t.Fatal(err)
		}
		vs, err := Validate(mods, us, []string{"a.nix"})
		if err != nil {
			t.Fatalf("Validate: %v", err)
		}
		if len(vs) != 0 {
			t.Errorf("clean tree: got violations %v, want none", vs)
		}
	})
}

// allUnitPaths selects every unit, so Validate's closure is the whole tree.
func allUnitPaths(us []units.Unit) []string {
	out := make([]string, len(us))
	for i, u := range us {
		out[i] = u.Path
	}
	return out
}

func TestValidate_OnlyImportedClosure(t *testing.T) {
	skipIfNoNix(t)
	mods := t.TempDir()
	write(t, filepath.Join(mods, "host.nix"), `{ luxos, ... }: { imports = luxos.modules [ "dep" ]; }`)
	write(t, filepath.Join(mods, "dep", "default.nix"), `{ imports = [ ./inner.nix ]; }`)
	write(t, filepath.Join(mods, "dep", "inner.nix"), `{ imports = [ ../../outside.nix ]; }`)
	write(t, filepath.Join(mods, "unused.nix"), `let n = "x"; in { imports = [ ./${n}.nix ../elsewhere.nix ]; }`)

	us, err := units.Walk(mods, "")
	if err != nil {
		t.Fatal(err)
	}
	vs, err := Validate(mods, us, []string{"host.nix"})
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	flagged := make(map[string]bool)
	for _, v := range vs {
		flagged[v.File] = true
	}
	if !flagged["dep/inner.nix"] {
		t.Errorf("dep/inner.nix (reached through luxos.modules) not flagged, got %v", vs)
	}
	if flagged["unused.nix"] {
		t.Errorf("unused.nix is imported by nothing but was flagged: %v", vs)
	}

	if _, err := Validate(mods, us, []string{"missing.nix"}); err == nil {
		t.Error("Validate with an unresolvable root: want error, got nil")
	}
}

//============================================================================
// Suite: refs.Closure — the display-only, best-effort walk `module`/`user
// list` uses to mark a module as pulled in without directly selecting it.
//============================================================================

func TestClosure(t *testing.T) {
	skipIfNoNix(t)
	mods := t.TempDir()
	write(t, filepath.Join(mods, "a.nix"), `{ luxos, ... }: { imports = luxos.modules [ "b" ]; }`)
	write(t, filepath.Join(mods, "b.nix"), `{ }`)
	write(t, filepath.Join(mods, "c.nix"), `{ }`) // unreferenced, never a key

	us, err := units.Walk(mods, "")
	if err != nil {
		t.Fatal(err)
	}
	got, err := Closure(mods, us, []string{"a.nix"})
	if err != nil {
		t.Fatalf("Closure: %v", err)
	}
	if want := []string{"a"}; !equalStrings(got["b"], want) {
		t.Errorf("pullers[b] = %v, want %v", got["b"], want)
	}
	if _, ok := got["c"]; ok {
		t.Errorf("c is never referenced but got a pullers entry: %v", got["c"])
	}
	if _, ok := got["a"]; ok {
		t.Errorf("a is a root, referenced by nothing, but got a pullers entry: %v", got["a"])
	}
}

func TestClosure_UnresolvableNameSkipped(t *testing.T) {
	skipIfNoNix(t)
	mods := t.TempDir()
	write(t, filepath.Join(mods, "a.nix"), `{ luxos, ... }: { imports = luxos.modules [ "missing" ]; }`)

	us, err := units.Walk(mods, "")
	if err != nil {
		t.Fatal(err)
	}
	got, err := Closure(mods, us, []string{"a.nix"})
	if err != nil {
		t.Fatalf("Closure: want no error for an unresolvable name (display-only, best-effort), got %v", err)
	}
	if len(got) != 0 {
		t.Errorf("Closure = %v, want empty", got)
	}
}

func TestClosure_UnparseableFileSkipped(t *testing.T) {
	skipIfNoNix(t)
	mods := t.TempDir()
	write(t, filepath.Join(mods, "a.nix"), `{ luxos, ...`) // unterminated, fails to parse

	us, err := units.Walk(mods, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Closure(mods, us, []string{"a.nix"}); err != nil {
		t.Fatalf("Closure: want no error for a file that fails to parse (display-only, best-effort), got %v", err)
	}
}

func equalStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
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

//============================================================================
// Suite: tests/retarget.sh — refs.Retarget, the one writer of module files.
//============================================================================

func TestRetarget(t *testing.T) {
	skipIfNoNix(t)
	F := t.TempDir()
	mods := filepath.Join(F, "modules")
	mustMkdirAll(t, mods)

	t.Run("rename: rewrites every dependent, leaves everything else untouched", func(t *testing.T) {
		write(t, filepath.Join(mods, "default.nix"), `{ imports = [ ./a.nix ./b.nix ./c.nix ]; }`)
		write(t, filepath.Join(mods, "a.nix"), `{ luxos, ... }: { imports = luxos.modules [ "wayland" ]; }`)
		write(t, filepath.Join(mods, "b.nix"), "# a comment that must survive\n{ luxos, ... }:\n{\n  imports = luxos.modules [\n    \"x\"\n    \"wayland\"\n  ];\n} # trailing too")
		write(t, filepath.Join(mods, "c.nix"), `{ }`)
		write(t, filepath.Join(mods, "wayland.nix"), `{ }`)

		changes, err := Retarget(mods, nil, "wayland", "wl")
		if err != nil {
			t.Fatalf("Retarget: %v", err)
		}

		byFile := make(map[string]Change)
		for _, c := range changes {
			byFile[c.File] = c
		}
		aFile := filepath.Join(mods, "a.nix")
		bFile := filepath.Join(mods, "b.nix")
		cFile := filepath.Join(mods, "c.nix")

		if c, ok := byFile[aFile]; !ok || c.Old != "wayland" || c.New != "wl" {
			t.Errorf("a.nix change = %+v, ok=%v", c, ok)
		}
		if c, ok := byFile[bFile]; !ok || c.Old != "wayland" || c.New != "wl" {
			t.Errorf("b.nix change = %+v, ok=%v", c, ok)
		}
		if _, ok := byFile[cFile]; ok {
			t.Errorf("c.nix should not be mentioned")
		}

		if got := mustReadFile(t, aFile); got != `{ luxos, ... }: { imports = luxos.modules [ "wl" ]; }`+"\n" {
			t.Errorf("a.nix content = %q", got)
		}
		if got := mustReadFile(t, cFile); got != `{ }` {
			t.Errorf("c.nix changed: %q", got)
		}

		gotB := mustReadFile(t, bFile)
		if !strings.Contains(gotB, "a comment that must survive") || !strings.Contains(gotB, "trailing too") ||
			!strings.Contains(gotB, `"x"`) || !strings.Contains(gotB, `"wl"`) || strings.Contains(gotB, `"wayland"`) {
			t.Errorf("b.nix rewrite wrong: %q", gotB)
		}

		if _, err := exec.Command("nix-instantiate", "--parse", aFile).CombinedOutput(); err != nil {
			t.Errorf("a.nix no longer parses")
		}
		if _, err := exec.Command("nix-instantiate", "--parse", bFile).CombinedOutput(); err != nil {
			t.Errorf("b.nix no longer parses")
		}
	})

	t.Run("remove: drops the element, deletes single-element lists cleanly", func(t *testing.T) {
		write(t, filepath.Join(mods, "default.nix"), `{ imports = [ ./a.nix ./d.nix ]; }`)
		write(t, filepath.Join(mods, "d.nix"), `{ luxos, ... }: { imports = luxos.modules [ "wl" ]; }`)

		changes, err := Retarget(mods, nil, "wl", "")
		if err != nil {
			t.Fatalf("Retarget: %v", err)
		}
		dFile := filepath.Join(mods, "d.nix")
		found := false
		for _, c := range changes {
			if c.File == dFile && c.Old == "wl" && c.New == "" {
				found = true
			}
		}
		if !found {
			t.Errorf("remove not reported: %v", changes)
		}
		if got := mustReadFile(t, dFile); got != `{ luxos, ... }: { imports = luxos.modules [ ]; }`+"\n" {
			t.Errorf("d.nix content = %q", got)
		}
		if err := exec.Command("nix-instantiate", "--parse", dFile).Run(); err != nil {
			t.Errorf("d.nix no longer parses")
		}
	})

	t.Run("no dependents: no-op, no error", func(t *testing.T) {
		changes, err := Retarget(mods, nil, "nonexistent-name", "newname")
		if err != nil {
			t.Fatalf("Retarget: %v", err)
		}
		if len(changes) != 0 {
			t.Errorf("expected no changes, got %v", changes)
		}
	})

	t.Run("item 20: a dependent with an unrecognized luxos shape refuses the whole write", func(t *testing.T) {
		write(t, filepath.Join(mods, "default.nix"), `{ imports = [ ./e.nix ./f.nix ]; }`)
		eFile := write(t, filepath.Join(mods, "e.nix"), `{ luxos, ... }: { imports = luxos.modules [ "shared" ]; }`)
		fFile := write(t, filepath.Join(mods, "f.nix"), `{ luxos, ... }: { imports = luxos.modules [ "shared" ]; extra = luxos; }`)
		beforeE := mustReadFile(t, eFile)
		beforeF := mustReadFile(t, fFile)

		if _, err := Retarget(mods, nil, "shared", "renamed"); err == nil {
			t.Fatalf("Retarget: want error refusing the whole write")
		}
		if mustReadFile(t, eFile) != beforeE {
			t.Errorf("e.nix was written despite refusal")
		}
		if mustReadFile(t, fFile) != beforeF {
			t.Errorf("f.nix was written despite refusal")
		}
	})

	t.Run("idempotent: a second identical retarget changes nothing further", func(t *testing.T) {
		write(t, filepath.Join(mods, "default.nix"), `{ imports = [ ./g.nix ]; }`)
		gFile := write(t, filepath.Join(mods, "g.nix"), `{ luxos, ... }: { imports = luxos.modules [ "shared" ]; }`)
		os.Remove(filepath.Join(mods, "e.nix"))
		os.Remove(filepath.Join(mods, "f.nix"))

		if _, err := Retarget(mods, nil, "shared", "renamed"); err != nil {
			t.Fatalf("Retarget (first run): %v", err)
		}
		before := mustReadFile(t, gFile)
		changes2, err := Retarget(mods, nil, "shared", "renamed")
		if err != nil {
			t.Fatalf("Retarget (second run): %v", err)
		}
		after := mustReadFile(t, gFile)
		if len(changes2) != 0 {
			t.Errorf("second run should be a no-op, got %v", changes2)
		}
		if before != after {
			t.Errorf("second run changed g.nix")
		}
	})
}

//============================================================================
// Suite: tests/modules-function.sh — a name-resolving `luxos.modules` inside
// the real Nix module system. The name map is built with units.Walk (the Go
// equivalent of configgen::walk_units), over a fixture laid out like
// $STAGING_DIR. This exercises the §3 "luxos.modules" generated function
// shape directly with nix-instantiate/nix, independent of internal/generate
// (Phase 7), which will embed the same function body.
//============================================================================

func TestModulesFunction(t *testing.T) {
	skipIfNoNix(t)
	if _, err := exec.LookPath("nix"); err != nil {
		t.Skip("nix not on PATH")
	}

	F := t.TempDir()
	S := filepath.Join(F, "staged")
	M := filepath.Join(S, "config", "modules")

	write(t, filepath.Join(S, "marks.nix"), `{ lib, ... }: { options.marks = lib.mkOption { type = lib.types.listOf lib.types.str; default = []; }; }`)
	write(t, filepath.Join(M, "system/desktop.nix"), `{ config.marks = [ "desktop" ]; }`)
	write(t, filepath.Join(M, "system/wayland.nix"), `{ luxos, lib, ... }: { imports = luxos.modules [ "desktop" ]; options.waylandOpt = lib.mkOption { default = 1; }; config.marks = [ "wayland" ]; }`)
	write(t, filepath.Join(M, "desktop/compositors/hyprland.nix"), `{ luxos, ... }: { imports = luxos.modules [ "wayland" ]; config.marks = [ "hyprland" ]; }`)
	write(t, filepath.Join(M, "desktop/greeters/ly-greeter.nix"), `{ config.marks = [ "ly-greeter" ]; }`)
	write(t, filepath.Join(M, "users/luar/default.nix"), `{ imports = [ ./account.nix ]; config.marks = [ "luar" ]; }`)
	write(t, filepath.Join(M, "users/luar/account.nix"), `{ config.marks = [ "account" ]; }`)
	write(t, filepath.Join(M, "cyc/a.nix"), `{ luxos, ... }: { imports = luxos.modules [ "b" ]; config.marks = [ "a" ]; }`)
	write(t, filepath.Join(M, "cyc/b.nix"), `{ luxos, ... }: { imports = luxos.modules [ "a" ]; config.marks = [ "b" ]; }`)

	us, err := units.Walk(M, "")
	if err != nil {
		t.Fatalf("units.Walk: %v", err)
	}
	var unitsNix strings.Builder
	unitsNix.WriteString("{\n")
	for _, u := range us {
		unitsNix.WriteString("  \"" + u.Name + "\" = ./config/modules/" + u.Path + ";\n")
	}
	unitsNix.WriteString("}\n")
	write(t, filepath.Join(S, "units.nix"), unitsNix.String())

	write(t, filepath.Join(S, "luxos.nix"), `{ units }: {
  modules = names: map (n: units.${n} or (throw "luxos.modules: '${n}' does not resolve to any module under modules/")) names;
}`)

	ev := func(expr string) (string, error) {
		cmd := exec.Command("nix-instantiate", "--eval", "--strict", "--json", "-E", expr)
		cmd.Dir = S
		out, err := cmd.CombinedOutput()
		return string(out), err
	}

	prelude := `let lib = import <nixpkgs/lib>; luxos = import ./luxos.nix { units = import ./units.nix; };
  eval = mods: lib.evalModules { specialArgs = { inherit luxos; }; modules = [ ./marks.nix ] ++ mods; }; in `

	expectJSON := func(label, expr, want string) {
		t.Run(label, func(t *testing.T) {
			got, err := ev(prelude + expr)
			got = strings.TrimSpace(got)
			if err != nil {
				t.Fatalf("eval error: %v: %s", err, got)
			}
			if got != want {
				t.Errorf("got %s, want %s", got, want)
			}
		})
	}
	expectError := func(label, expr, wantSubstr string) {
		t.Run(label, func(t *testing.T) {
			got, err := ev(prelude + expr)
			if err == nil {
				t.Fatalf("expected an error, evaluated to: %s", got)
			}
			if !strings.Contains(got, wantSubstr) {
				t.Errorf("error %q does not contain %q", got, wantSubstr)
			}
		})
	}

	t.Run("resolution via specialArgs", func(t *testing.T) {
		expectJSON("import by name (transitive: hyprland -> wayland -> desktop)",
			`(eval [ ./config/modules/desktop/compositors/hyprland.nix ]).config.marks`,
			`["desktop","wayland","hyprland"]`)
		expectJSON("folder module resolves to its directory",
			`(eval [ ({ luxos, ... }: { imports = luxos.modules [ "luar" ]; }) ]).config.marks`,
			`["account","luar"]`)
		expectJSON("name with a dash",
			`(eval [ ({ luxos, ... }: { imports = luxos.modules [ "ly-greeter" ]; }) ]).config.marks`,
			`["ly-greeter"]`)
		expectError("unknown name fails loudly",
			`(eval [ ({ luxos, ... }: { imports = luxos.modules [ "waylnd" ]; }) ]).config.marks`,
			`does not resolve to any module under modules/`)
		expectError("a file-inside-a-folder-module is not a name",
			`(eval [ ({ luxos, ... }: { imports = luxos.modules [ "account" ]; }) ]).config.marks`,
			`does not resolve`)
	})

	t.Run("_module.args instead of specialArgs", func(t *testing.T) {
		got, err := ev(`let lib = import <nixpkgs/lib>; luxos = import ` + S + `/luxos.nix { units = import ` + S + `/units.nix; }; in (lib.evalModules { modules = [ ` + S + `/marks.nix { _module.args.luxos = luxos; } ` + M + `/desktop/compositors/hyprland.nix ]; }).config.marks`)
		if err == nil {
			t.Fatalf("expected an error, got: %s", got)
		}
		if !strings.Contains(got, "infinite recursion") && !strings.Contains(got, "attribute 'luxos' missing") {
			t.Errorf("unexpected error: %s", got)
		}
	})

	t.Run("dedup: selected by path AND pulled in by name (order-insensitive)", func(t *testing.T) {
		sorted := `builtins.sort builtins.lessThan `
		expectJSON("same module twice runs once",
			sorted+`(eval [ ./config/modules/system/wayland.nix ./config/modules/desktop/compositors/hyprland.nix ]).config.marks`,
			`["desktop","hyprland","wayland"]`)
		expectJSON("lexically different spelling of the same path still dedupes",
			sorted+`(eval [ ./config/modules/desktop/../system/wayland.nix ./config/modules/desktop/compositors/hyprland.nix ]).config.marks`,
			`["desktop","hyprland","wayland"]`)
	})

	t.Run("cycles are a native module-system limit, not introduced by the function", func(t *testing.T) {
		write(t, filepath.Join(M, "cyc/p.nix"), `{ imports = [ ./q.nix ]; config.marks = [ "p" ]; }`)
		write(t, filepath.Join(M, "cyc/q.nix"), `{ imports = [ ./p.nix ]; config.marks = [ "q" ]; }`)
		expectError("plain-path cycle p <-> q overflows",
			`(eval [ ./config/modules/cyc/p.nix ]).config.marks`, "max-call-depth exceeded")
		expectError("name cycle a <-> b overflows identically",
			`(eval [ ./config/modules/cyc/a.nix ]).config.marks`, "max-call-depth exceeded")
	})

	t.Run("module that needs luxos, evaluated without it (--bypass shape)", func(t *testing.T) {
		got, err := ev(`let lib = import <nixpkgs/lib>; in (lib.evalModules { modules = [ ` + S + `/marks.nix ` + M + `/desktop/compositors/hyprland.nix ]; }).config.marks`)
		if err == nil {
			t.Fatalf("expected an error, got: %s", got)
		}
		if !strings.Contains(got, "luxos") {
			t.Errorf("error should name the missing argument: %s", got)
		}
	})
}

//============================================================================
// implementation-plan.md §6 Verification — items relevant to refs:
//   13. luxos.modules [ "waylnd" ] -> hard error naming the file and name,
//       before staging.
//   14. lux module rename with dependents -> both module files read
//       luxos.modules [ "wl" ], rebuild succeeds.
// (Numbered 14-21 in the phase brief; the plan's own §6 list only runs 1-15
// — see final report for this discrepancy. 13 and 14 are the items that
// concern refs; the rest of that range govern other packages/phases.)
//============================================================================

func TestVerification_UnresolvedNameNamesFileAndName(t *testing.T) {
	skipIfNoNix(t)
	mods := t.TempDir()
	write(t, filepath.Join(mods, "default.nix"), `{ imports = [ ./a.nix ]; }`)
	write(t, filepath.Join(mods, "a.nix"), `{ luxos, ... }: { imports = luxos.modules [ "waylnd" ]; }`)

	us, err := units.Walk(mods, "")
	if err != nil {
		t.Fatal(err)
	}
	violations, err := Validate(mods, us, []string{"a.nix"})
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	found := false
	for _, v := range violations {
		if v.File == "a.nix" && strings.Contains(v.Message, "'waylnd'") && strings.Contains(v.Message, "does not resolve") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected an unresolved-name violation naming a.nix and 'waylnd', got %v", violations)
	}
}

func TestVerification_RenameWithDependentsRewritesBothFiles(t *testing.T) {
	skipIfNoNix(t)
	mods := t.TempDir()
	write(t, filepath.Join(mods, "default.nix"), `{ imports = [ ./a.nix ./b.nix ./wayland.nix ]; }`)
	write(t, filepath.Join(mods, "a.nix"), `{ luxos, ... }: { imports = luxos.modules [ "wayland" ]; }`)
	write(t, filepath.Join(mods, "b.nix"), `{ luxos, ... }: { imports = luxos.modules [ "wayland" ]; }`)
	write(t, filepath.Join(mods, "wayland.nix"), `{ }`)

	changes, err := Retarget(mods, nil, "wayland", "wl")
	if err != nil {
		t.Fatalf("Retarget: %v", err)
	}
	if len(changes) != 2 {
		t.Fatalf("expected 2 changes, got %d: %v", len(changes), changes)
	}
	for _, f := range []string{"a.nix", "b.nix"} {
		names, err := Names(filepath.Join(mods, f))
		if err != nil {
			t.Fatalf("Names(%s): %v", f, err)
		}
		if len(names) != 1 || names[0] != "wl" {
			t.Errorf("%s: names = %v, want [wl]", f, names)
		}
	}
}

//============================================================================
// Suite: Phase 4 — per-host local scoping and the shared/local boundary
// rule (L7, L8).
//============================================================================

func TestDependents_ScansLocalModulesDirs(t *testing.T) {
	skipIfNoNix(t)
	F := t.TempDir()
	mods := filepath.Join(F, "modules")
	local1 := filepath.Join(F, "local", "host1", "modules")
	local2 := filepath.Join(F, "local", "host2", "modules")
	mustMkdirAll(t, mods)

	write(t, filepath.Join(mods, "shared.nix"), `{ }`)
	write(t, filepath.Join(local1, "a.nix"), `{ luxos, ... }: { imports = luxos.modules [ "shared" ]; }`)
	write(t, filepath.Join(local2, "b.nix"), `{ luxos, ... }: { imports = luxos.modules [ "other" ]; }`)

	got, err := Dependents(mods, []string{local1, local2}, "shared")
	if err != nil {
		t.Fatalf("Dependents: %v", err)
	}
	want := filepath.Join(local1, "a.nix")
	if len(got) != 1 || got[0] != want {
		t.Errorf("Dependents = %v, want [%s]", got, want)
	}
}

func TestDependents_MissingLocalModulesDirSkipped(t *testing.T) {
	skipIfNoNix(t)
	F := t.TempDir()
	mods := filepath.Join(F, "modules")
	mustMkdirAll(t, mods)
	write(t, filepath.Join(mods, "shared.nix"), `{ }`)

	missing := filepath.Join(F, "local", "no-such-host", "modules")
	if _, err := Dependents(mods, []string{missing}, "shared"); err != nil {
		t.Fatalf("Dependents: want no error for a missing localModulesDirs entry, got %v", err)
	}
}

func TestRetarget_ScopedToOneHostsLocalModulesDir(t *testing.T) {
	skipIfNoNix(t)
	F := t.TempDir()
	mods := filepath.Join(F, "modules")
	local1 := filepath.Join(F, "local", "host1", "modules")
	mustMkdirAll(t, mods)

	write(t, filepath.Join(mods, "default.nix"), `{ imports = [ ./shared.nix ]; }`)
	write(t, filepath.Join(mods, "shared.nix"), `{ }`)
	aFile := write(t, filepath.Join(local1, "a.nix"), `{ luxos, ... }: { imports = luxos.modules [ "shared" ]; }`)

	// A rename scoped to host1's local dir only.
	changes, err := Retarget(mods, []string{local1}, "shared", "renamed-shared")
	if err != nil {
		t.Fatalf("Retarget: %v", err)
	}
	if len(changes) != 1 || changes[0].File != aFile {
		t.Fatalf("changes = %v, want exactly one change to %s", changes, aFile)
	}
	names, err := Names(aFile)
	if err != nil {
		t.Fatalf("Names: %v", err)
	}
	if len(names) != 1 || names[0] != "renamed-shared" {
		t.Errorf("names = %v, want [renamed-shared]", names)
	}
}

func TestValidate_SharedModuleReferencingLocalModuleIsAViolation(t *testing.T) {
	skipIfNoNix(t)
	mods := t.TempDir()

	write(t, filepath.Join(mods, "default.nix"), `{ imports = [ ./shared.nix ]; }`)
	write(t, filepath.Join(mods, "shared.nix"), `{ luxos, ... }: { imports = luxos.modules [ "priv" ]; }`)

	local := filepath.Join(mods, "local")
	write(t, filepath.Join(local, "priv.nix"), `{ }`)

	us, err := units.Walk(mods, local)
	if err != nil {
		t.Fatalf("units.Walk: %v", err)
	}
	violations, err := Validate(mods, us, []string{"shared.nix"})
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	found := false
	for _, v := range violations {
		if v.File == "shared.nix" && strings.Contains(v.Message, "shared module references local module 'priv'") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected a shared-references-local violation, got %v", violations)
	}
}

func TestValidate_LocalModuleReferencingSharedModuleIsFine(t *testing.T) {
	skipIfNoNix(t)
	mods := t.TempDir()

	write(t, filepath.Join(mods, "default.nix"), `{ imports = [ ./local/priv.nix ]; }`)
	write(t, filepath.Join(mods, "shared.nix"), `{ }`)

	local := filepath.Join(mods, "local")
	write(t, filepath.Join(local, "priv.nix"), `{ luxos, ... }: { imports = luxos.modules [ "shared" ]; }`)

	us, err := units.Walk(mods, local)
	if err != nil {
		t.Fatalf("units.Walk: %v", err)
	}
	violations, err := Validate(mods, us, []string{"local/priv.nix"})
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	for _, v := range violations {
		if strings.Contains(v.Message, "shared module references local module") {
			t.Errorf("unexpected boundary violation for local -> shared reference: %v", v)
		}
	}
}
