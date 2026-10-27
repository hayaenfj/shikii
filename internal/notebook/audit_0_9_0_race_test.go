package notebook

import (
	"testing"

	"github.com/hayaenfj/shikii/internal/store"
)

// When another agent edits a lesson while classify runs, classify must not overwrite it with the old lesson it read (the edit would silently
// revert). Checked in one run, without waiting on a race: the edit is saved right after classify has read the page and before it saves.
func TestAudit090ClassifyDoesNotRevertConcurrentEdit(t *testing.T) {
	root := t.TempDir()
	scope := "/audit/race"
	if err := (store.Store{Dir: root}).AppendFeedback(scope, store.Feedback{ID: "00000000000003e7", Request: "r", Summary: "s", Reply: "x"}); err != nil {
		t.Fatal(err)
	}
	b := Book{Root: root, Scope: scope}
	draft := Draft{Sources: []string{"00000000000003e7"}, Title: "contested page", Lesson: "Old lesson.", AppliesTo: "a", Kind: "rule", Scene: "when working"}
	res, err := b.Ingest(draft)
	if err != nil {
		t.Fatal(err)
	}
	id := res.Page.ID
	if err := b.RewriteWithoutKindScene(id); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { classifyRead = func() {} })
	var editErr error
	met := 0
	classifyRead = func() {
		classifyRead = func() {}
		met++
		edit := draft
		edit.ID, edit.Lesson = id, "Edited lesson."
		_, editErr = b.Ingest(edit)
	}
	_, classifyErr := b.Classify(id, "fact", "when working")
	if met != 1 || editErr != nil {
		t.Fatalf("the edit is saved between classify's read and its save: met %d, %v", met, editErr)
	}
	live, err := b.Live()
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range live {
		if p.ID == id && p.Lesson != "Edited lesson." {
			t.Fatalf("a successfully saved lesson edit was reverted by classify to %q (classify gave %v)", p.Lesson, classifyErr)
		}
	}
	if classifyErr == nil {
		t.Fatal("classify refuses to save over a page edited after it read it")
	}
}
