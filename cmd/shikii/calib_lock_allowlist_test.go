package main

import (
	"fmt"
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
	for fn := range calibLockAllowed {
		if !seen[fn] {
			t.Errorf("calibLockAllowed lists %s, which no longer takes the lock (keep the list exact): found %v", fn, got)
		}
	}
	// negative example: a function outside the list that takes the lock is found
	bad := "package main\nfunc (s *server) handleNew() { s.calibMu.Lock(); defer s.calibMu.Unlock() }\n"
	if holders := calibLockHolders(t, fset, "bad.go", bad); len(holders) != 1 || calibLockAllowed[holders[0]] {
		t.Fatalf("the check finds a lock taken outside the list: %v", holders)
	}
}

// callGraph maps each function or method name in the package to the names it calls (s.x(...) and x(...)), and returns the routes registered
// with mux.HandleFunc: path -> handler name, and whether it goes through longWork.
func callGraph(t *testing.T, srcs map[string]string) (map[string][]string, map[string]string, map[string]bool, []string) {
	t.Helper()
	fset := token.NewFileSet()
	calls := map[string][]string{}
	routes, wrapped := map[string]string{}, map[string]bool{}
	var unreadable []string // registrations this check cannot follow: they fail the test instead of passing silently
	name := func(e ast.Expr) string {
		switch f := e.(type) {
		case *ast.SelectorExpr:
			return f.Sel.Name
		case *ast.Ident:
			return f.Name
		}
		return ""
	}
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
				c, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				if n := name(c.Fun); n != "" {
					calls[fn.Name.Name] = append(calls[fn.Name.Name], n)
				}
				sel, isMethod := c.Fun.(*ast.SelectorExpr)
				if !isMethod || (sel.Sel.Name != "HandleFunc" && sel.Sel.Name != "Handle") {
					return true
				}
				if id, ok := sel.X.(*ast.Ident); !ok || id.Name != "mux" {
					return true
				}
				lit, ok := c.Args[0].(*ast.BasicLit)
				if sel.Sel.Name == "Handle" || len(c.Args) != 2 || !ok {
					unreadable = append(unreadable, fmt.Sprintf("%s in %s", sel.Sel.Name, fn.Name.Name))
					return true
				}
				path := strings.Trim(lit.Value, "\"")
				h := c.Args[1]
				if w, ok := h.(*ast.CallExpr); ok && name(w.Fun) == "longWork" && len(w.Args) == 1 {
					wrapped[path], h = true, w.Args[0]
				}
				if m, ok := h.(*ast.SelectorExpr); !ok || !strings.HasPrefix(m.Sel.Name, "handle") {
					unreadable = append(unreadable, path+": the handler is not s.handleX or longWork(s.handleX)")
				}
				if _, dup := routes[path]; dup {
					unreadable = append(unreadable, path+": registered twice")
				}
				routes[path] = name(h)
				return true
			})
		}
	}
	return calls, routes, wrapped, unreadable
}

// reachesLock reports whether fn takes the calibration lock directly or through the functions it calls.
func reachesLock(fn string, calls map[string][]string, seen map[string]bool) bool {
	if calibLockAllowed[fn] {
		return true
	}
	if seen[fn] {
		return false
	}
	seen[fn] = true
	for _, c := range calls[fn] {
		if reachesLock(c, calls, seen) {
			return true
		}
	}
	return false
}

// 0.9.6 recheck, low 1: the allowlist above only sees functions that take the lock themselves. Every registered route whose handler reaches
// the lock through any call is registered through longWork; a route that reaches it without longWork fails (negative example below).
func TestEveryRouteThatReachesTheLockIsLongWork(t *testing.T) {
	files, _ := filepath.Glob("*.go")
	srcs := map[string]string{}
	for _, file := range files {
		if !strings.HasSuffix(file, "_test.go") {
			b, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			srcs[file] = string(b)
		}
	}
	calls, routes, wrapped, unreadable := callGraph(t, srcs)
	for _, u := range unreadable {
		t.Errorf("a route registration this check cannot follow (register it as mux.HandleFunc(\"/path\", s.handleX) or longWork(s.handleX)): %s", u)
	}
	if len(routes) < 10 {
		t.Fatalf("precondition: the routes are found (%d)", len(routes))
	}
	for path, h := range routes {
		if reachesLock(h, calls, map[string]bool{}) && !wrapped[path] {
			t.Errorf("%s (%s) reaches the calibration lock but is not registered through longWork", path, h)
		}
	}
	bad := map[string]string{"bad.go": "package main\nfunc (s *server) handleBad(w, r any) { s.maybeCalibrate(\"\", \"\", \"\") }\nfunc reg(mux *x) { mux.HandleFunc(\"/v1/bad\", s.handleBad) }\n"}
	calls, routes, wrapped, _ = callGraph(t, bad)
	if !reachesLock(routes["/v1/bad"], calls, map[string]bool{}) || wrapped["/v1/bad"] {
		t.Fatalf("negative example: a route that reaches the lock without longWork is found: %v %v", routes, wrapped)
	}
	odd := map[string]string{"odd.go": "package main\nfunc reg(mux *x, p string) { mux.HandleFunc(p, s.handleA); mux.Handle(\"/v1/b\", h); mux.HandleFunc(\"/v1/c\", s.auth(s.handleC)) }\n"}
	if _, _, _, u := callGraph(t, odd); len(u) != 3 {
		t.Fatalf("negative example: a variable path, mux.Handle and another wrapper are each reported: %v", u)
	}
}
