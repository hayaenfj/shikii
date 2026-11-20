package notebook

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/store"
)

// The readings that several words share (WordsFoundAlone, OpenMisses) fail where a wiki query of one of the words fails. Here the
// history of what was delivered is a folder, so a wiki query cannot say which sources wait for a review: a reading that went on and
// named words would answer for a wiki it could not read.
func TestTheSharedReadingsFailWhereAWikiQueryOfTheWordsFails(t *testing.T) {
	b := missFixture(t)
	if _, err := b.recordMissAt("saffron lanterns", "", "", emptyView(), time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	// the control: with a history that can be read, both readings answer
	words, err := b.WordsFoundAlone("quartz zzabsent", "", emptyView())
	if err != nil || len(words) != 1 || words[0] != "quartz" {
		t.Fatalf("the control, the word that finds the article alone: %q %v", words, err)
	}
	open, err := b.OpenMisses(time.Time{}, 8, "")
	if err != nil || len(open) != 1 {
		t.Fatalf("the control, the one open miss: %v %v", open, err)
	}

	history := filepath.Join(b.Root, "records", "feedback", store.StoreName(b.Root, b.Scope)+".jsonl")
	if err := os.MkdirAll(history, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Query("quartz"); err == nil {
		t.Fatal("the fixture: a wiki query fails when the history of what was delivered is a folder")
	}
	if words, err := b.WordsFoundAlone("quartz zzabsent", "", emptyView()); err == nil {
		t.Fatalf("the words found alone fail as the wiki query of each does, got %q", words)
	}
	if open, err := b.OpenMisses(time.Time{}, 8, ""); err == nil {
		t.Fatalf("the open misses fail as the wiki query of each does, got %v", open)
	}
}
