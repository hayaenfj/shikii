package notebook

import (
	"errors"
	"testing"
)

// Deleting an article keeps its supersession links: the article it replaced
// stays replaced. Neither is searched or delivered, both stay readable by id,
// and the knowledge comes back only as a new article.
func TestReplacedArticleStaysRetiredAfterItsReplacementIsDeleted(t *testing.T) {
	b := Book{Root: t.TempDir(), Scope: "/work/project"}
	d := draft(seed(t, b, 1), "Previous install steps")
	replaced, err := b.Ingest(d)
	if err != nil {
		t.Fatal(err)
	}
	d.Title, d.Replaces = "Current install steps", []string{replaced.Page.ID}
	replacement, err := b.Ingest(d)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.Manage(Change{ID: replacement.Page.ID, Revision: replacement.Page.Revision, Action: "delete"}); err != nil {
		t.Fatal(err)
	}
	searched, err := b.QueryPage("install steps", "")
	if err != nil || len(searched.Lessons) != 0 || searched.Search == nil || searched.Search.Total != 0 {
		t.Fatalf("a search returns the replaced or the deleted article: %+v %v", searched.Search, err)
	}
	delivered, snapshot, err := b.DeliverySnapshot(nil)
	if err != nil || len(delivered) != 0 {
		t.Fatalf("the replaced or the deleted article is delivered: %v %v", delivered, err)
	}
	states := map[string]string{}
	for _, entry := range snapshot.Entries {
		states[entry.ID] = entry.Retrieval
	}
	if states[replaced.Page.ID] != "superseded" || states[replacement.Page.ID] != "deleted" {
		t.Fatalf("delivery states: %v", states)
	}
	for _, id := range []string{replaced.Page.ID, replacement.Page.ID} {
		if v, err := b.Query("lesson:" + id); err != nil || len(v.Lessons) != 1 {
			t.Errorf("article %s is no longer readable by its id: %v", id, err)
		}
	}
	d.Replaces = nil
	for _, title := range []string{"Previous install steps", "Current install steps"} {
		d.Title = title
		if _, err := b.Ingest(d); !errors.Is(err, ErrArticleConflict) {
			t.Errorf("saving under the title %q again: %v, want a conflict", title, err)
		}
	}
	d.Title = "Install steps, restored"
	restored, err := b.Ingest(d)
	if err != nil {
		t.Fatalf("the knowledge cannot be saved again as a new article: %v", err)
	}
	live, err := b.Live()
	if err != nil || len(live) != 1 || live[0].ID != restored.Page.ID {
		t.Fatalf("only the new article is live: %v %v", live, err)
	}
	t.Logf("replaced %s: %s, deleted replacement %s: %s, new article %s: live", replaced.Page.ID, states[replaced.Page.ID], replacement.Page.ID, states[replacement.Page.ID], restored.Page.ID)
}
