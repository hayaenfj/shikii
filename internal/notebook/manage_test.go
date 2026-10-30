package notebook

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestManageArticlePreservesEvidenceAndRejectsStaleEdits(t *testing.T) {
	b := Book{Root: t.TempDir(), Scope: "/work/project"}
	d := draft(seed(t, b, 1), "Original article")
	r, err := b.Ingest(d)
	if err != nil {
		t.Fatal(err)
	}
	old := *r.Page
	c := Change{ID: old.ID, Revision: old.Revision, Action: "edit", Title: "Updated article", Lesson: "Updated summary", AppliesTo: "Updating docs", Body: "More detail", Kind: "howto", Scene: "Documentation"}
	got, err := b.Manage(c)
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != c.Title || got.Body != c.Body || got.Previous != old.Revision || got.Revision == old.Revision || len(got.Sources) != len(old.Sources)+1 {
		t.Fatalf("edit keeps evidence and makes a revision: %+v", got)
	}
	history, err := b.Query("lesson:" + old.ID + "@" + old.Revision)
	if err != nil || (len(history.Lessons) != 1 || history.Lessons[0].Revision != old.Revision || history.Lessons[0].Title != old.Title || !reflect.DeepEqual(history.Lessons[0].Sources, old.Sources)) {
		t.Fatalf("history changed: %+v %v", history, err)
	}
	if _, err := b.Manage(c); !errors.Is(err, ErrArticleConflict) {
		t.Fatalf("stale edit: %v", err)
	}
	c.Action = "delete"
	if _, err := b.Manage(c); !errors.Is(err, ErrArticleConflict) {
		t.Fatalf("stale delete: %v", err)
	}
	c.Action, c.Revision, c.Title = "edit", got.Revision, strings.Repeat("x", 201)
	if _, err := b.Manage(c); err == nil {
		t.Fatal("oversized title accepted")
	}
	current, _ := b.Query("lesson:" + old.ID)
	if current.Lessons[0].Revision != got.Revision {
		t.Fatal("rejected change modified article")
	}
	if pending, err := b.PendingObservations(); err != nil || len(pending) != 0 {
		t.Fatalf("successful changes should not need ingestion again: %v %v", pending, err)
	}
}

func TestDeleteArticleStopsDeliveryWithoutRestoringReplacedArticle(t *testing.T) {
	b := Book{Root: t.TempDir(), Scope: "/work/project"}
	d := draft(seed(t, b, 1), "Previous instructions")
	first, err := b.Ingest(d)
	if err != nil {
		t.Fatal(err)
	}
	d.Title, d.Scene, d.Replaces = "Current instructions", "always", []string{first.Page.ID}
	second, err := b.Ingest(d)
	if err != nil {
		t.Fatal(err)
	}
	p := *second.Page
	deleted, err := b.Manage(Change{ID: p.ID, Revision: p.Revision, Action: "delete"})
	if err != nil || deleted.Status != "deleted" || deleted.Previous != p.Revision {
		t.Fatalf("delete: %+v %v", deleted, err)
	}
	for name, read := range map[string]func() ([]Page, error){"live": b.Live, "always": b.AlwaysRules, "recall": func() ([]Page, error) { return b.Recall("README instructions") }} {
		pages, err := read()
		if err != nil || len(pages) != 0 {
			t.Fatalf("%s returns deleted or replaced article: %v %v", name, pages, err)
		}
	}
	v, err := b.Query("")
	if err != nil || len(v.Lessons) != 0 {
		t.Fatalf("query: %+v %v", v, err)
	}
	tasks, err := b.Maintenance()
	if err != nil {
		t.Fatal(err)
	}
	for _, task := range tasks {
		if task.PageID == p.ID {
			t.Fatal("deleted article still needs maintenance")
		}
	}
	d.ID = p.ID
	if _, err := b.Ingest(d); !errors.Is(err, ErrArticleConflict) {
		t.Fatalf("agent restored a deleted article: %v", err)
	}
	d.ID = ""
	if _, err := b.Ingest(d); !errors.Is(err, ErrArticleConflict) {
		t.Fatalf("same title restored a deleted article: %v", err)
	}
	if _, err := b.Manage(Change{ID: p.ID, Revision: deleted.Revision, Action: "delete"}); !errors.Is(err, ErrArticleConflict) {
		t.Fatalf("second deletion: %v", err)
	}
	if _, err := b.Manage(Change{ID: first.Page.ID, Revision: first.Page.Revision, Action: "delete"}); !errors.Is(err, ErrArticleConflict) {
		t.Fatalf("replaced article is not editable: %v", err)
	}
	for _, ref := range []string{"lesson:" + p.ID, "lesson:" + p.ID + "@" + p.Revision} {
		v, err := b.Query(ref)
		if err != nil || len(v.Lessons) != 1 {
			t.Fatalf("explicit history unavailable: %s %v", ref, err)
		}
	}
	if report, err := b.Lint(); err != nil || len(report.Issues) != 0 {
		t.Fatalf("delete breaks references: %+v %v", report, err)
	}
}

func TestDeletedReplacementSurvivesSourceFailure(t *testing.T) {
	b := Book{Root: t.TempDir(), Scope: "/work/project"}
	d := draft(seed(t, b, 1), "Previous instructions")
	d.Scene = "always"
	old, err := b.Ingest(d)
	if err != nil {
		t.Fatal(err)
	}
	d.Title, d.Scene, d.Replaces = "Current instructions", "always", []string{old.Page.ID}
	current, err := b.Ingest(d)
	if err != nil {
		t.Fatal(err)
	}
	deleted, err := b.Manage(Change{ID: current.Page.ID, Revision: current.Page.Revision, Action: "delete"})
	if err != nil {
		t.Fatal(err)
	}
	var source string
	for _, s := range deleted.Sources {
		found := false
		for _, old := range current.Page.Sources {
			if s.ID == old.ID {
				found = true
			}
		}
		if !found {
			source = s.ID
		}
	}
	if source == "" {
		t.Fatal("missing deletion source")
	}
	dir, err := b.directory(false, "sources")
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, source+".json")
	saved, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(p); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.WriteFile(p, saved, 0o600) })
	for name, read := range map[string]func() ([]Page, error){"query": func() ([]Page, error) { v, e := b.Query(""); return v.Lessons, e }, "recall": func() ([]Page, error) { return b.Recall("README installation instructions") }, "always": b.AlwaysRules, "live": b.Live} {
		got, err := read()
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range got {
			if p.ID == old.Page.ID {
				t.Errorf("%s restored the replaced article after deletion", name)
			}
		}
	}
}

func TestDeletedRenamedArticleCannotBeRecreated(t *testing.T) {
	b := Book{Root: t.TempDir(), Scope: "/work/project"}
	d := draft(seed(t, b, 1), "Original title")
	first, err := b.Ingest(d)
	if err != nil {
		t.Fatal(err)
	}
	edited, err := b.Manage(Change{ID: first.Page.ID, Revision: first.Page.Revision, Action: "edit", Title: "Renamed title", Lesson: d.Lesson, AppliesTo: d.AppliesTo, Kind: d.Kind, Scene: d.Scene})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.Manage(Change{ID: edited.ID, Revision: edited.Revision, Action: "delete"}); err != nil {
		t.Fatal(err)
	}
	for _, title := range []string{"Original title", "Renamed title"} {
		d.Title = title
		if _, err := b.Ingest(d); !errors.Is(err, ErrArticleConflict) {
			t.Errorf("recreated deleted title %q: %v", title, err)
		}
	}
}

func TestManageRechecksRevisionAfterLock(t *testing.T) {
	b := Book{Root: t.TempDir(), Scope: "/work/project"}
	d := draft(seed(t, b, 1), "Concurrent article")
	r, err := b.Ingest(d)
	if err != nil {
		t.Fatal(err)
	}
	c := Change{ID: r.Page.ID, Revision: r.Page.Revision, Action: "delete"}
	dir, err := b.directory(false, "pages")
	if err != nil {
		t.Fatal(err)
	}
	unlock, err := lock(dir)
	if err != nil {
		t.Fatal(err)
	}
	busy := make(chan struct{}, 1)
	lockBusy = func() {
		select {
		case busy <- struct{}{}:
		default:
		}
	}
	defer func() { lockBusy = nil }()
	result := make(chan error, 1)
	go func() { _, err := b.Manage(c); result <- err }()
	<-busy
	d.ID = r.Page.ID
	d.Lesson = "New summary while deletion waits"
	newer, err := b.ingest(d, true)
	unlock()
	if err != nil {
		t.Fatal(err)
	}
	if err := <-result; !errors.Is(err, ErrArticleConflict) {
		t.Fatalf("stale deletion accepted after lock: %v", err)
	}
	v, err := b.Query("lesson:" + r.Page.ID)
	if err != nil || len(v.Lessons) != 1 || v.Lessons[0].Revision != newer.Page.Revision {
		t.Fatalf("newer edit lost: %v", err)
	}
}
