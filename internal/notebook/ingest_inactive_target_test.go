package notebook

import (
	"errors"
	"testing"
)

func liveLesson(t *testing.T, b Book, id string) (Page, bool) {
	t.Helper()
	live, err := b.Live()
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range live {
		if p.ID == id {
			return p, true
		}
	}
	return Page{}, false
}

// Patch and Manage refuse an article another article replaced. Ingest must not
// report a saved lesson that no query or delivery can ever return.
func TestIngestIntoReplacedArticleDoesNotReportALostLesson(t *testing.T) {
	for _, byID := range []bool{false, true} {
		name := "by title"
		if byID {
			name = "by id"
		}
		t.Run(name, func(t *testing.T) {
			b := Book{Root: t.TempDir(), Scope: "/work/project"}
			s1, s2, s3 := roundTripSource(t, b, 1), roundTripSource(t, b, 2), roundTripSource(t, b, 3)
			old, err := b.Ingest(Draft{Title: "Release procedure", Lesson: "Tag after CI.", AppliesTo: "releases", Kind: "howto", Scene: "when releasing", Sources: []string{s1}})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := b.Ingest(Draft{Title: "Release procedure with audit", Lesson: "Tag after CI and the audit.", AppliesTo: "releases", Kind: "howto", Scene: "when releasing", Sources: []string{s2}, Replaces: []string{old.Page.ID}}); err != nil {
				t.Fatal(err)
			}
			d := Draft{Title: "Release procedure", Lesson: "Never tag on a Friday.", AppliesTo: "releases", Kind: "rule", Scene: "when releasing", Sources: []string{s3}}
			if byID {
				d.ID = old.Page.ID
			}
			saved, err := b.Ingest(d)
			if err != nil {
				if !errors.Is(err, ErrArticleConflict) {
					t.Fatalf("a refusal must be the article conflict, got: %v", err)
				}
				return
			}
			if _, ok := liveLesson(t, b, saved.Page.ID); !ok {
				v, _ := b.Query("Friday tag")
				t.Fatalf("ingest reported success for %s@%s, but the lesson is not live and a search for it returns %d lessons", saved.Page.ID, saved.Page.Revision, len(v.Lessons))
			}
		})
	}
}

// The id of an article comes from its first title. After a rename, a new lesson
// under the former title must not silently overwrite the renamed article.
func TestIngestByFormerTitleDoesNotOverwriteRenamedArticle(t *testing.T) {
	b := Book{Root: t.TempDir(), Scope: "/work/project"}
	s1, s2 := roundTripSource(t, b, 1), roundTripSource(t, b, 2)
	body := "Push once per milestone because CI is paid per run."
	first, err := b.Ingest(Draft{Title: "Commit rules", Lesson: "Commit per feature.", AppliesTo: "commits", Body: &body, Kind: "rule", Scene: "when committing", Sources: []string{s1}})
	if err != nil {
		t.Fatal(err)
	}
	renamed, err := b.Patch(Patch{ID: first.Page.ID, Revision: first.Page.Revision, Sources: []string{s1}, ChangeReason: "the article is about pushing", Edits: []TextEdit{{Field: "title", Before: "Commit rules", After: "Push rules"}, {Field: "lesson", Before: "Commit per feature.", After: "Push once per milestone."}}})
	if err != nil {
		t.Fatal(err)
	}
	saved, err := b.Ingest(Draft{Title: "Commit rules", Lesson: "Write detailed commit messages.", AppliesTo: "commit messages", Kind: "rule", Scene: "when committing", Sources: []string{s2}})
	if err != nil && !errors.Is(err, ErrArticleConflict) {
		t.Fatalf("a refusal must be the article conflict, got: %v", err)
	}
	now, ok := liveLesson(t, b, renamed.Page.ID)
	if !ok || now.Title != "Push rules" || now.Lesson != "Push once per milestone." {
		t.Fatalf("the renamed article was overwritten by a new lesson under its former title (ingest error=%v): live=%v title=%q lesson=%q body=%q", err, ok, now.Title, now.Lesson, now.Body)
	}
	if err == nil && saved.Page.ID == renamed.Page.ID {
		t.Fatalf("the new lesson was saved into the renamed article %s", renamed.Page.ID)
	}
	// Updating the renamed article by its id still works.
	if _, err := b.Ingest(Draft{ID: renamed.Page.ID, Title: "Push rules", Lesson: "Push once per milestone, after the audit.", AppliesTo: "commits", Kind: "rule", Scene: "when committing", Sources: []string{s2}}); err != nil {
		t.Fatalf("an update by id is refused: %v", err)
	}
}
