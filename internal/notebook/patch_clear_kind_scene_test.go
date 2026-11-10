package notebook

import (
	"testing"
)

// A patch edit that empties kind or scene must not be reported as saved while
// the old value silently stays.
func TestPatchCannotSilentlyKeepAClearedKindOrScene(t *testing.T) {
	for field, before := range map[string]string{"scene": "when committing", "kind": "rule"} {
		t.Run(field, func(t *testing.T) {
			b := Book{Root: t.TempDir(), Scope: "/work/project"}
			source := roundTripSource(t, b, 1)
			first, err := b.Ingest(Draft{Title: "Commit rules", Lesson: "Commit per feature.", AppliesTo: "commits", Kind: "rule", Scene: "when committing", Sources: []string{source}})
			if err != nil {
				t.Fatal(err)
			}
			saved, err := b.Patch(Patch{ID: first.Page.ID, Revision: first.Page.Revision, Sources: []string{source}, ChangeReason: "remove the " + field, Edits: []TextEdit{{Field: field, Before: before, After: ""}}})
			if err != nil {
				return // refused: the caller knows the edit was not applied
			}
			value := saved.Page.Scene
			if field == "kind" {
				value = saved.Page.Kind
			}
			t.Fatalf("the patch reported revision %s as saved, but %s is still %q (the edit asked for an empty value)", saved.Page.Revision, field, value)
		})
	}
}
