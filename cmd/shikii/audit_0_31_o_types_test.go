package main

import (
	"go/ast"
	"go/parser"
	"go/types"
	"testing"
)

// Audit O of 0.31.0: what the struct WorkSource (the fix of N-U5) keeps by the types.

// The kinds were constants of a uint8 before the fix, and now they are exported variables of the struct: a conversion from a number
// no longer compiles, yet any package can now rebind a kind (notebook.WorkResult() = notebook.WorkTrialPlan()), which the constants did
// not allow. After such a line every result is saved under the title of a trial plan, a record the evidence may cite. The typed
// allowlist of cmd/shikii (TestOnlyWorkArchiveBuildsASourceShikiiWrites) does not look at it either.
func TestAuditOAKindCanBeRebound(t *testing.T) {
	src := "package main\n\nimport \"github.com/hayaenfj/shikii/internal/notebook\"\n\nfunc init() { notebook.WorkResult = notebook.WorkTrialPlan }\n"
	f, err := parser.ParseFile(typedFset, "rebind.go", src, 0)
	if err != nil {
		t.Fatal(err)
	}
	typedMu.Lock()
	_, err = (&types.Config{Importer: typedImporter}).Check("main", typedFset, []*ast.File{f}, nil)
	typedMu.Unlock()
	t.Logf("type-check of %q: %v", "notebook.WorkResult = notebook.WorkTrialPlan", err)
	if err == nil {
		t.Errorf("INVARIANT 10 (type side): a kind of the table can be rebound outside the notebook (the constants before the fix could not)")
	}
	// the kinds are functions: the rebinding does not compile, so no reader of the code needs to look for it
}
