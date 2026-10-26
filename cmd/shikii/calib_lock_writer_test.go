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

// lockCallers is the allowlist of the places that call the three functions waiting for the calibration lock (0.9.7, after four audits
// found ways around a general check of every writer-taking function): callee → calling function → how the writer is passed.
// "w" is the calling function's own http.ResponseWriter parameter, never assigned, ranged over or taken by address in it; "nil in go" is a
// literal nil inside a go statement (no reply waits). Any other mention of these names, anywhere in the package (a value, a function
// literal, a package-level variable, another caller), fails. A new path that waits for the lock is added here on purpose.
// Every function that waits for the lock, directly or through another, is a key, so its own callers are bound all the way up (audit F-1,
// F-3); a handler is reached only through its HTTP registration ("registered": passed to HandleFunc in that function).
var lockCallers = map[string]map[string]string{
	"lockCalibration":   {"runTuneCaptured": "w", "maybeCalibrate": "w", "handleTuneVariant": "w", "handleTuneAdopt": "w", "handleTuneReview": "w"},
	"runTuneCaptured":   {"handleTuneSync": "w", "handleTuneRun": "nil in go"},
	"maybeCalibrate":    {"handleLabel": "w"},
	"handleLabel":       {"handler": "registered"},
	"handleTuneSync":    {"handler": "registered"},
	"handleTuneRun":     {"handler": "registered"},
	"handleTuneVariant": {"handler": "registered"},
	"handleTuneAdopt":   {"handler": "registered"},
	"handleTuneReview":  {"handler": "registered"},
}

// serveHTTPAllowed are the only places that run an http.Handler with a writer other than their own: function → the variable passed.
// MCP calls the server in process through a recorder, which has no write deadline (audit G-1: without this, running the whole server
// with a recorder or nil reached the listed handlers again, above the registration).
var serveHTTPAllowed = map[string]string{"callTool": "rec"}

// nilWriters returns the places that mention a lock-waiting function other than as allowed by lockCallers, and the places that run an
// http.Handler (ServeHTTP) with a writer other than their own outside serveHTTPAllowed.
// What it covers: every path inside this package from a connection to the calibration lock. What it does not: code outside the package
// (there is none that imports package main) and what a handler does at run time beyond these calls (the lock itself extends the deadline
// of the writer it is given; a recorder has none).
func nilWriters(t *testing.T, srcs map[string]string) []string {
	t.Helper()
	fset := token.NewFileSet()
	var out []string
	unparen := func(e ast.Expr) ast.Expr {
		for {
			p, ok := e.(*ast.ParenExpr)
			if !ok {
				return e
			}
			e = p.X
		}
	}
	for file, src := range srcs {
		f, err := parser.ParseFile(fset, file, src, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range f.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok {
				// package-level declarations: any mention is outside the allowlist
				ast.Inspect(d, func(n ast.Node) bool {
					if id, ok := n.(*ast.Ident); ok && lockCallers[id.Name] != nil {
						out = append(out, "a package-level declaration mentions "+id.Name)
					}
					return true
				})
				continue
			}
			if fn.Body == nil {
				continue
			}
			onServer := false // a method of *server (the registration counts only in (*server).handler; audit G-1)
			if fn.Recv != nil && len(fn.Recv.List) == 1 {
				if st, ok := fn.Recv.List[0].Type.(*ast.StarExpr); ok {
					if id, ok := st.X.(*ast.Ident); ok && id.Name == "server" {
						onServer = true
					}
				}
			}
			writer := "" // the function's own http.ResponseWriter parameter
			for _, p := range fn.Type.Params.List {
				if sel, ok := p.Type.(*ast.SelectorExpr); ok && sel.Sel.Name == "ResponseWriter" && len(p.Names) == 1 {
					writer = p.Names[0].Name
				}
			}
			changed := false // the writer parameter is assigned, ranged over or taken by address
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				is := func(e ast.Expr) bool {
					id, ok := unparen(e).(*ast.Ident)
					return ok && writer != "" && id.Name == writer
				}
				switch x := n.(type) {
				case *ast.AssignStmt:
					for _, l := range x.Lhs {
						changed = changed || is(l)
					}
				case *ast.RangeStmt:
					changed = changed || (x.Key != nil && is(x.Key)) || (x.Value != nil && is(x.Value))
				case *ast.UnaryExpr:
					changed = changed || (x.Op == token.AND && is(x.X))
				case *ast.ValueSpec:
					for _, id := range x.Names {
						changed = changed || id.Name == writer
					}
				}
				return true
			})
			// the writer parameters of the function literals in it (a returned handler such as http.HandlerFunc(func(w, r) {...})), unless
			// assigned, ranged over or taken by address
			litWriters := map[string]bool{}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				if lit, ok := n.(*ast.FuncLit); ok {
					for _, p := range lit.Type.Params.List {
						if sel, ok := p.Type.(*ast.SelectorExpr); ok && sel.Sel.Name == "ResponseWriter" {
							for _, id := range p.Names {
								litWriters[id.Name] = true
							}
						}
					}
				}
				return true
			})
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				drop := func(e ast.Expr) {
					if id, ok := unparen(e).(*ast.Ident); ok {
						delete(litWriters, id.Name)
					}
				}
				switch x := n.(type) {
				case *ast.AssignStmt:
					for _, l := range x.Lhs {
						drop(l)
					}
				case *ast.RangeStmt:
					if x.Key != nil {
						drop(x.Key)
					}
					if x.Value != nil {
						drop(x.Value)
					}
				case *ast.UnaryExpr:
					if x.Op == token.AND {
						drop(x.X)
					}
				}
				return true
			})
			allowedCall := map[ast.Node]bool{}
			var walk func(n ast.Node, inGo, inLit bool)
			walk = func(n ast.Node, inGo, inLit bool) {
				ast.Inspect(n, func(m ast.Node) bool {
					switch x := m.(type) {
					case *ast.GoStmt:
						if !inGo {
							walk(x.Call, true, inLit)
							return false
						}
					case *ast.FuncLit:
						if !inLit {
							walk(x.Body, inGo, true)
							return false
						}
					case *ast.CallExpr:
						var name ast.Node
						var callee string
						switch fun := x.Fun.(type) {
						case *ast.SelectorExpr:
							name, callee = fun.Sel, fun.Sel.Name
						case *ast.Ident:
							name, callee = fun, fun.Name
						}
						if callee == "ServeHTTP" && len(x.Args) > 0 { // running a handler: with its own writer, or at an allowed place
							arg, _ := unparen(x.Args[0]).(*ast.Ident)
							ownWriter := arg != nil && (writer != "" && arg.Name == writer && !changed || litWriters[arg.Name])
							allowedArg := arg != nil && serveHTTPAllowed[fn.Name.Name] == arg.Name
							if !ownWriter && !allowedArg {
								out = append(out, fn.Name.Name+" runs ServeHTTP with a writer other than its own")
							}
						}
						mux := false
						if sel, ok := x.Fun.(*ast.SelectorExpr); ok {
							if id, ok := sel.X.(*ast.Ident); ok && id.Name == "mux" {
								mux = true
							}
						}
						if callee == "HandleFunc" && mux && onServer && !inGo && !inLit { // a registration: the handler gets the connection's writer
							for _, a := range x.Args {
								var arg *ast.Ident
								switch v := unparen(a).(type) {
								case *ast.SelectorExpr:
									arg = v.Sel
								case *ast.Ident:
									arg = v
								}
								if arg != nil && lockCallers[arg.Name][fn.Name.Name] == "registered" {
									allowedCall[arg] = true
								}
							}
						}
						how, listed := lockCallers[callee][fn.Name.Name]
						if !listed || how == "registered" || len(x.Args) == 0 {
							return true
						}
						arg, _ := unparen(x.Args[0]).(*ast.Ident)
						switch {
						case how == "w" && !inGo && !inLit && arg != nil && writer != "" && arg.Name == writer && !changed:
							allowedCall[name] = true
						case how == "nil in go" && inGo && arg != nil && arg.Name == "nil":
							allowedCall[name] = true
						}
					}
					return true
				})
			}
			walk(fn.Body, false, false)
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				if id, ok := n.(*ast.Ident); ok && lockCallers[id.Name] != nil && !allowedCall[n] {
					out = append(out, fn.Name.Name+" mentions "+id.Name+" outside lockCallers")
				}
				return true
			})
		}
	}
	return out
}

func TestOnlyBackgroundWorkWaitsForTheLockWithoutAWriter(t *testing.T) {
	for _, call := range nilWriters(t, packageSources(t)) {
		t.Errorf("%s: a reply waiting for the calibration lock must pass its own writer from a place listed in lockCallers", call)
	}
	allowed := "package main\nimport \"net/http\"\nfunc (s *server) runTuneCaptured(w http.ResponseWriter) { defer s.lockCalibration(w)() }\nfunc (s *server) handleTuneRun(w http.ResponseWriter) { go func() { s.runTuneCaptured(nil) }() }\nfunc (s *server) handleTuneSync(w http.ResponseWriter) { s.runTuneCaptured(w) }\n"
	if got := nilWriters(t, map[string]string{"ok.go": allowed}); len(got) != 0 {
		t.Fatalf("the allowed places pass: %v", got)
	}
	for name, src := range map[string]string{
		"a caller not in the list":   "package main\nimport \"net/http\"\nfunc (s *server) handleA(w http.ResponseWriter) { defer s.lockCalibration(w)() }\n",
		"nil outside a go statement": "package main\nimport \"net/http\"\nfunc (s *server) handleTuneRun(w http.ResponseWriter) { s.runTuneCaptured(nil) }\n",
		"the writer changed first":   "package main\nimport \"net/http\"\nfunc (s *server) handleTuneSync(w http.ResponseWriter) { w = nil; s.runTuneCaptured(w) }\n",
		"a value instead of a call":  "package main\nimport \"net/http\"\nfunc (s *server) handleTuneSync(w http.ResponseWriter) { f := s.runTuneCaptured; f(w) }\n",
		"inside a function literal":  "package main\nimport \"net/http\"\nfunc (s *server) handleTuneSync(w http.ResponseWriter) { func() { s.runTuneCaptured(w) }() }\n",
		"a package-level variable":   "package main\nvar g = (*server).lockCalibration\n",
	} {
		if got := nilWriters(t, map[string]string{"bad.go": src}); len(got) == 0 {
			t.Errorf("negative example %q is found", name)
		}
	}
}
