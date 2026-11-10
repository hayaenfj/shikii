package notebook

import (
	"os"
	"path/filepath"
	"testing"
)

// Reconcile reads the current article with an exact query. That query also
// returns the source problems of the articles it scanned before reaching it, in
// id order. A problem in another article must not make this one "unavailable",
// and the answer must not depend on which of the two ids sorts first.
func TestReconcileIgnoresSourceProblemsOfOtherArticles(t *testing.T) {
	b := Book{Root: t.TempDir(), Scope: "/work/project"}
	s1, s2 := roundTripSource(t, b, 1), roundTripSource(t, b, 2)
	body := "first line\nsecond line\n"
	one, err := b.Ingest(Draft{Title: "Article one", Lesson: "Lesson one.", AppliesTo: "one", Body: &body, Kind: "rule", Scene: "when one", Sources: []string{s1}})
	if err != nil {
		t.Fatal(err)
	}
	two, err := b.Ingest(Draft{Title: "Article two", Lesson: "Lesson two.", AppliesTo: "two", Body: &body, Kind: "rule", Scene: "when two", Sources: []string{s2}})
	if err != nil {
		t.Fatal(err)
	}
	// The article with the smaller id loses its source record; the other stays whole.
	damaged, healthy, lost := one.Page, two.Page, s1
	if damaged.ID > healthy.ID {
		damaged, healthy, lost = two.Page, one.Page, s2
	}
	sources, err := b.directory(false, "sources")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(sources, lost+".json")); err != nil {
		t.Fatal(err)
	}
	healthySource := s1
	if lost == s1 {
		healthySource = s2
	}
	intent := Patch{ID: healthy.ID, Revision: healthy.Revision, Sources: []string{healthySource}, ChangeReason: "clarify the first line", Edits: []TextEdit{{Field: "body", Before: "first line", After: "first line, clarified"}}}
	// The ordinary patch preview accepts the healthy article.
	if _, err := b.PreviewPatch(intent); err != nil {
		t.Fatalf("the healthy article cannot be patched: %v", err)
	}
	got, err := b.Reconcile(intent)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "ready" {
		t.Fatalf("reconcile of healthy article %s (damaged other article %s sorts first): status=%q reason=%q", healthy.ID, damaged.ID, got.Status, got.Reason)
	}
}
