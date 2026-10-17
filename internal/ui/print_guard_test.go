package ui

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

const (
	guardRoot = ".."
	outField  = "Out"
)

var (
	printFuncs  = map[string]bool{"Print": true, "Printf": true, "Println": true}
	fprintFuncs = map[string]bool{"Fprint": true, "Fprintf": true, "Fprintln": true}
	writeFuncs  = map[string]bool{"Write": true, "WriteString": true}
)

// TestNoDirectPrinting fails on any print to the terminal in internal/
// outside ui.
func TestNoDirectPrinting(t *testing.T) {
	uiDir := filepath.Join(guardRoot, "ui")
	fset := token.NewFileSet()
	err := filepath.WalkDir(guardRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path == uiDir {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		for _, pos := range directPrints(f) {
			t.Errorf("%s: prints directly; go through ui", fset.Position(pos))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestDirectPrintDetection(t *testing.T) {
	src := `package p
func f() {
	fmt.Println("a")
	fmt.Fprintf(os.Stderr, "b")
	io.WriteString(os.Stdout, "c")
	os.Stdout.Write(nil)
	fmt.Fprintf(&b, "ok")
	fmt.Fprintf(w, "ok")
	os.Stdout.Fd()
	os.Stdout.Stat()
	fmt.Fprintln(w.Out, "d")
}`
	f, err := parser.ParseFile(token.NewFileSet(), "p.go", src, 0)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(directPrints(f)); got != 5 {
		t.Errorf("found %d direct prints, want 5", got)
	}
}

// directPrints returns the position of every call in f that prints to the
// terminal.
func directPrints(f *ast.File) []token.Pos {
	var found []token.Pos
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if isDirectPrint(sel, call.Args) {
			found = append(found, call.Pos())
		}
		return true
	})
	return found
}

func isDirectPrint(sel *ast.SelectorExpr, args []ast.Expr) bool {
	name := sel.Sel.Name
	if isStdStream(sel.X) {
		return writeFuncs[name]
	}
	pkg, ok := sel.X.(*ast.Ident)
	if !ok {
		return false
	}
	switch {
	case pkg.Name == "fmt" && printFuncs[name]:
		return true
	case pkg.Name == "fmt" && fprintFuncs[name], pkg.Name == "io" && name == "WriteString":
		return len(args) > 0 && (isStdStream(args[0]) || isOutField(args[0]))
	}
	return false
}

// isStdStream reports whether e is os.Stdout or os.Stderr.
func isStdStream(e ast.Expr) bool {
	sel, ok := e.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	return ok && pkg.Name == "os" && (sel.Sel.Name == "Stdout" || sel.Sel.Name == "Stderr")
}

// isOutField reports whether e is a field named Out: the writer a command's
// screen is handed, which is os.Stdout outside tests.
func isOutField(e ast.Expr) bool {
	sel, ok := e.(*ast.SelectorExpr)
	return ok && sel.Sel.Name == outField
}
