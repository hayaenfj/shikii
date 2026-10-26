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

// 0.9.7: replies in flight are kept alive while a calibration holds or waits for the lock (keepRepliesAliveDuringCalibration), whatever path
// reached it. That rests on one rule kept here: only lockCalibration touches calibMu, so the count of active calibrations is always kept.

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
