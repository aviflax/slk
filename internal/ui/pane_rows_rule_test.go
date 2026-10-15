package ui

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
)

// paneRowsWriters are the methods that replace a pane's rows wholesale,
// with the index of their rows argument.
var paneRowsWriters = map[string]int{"SetMessages": 0, "PrependMessages": 0, "SetThread": 1}

// paneRowsExempt are the functions that may pass a pane other rows:
// splitWindow seeds a new window with a copy of a pane's rows, which
// went through paneRows when they were set.
var paneRowsExempt = map[string]bool{"splitWindow": true}

// TestPaneRowsGoThroughPaneRows keeps every wholesale replacement of a
// pane's rows going through paneRows (or olderRows for a prepended
// page): the rows come from Slack or its cache, and only those put back
// the user's pending edits, deletes and sends and the ephemerals on
// screen. A replacement that skipped them would show a pending edit's
// old text, bring back a message being deleted, or drop a placeholder.
// Clearing a pane (nil) is allowed anywhere.
func TestPaneRowsGoThroughPaneRows(t *testing.T) {
	paths, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	for _, path := range paths {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, decl := range f.Decls {
			if fn, ok := decl.(*ast.FuncDecl); ok && paneRowsExempt[fn.Name.Name] {
				continue
			}
			ast.Inspect(decl, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				i, writes := paneRowsWriters[sel.Sel.Name]
				if !writes || len(call.Args) <= i || throughPaneRows(call.Args[i]) {
					return true
				}
				t.Errorf("%s: %s with rows not from paneRows/olderRows", fset.Position(call.Pos()), sel.Sel.Name)
				return true
			})
		}
	}
}

// throughPaneRows reports whether rows is nil or a call of paneRows or
// olderRows.
func throughPaneRows(rows ast.Expr) bool {
	if id, ok := rows.(*ast.Ident); ok && id.Name == "nil" {
		return true
	}
	call, ok := rows.(*ast.CallExpr)
	if !ok {
		return false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	return ok && (sel.Sel.Name == "paneRows" || sel.Sel.Name == "olderRows")
}
