package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// calibLockAllowed are the only functions that take the calibration lock. Each runs under a route in longRoutes (or a background goroutine
// started from one), so a reply that waits for the lock is not cut by the HTTP write timeout. A new function that takes the lock must have its
// route added to longRoutes and its name added here (0.9.6 audit of the HTTP limits).
var calibLockAllowed = map[string]bool{"runTuneCaptured": true, "handleTuneVariant": true, "handleTuneAdopt": true, "handleTuneReview": true, "maybeCalibrate": true}

// calibLockHolders returns the functions in src that call calibMu.Lock.
func calibLockHolders(t *testing.T, fset *token.FileSet, name string, src any) []string {
	t.Helper()
	f, err := parser.ParseFile(fset, name, src, 0)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, d := range f.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			if sel, ok := n.(*ast.SelectorExpr); ok && sel.Sel.Name == "Lock" {
				if inner, ok := sel.X.(*ast.SelectorExpr); ok && inner.Sel.Name == "calibMu" {
					out = append(out, fn.Name.Name)
				}
			}
			return true
		})
	}
	return out
}

func TestOnlyAllowedFunctionsTakeTheCalibrationLock(t *testing.T) {
	fset := token.NewFileSet()
	files, _ := filepath.Glob("*.go")
	seen := map[string]bool{}
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		src, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		for _, fn := range calibLockHolders(t, fset, file, src) {
			seen[fn] = true
			if !calibLockAllowed[fn] {
				t.Errorf("%s takes the calibration lock but is not in calibLockAllowed: register its route through longWork (longRoutes) and add it here", fn)
			}
		}
	}
	var got []string
	for fn := range seen {
		got = append(got, fn)
	}
	sort.Strings(got)
	if len(got) != len(calibLockAllowed) {
		t.Errorf("calibLockAllowed lists a function that no longer takes the lock (keep the list exact): found %v", got)
	}
	// negative example: a function outside the list that takes the lock is found
	bad := "package main\nfunc (s *server) handleNew() { s.calibMu.Lock(); defer s.calibMu.Unlock() }\n"
	if holders := calibLockHolders(t, fset, "bad.go", bad); len(holders) != 1 || calibLockAllowed[holders[0]] {
		t.Fatalf("the check finds a lock taken outside the list: %v", holders)
	}
}
