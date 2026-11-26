package main

import (
	"go/ast"
	"strings"
	"testing"
)

// typedSources keeps what it made of a set of sources, so the many tests that read the same package check it once. What it keeps is
// told by the sources and by whether the read is strict, nothing else: the same sources give the same checked files, and a changed
// text, another set of files or the other kind of read gives a new check.
func TestTypedSourcesIsSharedOnlyForTheSameSourcesAndKindOfRead(t *testing.T) {
	one := map[string]string{"a.go": "package main\n\nvar x = 1\n"}
	first := typedSources(t, one, true)
	if len(first) != 1 || first[0].name != "a.go" || first[0].info == nil {
		t.Fatalf("the check of a one-file package: %+v", first)
	}
	again := typedSources(t, map[string]string{"a.go": "package main\n\nvar x = 1\n"}, true)
	if len(again) != 1 || again[0].file != first[0].file || again[0].info != first[0].info {
		t.Fatal("the same sources were checked again instead of being shared")
	}
	shared := again[0].file
	first[0], again[0] = typedFile{}, typedFile{} // what a caller does to the slice it got does not reach the next caller
	if third := typedSources(t, one, true); len(third) != 1 || third[0].file == nil || third[0].file != shared {
		t.Fatal("a caller changed the slice another caller gets")
	}
	literal := func(files []typedFile) string {
		var value string
		ast.Inspect(files[0].file, func(n ast.Node) bool {
			if lit, ok := n.(*ast.BasicLit); ok {
				value = lit.Value
			}
			return true
		})
		return value
	}
	if got := literal(typedSources(t, map[string]string{"a.go": "package main\n\nvar x = 2\n"}, true)); got != "2" {
		t.Fatalf("a changed text was served from the check of the old one: the literal is %q, want 2", got)
	}
	if got := typedSources(t, map[string]string{"a.go": "package main\n\nvar x = 1\n", "b.go": "package main\n\nvar y = x\n"}, true); len(got) != 2 {
		t.Fatalf("a package with one more file was served from the check of the smaller one: %d files", len(got))
	}
	loose := typedSources(t, one, false)
	if len(loose) != 2 || !loose[1].prelude && !loose[0].prelude {
		t.Fatalf("a snippet read (not strict) was served from the strict check of the same text: %d files, want the file and the prelude's", len(loose))
	}
}

// A strict read of code that does not type-check ends the test every time it is asked for: the failure is not kept as a check that went
// through.
func TestTypedSourcesDoesNotKeepACheckThatFailed(t *testing.T) {
	bad := map[string]string{"bad.go": "package main\n\nvar x int = \"text\"\n"}
	for range 2 {
		rec := &fatalRecorder{TB: t}
		done := make(chan struct{})
		go func() {
			defer close(done)
			typedSources(rec, bad, true)
		}()
		<-done
		if !strings.Contains(rec.fatal, "does not type-check") {
			t.Fatalf("code that does not type-check was read: %q", rec.fatal)
		}
	}
}
