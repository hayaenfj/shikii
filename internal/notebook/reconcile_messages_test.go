package notebook

import (
	"strings"
	"testing"
)

// A reconciliation is a patch written against an earlier revision, so its refusals say what a patch's refusals say (patch_messages_test.go):
// why the edit cannot be read, and how many times its before is in the base revision. They said only "invalid reconciliation edit 1" and
// "must match exactly once in its base field".
func TestReconcileRefusalsSayTheReasonAndTheCount(t *testing.T) {
	b := Book{Root: t.TempDir(), Scope: "/work/project"}
	s := roundTripSource(t, b, 1)
	body := "alpha beta. gamma beta.\n"
	saved, err := b.Ingest(Draft{Title: "Reconciled article", Lesson: "A lesson.", AppliesTo: "here", Body: &body, Kind: "rule", Scene: "when here", Sources: []string{s}})
	if err != nil {
		t.Fatal(err)
	}
	p := saved.Page
	for _, c := range []struct {
		name string
		edit TextEdit
		want []string
	}{
		{"no field", TextEdit{Before: "alpha", After: "ALPHA"}, append([]string{"reconciliation edit 1", "no field"}, patchFieldNames...)},
		{"a field that is not an article field", TextEdit{Field: "text", Before: "alpha", After: "ALPHA"}, []string{"reconciliation edit 1", `"text"`, "body"}},
		{"a before that is nowhere", TextEdit{Field: "body", Before: "delta", After: "DELTA"}, []string{"reconciliation edit 1", "0 times", "body"}},
		{"a before that is there twice", TextEdit{Field: "body", Before: "beta", After: "BETA"}, []string{"reconciliation edit 1", "2 times", "body"}},
		{"an after that is its before", TextEdit{Field: "body", Before: "alpha", After: "alpha"}, []string{"a reconciliation edit must change its text"}},
		{"a before and an after that are both empty", TextEdit{Field: "body"}, []string{"a reconciliation edit must change its text"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			_, err := b.Reconcile(Patch{ID: p.ID, Revision: p.Revision, Sources: []string{s}, ChangeReason: "a checked edit", Edits: []TextEdit{c.edit}})
			if err == nil {
				t.Fatalf("the edit %v was accepted", c.edit)
			}
			for _, w := range c.want {
				if !strings.Contains(err.Error(), w) {
					t.Errorf("the refusal lacks %q: %s", w, err)
				}
			}
			for _, w := range []string{"invalid reconciliation edit", "exactly once in its base field"} {
				if strings.Contains(err.Error(), w) {
					t.Errorf("the refusal still says only %q: %s", w, err)
				}
			}
		})
	}
	// negative: an edit that is right is ready, so the refusals are about the edits
	got, err := b.Reconcile(Patch{ID: p.ID, Revision: p.Revision, Sources: []string{s}, ChangeReason: "a checked edit", Edits: []TextEdit{{Field: "body", Before: "alpha", After: "ALPHA"}}})
	if err != nil || got.Status != "ready" {
		t.Fatalf("the edit that is right is not ready: %v %q %q", err, got.Status, got.Reason)
	}
}
