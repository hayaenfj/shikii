package notebook

import (
	"fmt"
	"sync"
	"testing"

	"github.com/hayaenfj/shikii/internal/store"
)

// When another agent edits a lesson while classify runs, classify overwrites it with the old lesson it read, and the edit silently reverts.
func TestAudit090ClassifyDoesNotRevertConcurrentEdit(t *testing.T) {
	root := t.TempDir()
	scope := "/audit/race"
	if err := (store.Store{Dir: root}).AppendFeedback(scope, store.Feedback{ID: "00000000000003e7", Request: "r", Summary: "s", Reply: "x"}); err != nil {
		t.Fatal(err)
	}
	b := Book{Root: root, Scope: scope}
	// Put many pages in place, closer to a real wiki where reading Live takes time
	for i := 0; i < 40; i++ {
		if _, err := b.Ingest(Draft{Sources: []string{"00000000000003e7"}, Title: fmt.Sprintf("other page %d", i), Lesson: "A lesson.", AppliesTo: "a", Kind: "rule", Scene: "when working"}); err != nil {
			t.Fatal(err)
		}
	}
	lost := 0
	for i := 0; i < 60; i++ {
		res, err := b.Ingest(Draft{Sources: []string{"00000000000003e7"}, Title: fmt.Sprintf("contested page %d", i), Lesson: "Old lesson.", AppliesTo: "a", Kind: "rule", Scene: "when working"})
		if err != nil {
			t.Fatal(err)
		}
		id := res.Page.ID
		if err := b.RewriteWithoutKindScene(id); err != nil {
			t.Fatal(err)
		}
		var wg sync.WaitGroup
		var editErr error
		wg.Add(2)
		go func() { defer wg.Done(); _, _ = b.Classify(id, "fact", "when working") }()
		go func() {
			defer wg.Done()
			_, editErr = b.Ingest(Draft{ID: id, Sources: []string{"00000000000003e7"}, Title: fmt.Sprintf("contested page %d", i), Lesson: "Edited lesson.", AppliesTo: "a", Kind: "rule", Scene: "when working"})
		}()
		wg.Wait()
		live, _ := b.Live()
		for _, p := range live {
			if p.ID == id && editErr == nil && p.Lesson == "Old lesson." {
				lost++
			}
		}
	}
	if lost > 0 {
		t.Fatalf("in %d of 60 runs, a successfully saved lesson edit was reverted to the old lesson by classify", lost)
	}
}
