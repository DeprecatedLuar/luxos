package nix

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func writeNix(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestReadOptions_Leaves(t *testing.T) {
	skipIfNoNix(t)
	dir := t.TempDir()
	good := writeNix(t, dir, "good.nix", `{ lib, ... }: {
  options.laptop.mode = lib.mkOption { type = lib.types.enum [ "a" "b" ]; default = "a"; };
  options.laptop.limit = lib.mkOption { type = lib.types.int; default = 80; };
  options.laptop.list = lib.mkOption { type = lib.types.anything; default = [ { a = "x;y"; } ]; };
}`)
	got, broken, err := ReadOptions(good)
	if err != nil || len(broken) > 0 {
		t.Fatalf("ReadOptions: %v %v", err, broken)
	}
	f := got[good]
	if !reflect.DeepEqual(f.Top, []string{"options"}) {
		t.Errorf("Top = %v", f.Top)
	}
	want := map[string]any{"laptop.limit": float64(80), "laptop.list": []any{map[string]any{"a": "x;y"}}, "laptop.mode": "a"}
	if len(f.Options) != len(want) {
		t.Fatalf("Options = %+v", f.Options)
	}
	for _, o := range f.Options {
		key := strings.Join(o.Path, ".")
		if !o.Option || !o.HasType || !o.HasDefault || !o.PlainDefault || !reflect.DeepEqual(o.Default, want[key]) {
			t.Errorf("%s = %+v, want a plain option defaulting to %v", key, o, want[key])
		}
	}
}

func TestReadOptions_RuleFacts(t *testing.T) {
	skipIfNoNix(t)
	dir := t.TempDir()
	files := map[string]string{
		"config.nix": `{ lib, ... }: { options.laptop.a = lib.mkOption { type = lib.types.int; default = 1; }; config.x = 1; }`,
		"nested.nix": `{ lib, ... }: { options.laptop.a.b = lib.mkOption { type = lib.types.int; default = 1; }; }`,
		"nodef.nix":  `{ lib, ... }: { options.laptop.a = lib.mkOption { type = lib.types.int; }; }`,
		"fn.nix":     `{ lib, ... }: { options.laptop.a = lib.mkOption { type = lib.types.anything; default = x: x; }; }`,
		"path.nix":   `{ lib, ... }: { options.laptop.a = lib.mkOption { type = lib.types.path; default = ./path.nix; }; }`,
		"notopt.nix": `{ lib, ... }: { options.laptop.a = 5; }`,
		"typo.nix":   `{ lib, ... }: { options.laptop.a = lib.mkOption { type = lib.types.inttt; default = 1; }; }`,
	}
	paths := map[string]string{}
	var all []string
	for name, content := range files {
		paths[name] = writeNix(t, dir, name, content)
		all = append(all, paths[name])
	}
	got, broken, err := ReadOptions(all...)
	if err != nil || len(broken) > 0 {
		t.Fatalf("ReadOptions: %v %v", err, broken)
	}
	if top := got[paths["config.nix"]].Top; !reflect.DeepEqual(top, []string{"config", "options"}) {
		t.Errorf("config.nix Top = %v", top)
	}
	if p := got[paths["nested.nix"]].Options[0].Path; !reflect.DeepEqual(p, []string{"laptop", "a", "b"}) {
		t.Errorf("nested.nix path = %v", p)
	}
	if o := got[paths["nodef.nix"]].Options[0]; o.HasDefault {
		t.Errorf("nodef.nix = %+v, want HasDefault false", o)
	}
	for _, name := range []string{"fn.nix", "path.nix"} {
		if o := got[paths[name]].Options[0]; !o.HasDefault || o.PlainDefault {
			t.Errorf("%s = %+v, want a default that is not plain", name, o)
		}
	}
	if o := got[paths["notopt.nix"]].Options[0]; o.Option {
		t.Errorf("notopt.nix = %+v, want Option false", o)
	}
	if o := got[paths["typo.nix"]].Options[0]; !o.Option || !o.HasType {
		t.Errorf("typo.nix = %+v: the type is never evaluated", o)
	}
}

func TestReadOptions_BrokenFiles(t *testing.T) {
	skipIfNoNix(t)
	dir := t.TempDir()
	ok := writeNix(t, dir, "ok.nix", `{ lib, ... }: { options.laptop.a = lib.mkOption { type = lib.types.int; default = 1; }; }`)
	pkgs := writeNix(t, dir, "pkgs.nix", `{ lib, pkgs, ... }: { options.laptop.a = lib.mkOption { type = lib.types.package; default = pkgs.hello; }; }`)
	libCall := writeNix(t, dir, "lib.nix", `{ lib, ... }: { options.laptop.a = lib.mkOption { type = lib.types.str; default = lib.concatStrings [ "a" ]; }; }`)
	got, broken, err := ReadOptions(ok, pkgs, libCall)
	if err != nil {
		t.Fatalf("ReadOptions: %v", err)
	}
	if _, has := got[ok]; !has {
		t.Errorf("ok.nix missing from %v", got)
	}
	if !strings.Contains(broken[pkgs], "pkgs") {
		t.Errorf("pkgs.nix broken = %q, want nix's missing-argument message", broken[pkgs])
	}
	if !strings.Contains(broken[libCall], "concatStrings") {
		t.Errorf("lib.nix broken = %q, want nix's missing-attribute message", broken[libCall])
	}
}

func TestReadOptions_Description(t *testing.T) {
	skipIfNoNix(t)
	dir := t.TempDir()
	f := writeNix(t, dir, "d.nix", `{ lib, ... }: {
  options.laptop.mode = lib.mkOption { type = lib.types.str; default = "a"; description = "a | b"; };
  options.laptop.limit = lib.mkOption { type = lib.types.int; default = 80; };
}`)
	got, broken, err := ReadOptions(f)
	if err != nil || len(broken) > 0 {
		t.Fatalf("ReadOptions: %v %v", err, broken)
	}
	desc := map[string]string{}
	for _, o := range got[f].Options {
		desc[strings.Join(o.Path, ".")] = o.Description
	}
	if desc["laptop.mode"] != "a | b" || desc["laptop.limit"] != "" {
		t.Errorf("descriptions = %v", desc)
	}
}
