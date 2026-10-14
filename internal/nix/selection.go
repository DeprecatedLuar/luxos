package nix

import (
	"encoding/json"
	"fmt"
	"path/filepath"
)

// selectionShapeExpr evaluates a selection file with every module argument
// stubbed to throw, and reports its top-level names and how many entries its
// imports list holds (-1 when there is no imports list). The entries are
// never forced.
const selectionShapeExpr = `
{ file }:
let
  m = import file;
  isFn = builtins.isFunction m;
  args = if isFn then
    builtins.mapAttrs (n: _: throw "a selection cannot use the module argument '${n}'") (builtins.functionArgs m)
  else {};
  r = if isFn then m args else m;
  isSet = builtins.isAttrs r;
in {
  attrs = if isSet then builtins.attrNames r else [];
  imports = if isSet && r ? imports && builtins.isList r.imports then builtins.length r.imports else -1;
}
`

// SelectionShape is what a selection file evaluates to: its top-level
// attribute names, sorted, and the length of its imports list.
type SelectionShape struct {
	Attrs   []string `json:"attrs"`
	Imports int      `json:"imports"`
}

// ReadSelectionShape evaluates file and returns its SelectionShape.
func ReadSelectionShape(file string) (SelectionShape, error) {
	abs, err := filepath.Abs(file)
	if err != nil {
		return SelectionShape{}, err
	}
	out, err := EvalJSON(selectionShapeExpr, map[string]string{"file": abs})
	if err != nil {
		return SelectionShape{}, fmt.Errorf("%s: %s", file, nixMessage(err))
	}
	var s SelectionShape
	if err := json.Unmarshal(out, &s); err != nil {
		return SelectionShape{}, fmt.Errorf("nix.ReadSelectionShape: parse nix-instantiate output: %w", err)
	}
	return s, nil
}
