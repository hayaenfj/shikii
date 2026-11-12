package main

import (
	"go/ast"
	"go/parser"
	"go/types"
	"testing"

	"github.com/hayaenfj/shikii/internal/notebook"
)

// Audit N of 0.31.0: what "kept by the types" covers.

// INVARIANT 10: a source shikii writes is made only from the kinds of notebook.WorkSource, and the types keep it: outside the notebook a
// WorkSource is one of its values or the zero value. Turning a number into one, or setting its field, does not compile (audit N of
// 0.31.0: WorkSource(5) compiled and saved a source of that kind). The check type-checks each spelling against the real notebook.
func TestAWorkSourceIsNoNumber(t *testing.T) {
	for name, body := range map[string]string{
		"a conversion":  "var _ = notebook.WorkSource(5)",
		"its field":     "var _ = notebook.WorkSource{k: 5}",
		"a positional":  "var _ = notebook.WorkSource{5}",
		"an arithmetic": "var _ = notebook.WorkResult() + 1",
	} {
		src := "package main\n\nimport \"github.com/hayaenfj/shikii/internal/notebook\"\n\n" + body + "\n"
		f, err := parser.ParseFile(typedFset, "kind.go", src, 0)
		if err != nil {
			t.Fatal(err)
		}
		typedMu.Lock()
		_, err = (&types.Config{Importer: typedImporter}).Check("main", typedFset, []*ast.File{f}, nil)
		typedMu.Unlock()
		if err == nil {
			t.Errorf("%s: %q type-checks outside the notebook", name, body)
		}
	}
	// control: a kind of the table and the zero value
	src := "package main\n\nimport \"github.com/hayaenfj/shikii/internal/notebook\"\n\nvar _, _ = notebook.WorkResult(), notebook.WorkSource{}\n"
	f, _ := parser.ParseFile(typedFset, "kind.go", src, 0)
	typedMu.Lock()
	_, err := (&types.Config{Importer: typedImporter}).Check("main", typedFset, []*ast.File{f}, nil)
	typedMu.Unlock()
	if err != nil {
		t.Errorf("control: a kind of the table does not type-check: %v", err)
	}
}

// A trial is read back only from a source shikii wrote as one (kind agent): an observation of another kind with the very title and
// content of a saved trial plan is refused (the kind is part of what a record is, not only the title and the content).
func TestATrialIsReadOnlyFromAWorkSource(t *testing.T) {
	_, _, b, p := reasoningFixture(t)
	saveReasoning(t, b, p["receipt"], reasoningInput("start", "current"), nil)
	plan := trialRecord(t, b, p["receipt"], map[string]any{"plan": trialPlan("small", "current")}, nil)
	id := ""
	for _, raw := range plan["saved"].([]any) {
		if item, ok := raw.(map[string]any); ok && item["kind"] == "trial" {
			id, _ = item["id"].(string)
		}
	}
	if id == "" {
		t.Fatalf("precondition: the trial plan is saved as a source: %.400s", auditJSON(plan))
	}
	if _, err := readWorkTrialSource(b, id, notebook.WorkTrialPlan(), "trial-plan/v1"); err != nil {
		t.Fatalf("control: the trial plan shikii wrote is read: %v", err)
	}
	material, err := b.ReadSources([]string{id})
	if err != nil {
		t.Fatal(err)
	}
	original := material["observations"].([]notebook.Observation)[0]
	forged, err := b.CaptureObserved(notebook.Observation{Kind: "test", Title: original.Title, Content: original.Content, Reference: original.Reference, Version: original.Version, ObservedAt: original.ObservedAt})
	if err != nil {
		t.Fatalf("precondition: an observation of kind test keeps the title: %v", err)
	}
	if _, err := readWorkTrialSource(b, forged.ID, notebook.WorkTrialPlan(), "trial-plan/v1"); err == nil {
		t.Errorf("an observation of kind test with the title and content of a trial plan is read as a trial plan")
	}
}
