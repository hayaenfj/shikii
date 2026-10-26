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

// handlerRegistrations are the calls a writer-taking function may be passed to as a value: net/http gives it the connection's writer.
var handlerRegistrations = map[string]bool{"HandleFunc": true, "HandlerFunc": true}

// nilWriters returns "function → callee" for every call that passes something other than the enclosing function's writer parameter to a
// function's http.ResponseWriter parameter, except a literal nil at an allowed place inside a go statement. It also returns the ways around
// that check (0.9.7 re-audit C-4): a writer parameter that is assigned, a writer-taking function used as a value outside a handler
// registration, and an immediately called function literal (checked like any call). A type alias of http.ResponseWriter is a writer too.
func nilWriters(t *testing.T, srcs map[string]string) []string {
	t.Helper()
	fset := token.NewFileSet()
	var parsed []*ast.File
	writerTypes := map[string]bool{} // local names of http.ResponseWriter (type aliases and defined types)
	for file, src := range srcs {
		f, err := parser.ParseFile(fset, file, src, 0)
		if err != nil {
			t.Fatal(err)
		}
		parsed = append(parsed, f)
		ast.Inspect(f, func(n ast.Node) bool {
			if ts, ok := n.(*ast.TypeSpec); ok {
				if sel, ok := ts.Type.(*ast.SelectorExpr); ok && sel.Sel.Name == "ResponseWriter" {
					writerTypes[ts.Name.Name] = true
				}
			}
			return true
		})
	}
	isWriter := func(e ast.Expr) bool {
		switch x := e.(type) {
		case *ast.SelectorExpr:
			return x.Sel.Name == "ResponseWriter"
		case *ast.Ident:
			return writerTypes[x.Name]
		}
		return false
	}
	writerIndex := func(ft *ast.FuncType) int {
		i := 0
		for _, p := range ft.Params.List {
			if isWriter(p.Type) {
				return i
			}
			i += max(1, len(p.Names))
		}
		return -1
	}
	// a handler type: http.HandlerFunc, or a function type taking a writer
	isHandlerType := func(e ast.Expr) bool {
		switch x := e.(type) {
		case *ast.SelectorExpr:
			return x.Sel.Name == "HandlerFunc"
		case *ast.Ident:
			return x.Name == "HandlerFunc"
		case *ast.FuncType:
			return writerIndex(x) >= 0
		}
		return false
	}
	writerAt := map[string]int{}               // function name → index of its http.ResponseWriter parameter
	handlerParams := map[string]map[int]bool{} // function name → indexes of its handler-typed parameters (a middleware)
	for _, f := range parsed {
		for _, d := range f.Decls {
			if fn, ok := d.(*ast.FuncDecl); ok {
				if i := writerIndex(fn.Type); i >= 0 {
					writerAt[fn.Name.Name] = i
				}
				i := 0
				for _, p := range fn.Type.Params.List {
					for k := 0; k < max(1, len(p.Names)); k++ {
						if isHandlerType(p.Type) {
							if handlerParams[fn.Name.Name] == nil {
								handlerParams[fn.Name.Name] = map[int]bool{}
							}
							handlerParams[fn.Name.Name][i] = true
						}
						i++
					}
				}
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
			// a writer parameter that is assigned (by =, :=, range or through its address) is no longer the connection's writer
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				switch x := n.(type) {
				case *ast.RangeStmt:
					for _, e := range []ast.Expr{x.Key, x.Value} {
						if id, ok := e.(*ast.Ident); ok && own[id.Name] {
							out = append(out, fn.Name.Name+" assigns its writer "+id.Name+" by range")
						}
					}
				case *ast.UnaryExpr:
					if id, ok := x.X.(*ast.Ident); ok && x.Op == token.AND && own[id.Name] {
						out = append(out, fn.Name.Name+" takes the address of its writer "+id.Name)
					}
				case *ast.AssignStmt:
					for _, l := range x.Lhs {
						if id, ok := l.(*ast.Ident); ok && own[id.Name] {
							out = append(out, fn.Name.Name+" assigns its writer "+id.Name)
						}
					}
				case *ast.ValueSpec:
					for _, id := range x.Names {
						if own[id.Name] {
							out = append(out, fn.Name.Name+" declares another "+id.Name)
						}
					}
				}
				return true
			})
			// a writer-taking function used as a value (not called) can be called with anything later
			called, registered := map[ast.Node]bool{}, map[ast.Node]bool{}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				if c, ok := n.(*ast.CallExpr); ok {
					called[c.Fun] = true
					var callee string
					switch fun := c.Fun.(type) {
					case *ast.SelectorExpr:
						callee = fun.Sel.Name
					case *ast.Ident:
						callee = fun.Name
					}
					for i, a := range c.Args {
						if handlerRegistrations[callee] || handlerParams[callee][i] {
							registered[a] = true
						}
					}
				}
				return true
			})
			// a function literal taking a writer can be called with anything: only as a handler registration argument or a returned handler
			returned := map[ast.Node]bool{}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				if r, ok := n.(*ast.ReturnStmt); ok {
					for _, e := range r.Results {
						returned[e] = true
						if c, ok := e.(*ast.CallExpr); ok && len(c.Args) == 1 { // return http.HandlerFunc(func(w, r) {...})
							returned[c.Args[0]] = true
						}
					}
				}
				return true
			})
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				if lit, ok := n.(*ast.FuncLit); ok && writerIndex(lit.Type) >= 0 && !registered[n] && !returned[n] {
					out = append(out, fn.Name.Name+" has a function literal taking a writer outside a handler registration")
				}
				return true
			})
			inner := map[ast.Node]bool{} // the name inside x.name, judged with the selector itself
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				var name string
				switch x := n.(type) {
				case *ast.SelectorExpr:
					name = x.Sel.Name
					inner[x.Sel] = true
				case *ast.Ident:
					if inner[x] {
						return true
					}
					name = x.Name
				default:
					return true
				}
				if _, ok := writerAt[name]; ok && !called[n] && !registered[n] {
					out = append(out, fn.Name.Name+" uses "+name+" as a value")
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
					i := -1
					switch fun := c.Fun.(type) {
					case *ast.SelectorExpr:
						name = fun.Sel.Name
					case *ast.Ident:
						name = fun.Name
					case *ast.FuncLit:
						name, i = "a function literal", writerIndex(fun.Type)
					}
					if j, ok := writerAt[name]; ok {
						i = j
					}
					if i < 0 || i >= len(c.Args) {
						return true
					}
					id, _ := c.Args[i].(*ast.Ident)
					switch {
					case id != nil && own[id.Name] && id.Name != "nil":
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
