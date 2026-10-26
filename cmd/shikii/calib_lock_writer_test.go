package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 0.9.7: the write deadline of a reply that waits for the calibration lock is extended by the lock itself (lockCalibration), not by a list
// of routes. Before, each audit found a route that reached the lock without being on the list, or a check that missed one. Two rules keep it:
// only lockCalibration touches calibMu, and a nil writer (no reply waits) is passed only inside a go statement.

func packageSources(t *testing.T) map[string]string {
	t.Helper()
	files, _ := filepath.Glob("*.go")
	out := map[string]string{}
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		b, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		out[file] = string(b)
	}
	return out
}

// calibMuUsers returns the functions that touch calibMu.
func calibMuUsers(t *testing.T, srcs map[string]string) []string {
	t.Helper()
	fset := token.NewFileSet()
	var out []string
	for file, src := range srcs {
		f, err := parser.ParseFile(fset, file, src, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range f.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				if sel, ok := n.(*ast.SelectorExpr); ok && sel.Sel.Name == "calibMu" {
					out = append(out, fn.Name.Name)
				}
				return true
			})
		}
	}
	return out
}

func TestOnlyLockCalibrationTouchesTheCalibrationLock(t *testing.T) {
	users := calibMuUsers(t, packageSources(t))
	if len(users) == 0 {
		t.Fatal("precondition: lockCalibration is found")
	}
	for _, fn := range users {
		if fn != "lockCalibration" {
			t.Errorf("%s touches calibMu: take the lock with defer s.lockCalibration(w)(), which extends the waiting reply's write deadline", fn)
		}
	}
	bad := map[string]string{"bad.go": "package main\nfunc (s *server) handleNew() { s.calibMu.Lock(); defer s.calibMu.Unlock() }\n"}
	if got := calibMuUsers(t, bad); len(got) != 2 || got[0] != "handleNew" {
		t.Fatalf("negative example: a lock taken outside lockCalibration is found: %v", got)
	}
}

// nilWriters returns the calls that pass nil as an http.ResponseWriter parameter outside a go statement.
func nilWriters(t *testing.T, srcs map[string]string) []string {
	t.Helper()
	fset := token.NewFileSet()
	var parsed []*ast.File
	writerAt := map[string]int{} // function name → index of its http.ResponseWriter parameter
	for file, src := range srcs {
		f, err := parser.ParseFile(fset, file, src, 0)
		if err != nil {
			t.Fatal(err)
		}
		parsed = append(parsed, f)
		for _, d := range f.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok {
				continue
			}
			i := 0
			for _, p := range fn.Type.Params.List {
				if sel, ok := p.Type.(*ast.SelectorExpr); ok && sel.Sel.Name == "ResponseWriter" {
					writerAt[fn.Name.Name] = i
				}
				i += max(1, len(p.Names))
			}
		}
	}
	var out []string
	for _, f := range parsed {
		var walk func(n ast.Node, inGo bool)
		walk = func(n ast.Node, inGo bool) {
			ast.Inspect(n, func(m ast.Node) bool {
				if g, ok := m.(*ast.GoStmt); ok && !inGo {
					walk(g.Call, true)
					return false
				}
				c, ok := m.(*ast.CallExpr)
				if !ok || inGo {
					return true
				}
				var name string
				switch fun := c.Fun.(type) {
				case *ast.SelectorExpr:
					name = fun.Sel.Name
				case *ast.Ident:
					name = fun.Name
				}
				if i, ok := writerAt[name]; ok && i < len(c.Args) {
					if id, ok := c.Args[i].(*ast.Ident); ok && id.Name == "nil" {
						out = append(out, name)
					}
				}
				return true
			})
		}
		walk(f, false)
	}
	return out
}

func TestOnlyBackgroundWorkWaitsForTheLockWithoutAWriter(t *testing.T) {
	for _, name := range nilWriters(t, packageSources(t)) {
		t.Errorf("%s is called with a nil writer outside a go statement: a reply waiting for the lock must pass its writer", name)
	}
	bad := map[string]string{"bad.go": "package main\nimport \"net/http\"\nfunc (s *server) lockCalibration(w http.ResponseWriter) func() { return nil }\nfunc (s *server) handleA() { defer s.lockCalibration(nil)(); go func() { s.lockCalibration(nil)() }() }\n"}
	if got := nilWriters(t, bad); len(got) != 1 || got[0] != "lockCalibration" {
		t.Fatalf("negative example: a nil writer outside a go statement is found, inside one it is not: %v", got)
	}
}
