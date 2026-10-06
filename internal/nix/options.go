package nix

import (
	"encoding/json"
	"fmt"
)

// evalOptionsExpr evaluates each options.nix with a stub lib: mkOption only
// tags its argument and types is empty, so a type is never evaluated, only
// seen. Every leaf under options is reported with its path; a tagged leaf
// says whether it has a type and a default and whether the default is plain
// data (no path, function or derivation), which alone is returned as a value.
const evalOptionsExpr = `
{ filesJson }:
let
  files = builtins.fromJSON filesJson;
  lib = { mkOption = o: o // { _luxosOption = true; }; types = { }; };
  plain = v:
    let t = builtins.typeOf v; in
    builtins.elem t [ "null" "bool" "int" "float" "string" ]
    || (t == "list" && builtins.all plain v)
    || (t == "set" && builtins.all plain (builtins.attrValues v));
  leaves = prefix: set: builtins.concatMap (n:
    let v = set.${n}; p = prefix ++ [ n ]; in
    if builtins.isAttrs v && !(v ? _luxosOption) then leaves p v
    else [ ({ path = p; option = builtins.isAttrs v; }
      // (if builtins.isAttrs v then {
        hasType = v ? type;
        hasDefault = v ? default;
        plainDefault = v ? default && plain v.default;
        default = if v ? default && plain v.default then v.default else null;
      } else { })) ]) (builtins.attrNames set);
  readFile = file:
    let
      m = import file;
      r = if builtins.isFunction m then m { inherit lib; } else m;
    in {
      top = builtins.attrNames r;
      options = if r ? options && builtins.isAttrs r.options then leaves [ ] r.options else [ ];
    };
in builtins.listToAttrs (map (file: { name = file; value = readFile file; }) files)
`

// OptionLeaf is one entry under a file's options: a lib.mkOption (Option)
// or a value where one was expected. Default is set only when PlainDefault.
type OptionLeaf struct {
	Path         []string `json:"path"`
	Option       bool     `json:"option"`
	HasType      bool     `json:"hasType"`
	HasDefault   bool     `json:"hasDefault"`
	PlainDefault bool     `json:"plainDefault"`
	Default      any      `json:"default"`
}

// OptionsFile is what an options.nix sets: its top-level names and every
// leaf under options.
type OptionsFile struct {
	Top     []string     `json:"top"`
	Options []OptionLeaf `json:"options"`
}

// ReadOptions evaluates each file with a stub lib, keyed by absolute path. A
// file nix cannot evaluate (a pkgs argument, a lib call, a syntax error) is
// left out and returned in broken with nix's message. Every file must exist.
func ReadOptions(files ...string) (map[string]OptionsFile, map[string]string, error) {
	abs, err := absExisting("nix.ReadOptions", files)
	if err != nil {
		return nil, nil, err
	}
	out := map[string]OptionsFile{}
	broken, err := evalEach(abs, func(batch []string) error {
		if len(batch) == 0 {
			return nil
		}
		payload, err := json.Marshal(batch)
		if err != nil {
			return err
		}
		raw, err := EvalJSON(evalOptionsExpr, map[string]string{"filesJson": string(payload)})
		if err != nil {
			return err
		}
		got := map[string]OptionsFile{}
		if err := json.Unmarshal(raw, &got); err != nil {
			return fmt.Errorf("nix.ReadOptions: parse nix-instantiate output: %w", err)
		}
		out = got
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	return out, broken, nil
}
