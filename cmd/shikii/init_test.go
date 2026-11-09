package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hayaenfj/shikii/internal/notebook"
)

func readJSON(t *testing.T, path string) map[string]any {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("%s: %v\n%s", path, err, b)
	}
	return m
}

func probeBlock(cmd string) string {
	return "[mcp_servers.shikii]\ncommand = " + quotedList([]string{cmd}) + "\nargs = [" + quotedList(codexServerArgs) + "]\n"
}

func finalBC1(t *testing.T, query bool) {
	common, old, moved := movedProject(t)
	asIf096(t, common, old, moved)
	t.Chdir(moved)
	if query {
		capture(t, func() error { return cmdNotebook([]string{"query"}) }) // e.g. the skill's "query the wiki first", before --init
	}
	capture(t, func() error { return cmdInit(nil) }) // the person's --init, as the notice asks
	sum := sha256.Sum256([]byte(old))
	recorded, _ := os.ReadFile(filepath.Join(moved, ".shikii", "records", "store-name"))
	live, err := (notebook.Book{Root: filepath.Join(moved, ".shikii"), Scope: moved}).Live()
	if strings.TrimSpace(string(recorded)) != hex.EncodeToString(sum[:]) || err != nil || len(live) != 1 {
		t.Fatalf("B2: a store 0.9.6 wrote is read as it is after a move: store-name=%q (want sha256(old)=%s), %d pages found %v",
			strings.TrimSpace(string(recorded)), hex.EncodeToString(sum[:]), len(live), err)
	}
}

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

// Codex audit 0.9.2 (.shikii/handoff/codex-audit-0.9.2/report.md).

var builtInSpec = []byte("name: needs-plan\nquestion: Should the plan be agreed first?\n")

// The audit of the 0.9.9 changes (v0.9.8..34451f3, .shikii/handoff/plan-0.9.9/audit.md). Each test failed on 34451f3.

// 0.9.9: the small items the 0.9.7 audits left (.shikii/handoff/plan-0.9.9/triage.md, C). Each test is checked to fail before its fix.

func relinkInit(t *testing.T, f string) string {
	t.Helper()
	t.Chdir(f)
	return capture(t, func() error { return cmdInit(nil) })
}
