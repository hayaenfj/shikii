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

// nilWriterAllowed are the only places that pass no writer (nil) to a function taking an http.ResponseWriter: enclosing function → callee,
// and only inside a go statement there (the background tune after the route answered 202). Everywhere else the writer passed must be the
// handler's own writer parameter (0.9.7 audit A-2: a nil held in a variable, or a go statement the reply then waits for, slipped through).
var nilWriterAllowed = map[string]string{"handleTuneRun": "runTuneCaptured"}

// writerArgAllowed are the other writers allowed by name: "function → callee" → the variable passed. MCP calls the handlers in process
// through a recorder, which has no connection and no write deadline.
var writerArgAllowed = map[string]string{"callTool → ServeHTTP": "rec"}

// nilWriters returns "function → callee" for every call that passes something other than the enclosing function's writer parameter to a
// function's http.ResponseWriter parameter, except a literal nil at an allowed place inside a go statement.
func nilWriters(t *testing.T, srcs map[string]string) []string {
	t.Helper()
	fset := token.NewFileSet()
	var parsed []*ast.File
	writerAt := map[string]int{} // function name → index of its http.ResponseWriter parameter
	isWriter := func(e ast.Expr) bool {
		sel, ok := e.(*ast.SelectorExpr)
		return ok && sel.Sel.Name == "ResponseWriter"
	}
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
				if isWriter(p.Type) {
					writerAt[fn.Name.Name] = i
				}
				i += max(1, len(p.Names))
			}
		}
	}
	var out []string
	for _, f := range parsed {
		for _, d := range f.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			own := map[string]bool{} // the writer parameters of this function and of the function literals in it
			ast.Inspect(fn, func(n ast.Node) bool {
				var ft *ast.FuncType
				switch x := n.(type) {
				case *ast.FuncDecl:
					ft = x.Type
				case *ast.FuncLit:
					ft = x.Type
				}
				if ft != nil {
					for _, p := range ft.Params.List {
						if isWriter(p.Type) {
							for _, name := range p.Names {
								own[name.Name] = true
							}
						}
					}
				}
				return true
			})
			var walk func(n ast.Node, inGo bool)
			walk = func(n ast.Node, inGo bool) {
				ast.Inspect(n, func(m ast.Node) bool {
					if g, ok := m.(*ast.GoStmt); ok && !inGo {
						walk(g.Call, true)
						return false
					}
					c, ok := m.(*ast.CallExpr)
					if !ok {
						return true
					}
					var name string
					switch fun := c.Fun.(type) {
					case *ast.SelectorExpr:
						name = fun.Sel.Name
					case *ast.Ident:
						name = fun.Name
					}
					i, ok := writerAt[name]
					if !ok || i >= len(c.Args) {
						return true
					}
					id, _ := c.Args[i].(*ast.Ident)
					switch {
					case id != nil && own[id.Name]:
					case id != nil && id.Name == "nil" && inGo && nilWriterAllowed[fn.Name.Name] == name:
					case id != nil && writerArgAllowed[fn.Name.Name+" → "+name] == id.Name:
					default:
						out = append(out, fn.Name.Name+" → "+name)
					}
					return true
				})
			}
			walk(fn.Body, false)
		}
	}
	return out
}

func TestOnlyBackgroundWorkWaitsForTheLockWithoutAWriter(t *testing.T) {
	for _, call := range nilWriters(t, packageSources(t)) {
		t.Errorf("%s passes something other than the handler's own writer: a reply waiting for the lock must pass its writer (a nil writer only at a place in nilWriterAllowed, inside a go statement)", call)
	}
	allowed := "package main\nimport \"net/http\"\nfunc (s *server) runTuneCaptured(w http.ResponseWriter) {}\nfunc (s *server) handleTuneRun(w http.ResponseWriter) { go func() { s.runTuneCaptured(nil) }(); s.runTuneCaptured(w) }\n"
	if got := nilWriters(t, map[string]string{"ok.go": allowed}); len(got) != 0 {
		t.Fatalf("the allowed background tune and the handler's own writer pass: %v", got)
	}
	for name, src := range map[string]string{
		"nil outside go": "package main\nimport \"net/http\"\nfunc (s *server) lockCalibration(w http.ResponseWriter) func() { return nil }\nfunc (s *server) handleA(w http.ResponseWriter) { defer s.lockCalibration(nil)() }\n",
		"nil in a go statement at a place not allowed": "package main\nimport \"net/http\"\nfunc (s *server) lockCalibration(w http.ResponseWriter) func() { return nil }\nfunc (s *server) handleA(w http.ResponseWriter) { done := make(chan bool); go func() { s.lockCalibration(nil)(); done <- true }(); <-done }\n",
		"nil held in a variable":                       "package main\nimport \"net/http\"\nfunc (s *server) lockCalibration(w http.ResponseWriter) func() { return nil }\nfunc (s *server) handleA(w http.ResponseWriter) { var none http.ResponseWriter; defer s.lockCalibration(none)() }\n",
	} {
		if got := nilWriters(t, map[string]string{"bad.go": src}); len(got) != 1 {
			t.Errorf("negative example %q is found: %v", name, got)
		}
	}
}
