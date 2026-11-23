package notebook

import (
	"errors"
	"strings"
	"testing"
)

// A withdrawn article is gone like a deleted one in every place the wiki asks. Each test here holds a place where a check that only knew
// the deleted status passed all the other tests, because another check caught the withdrawn article first or nothing looked at it.

// withdrawnProcedure saves a howto with one reported failure and withdraws it. It returns the article as it was before and after.
func withdrawnProcedure(t *testing.T, f *outcomeFixture) (live, withdrawn Page) {
	t.Helper()
	live = f.page("howto", "Export procedure", "Check the export.", "exports", "Run the raw parser.")
	f.report(live, "Linux arm64", "Quoted input was cut.", "Use the quoted parser.", "Fixed")
	withdrawn, err := f.b.Withdraw(Withdrawal{ID: live.ID, Revision: live.Revision, Sources: []string{f.source}, Reason: "the request was deleted"})
	if err != nil {
		t.Fatal(err)
	}
	return live, withdrawn
}

// A withdrawn article is no review task: it is not handed to the agent as a page to maintain, and a submit does not compare its key.
func TestAWithdrawnArticleIsNoMaintenanceTask(t *testing.T) {
	f := newOutcomeFixture(t)
	live := f.page("howto", "Export procedure", "Check the export.", "exports", "Run the raw parser.")
	tasks, err := f.b.Maintenance()
	keys, keysErr := f.b.MaintenanceKeys()
	if err != nil || keysErr != nil || maintenanceTask(tasks, live.ID) == nil || keys[live.ID].PageID != live.ID {
		t.Fatalf("a live article is a review task and has a key: %+v %v %v %v", tasks, err, keys, keysErr)
	}
	if _, err := f.b.Withdraw(Withdrawal{ID: live.ID, Revision: live.Revision, Sources: []string{f.source}, Reason: "the request was deleted"}); err != nil {
		t.Fatal(err)
	}
	tasks, err = f.b.Maintenance()
	keys, keysErr = f.b.MaintenanceKeys()
	if err != nil || keysErr != nil || maintenanceTask(tasks, live.ID) != nil {
		t.Fatalf("a withdrawn article is handed out as a maintenance task: %+v %v", tasks, err)
	}
	if _, ok := keys[live.ID]; ok {
		t.Fatalf("a withdrawn article has a maintenance key: %+v", keys)
	}
}

// The reported failures of a procedure are attached to a review of the procedure, never to a task for a withdrawn one.
func TestAWithdrawnProcedureGetsNoOutcomeReview(t *testing.T) {
	f := newOutcomeFixture(t)
	live, withdrawn := withdrawnProcedure(t, f)
	for name, c := range map[string]struct {
		page Page
		want int
	}{"live": {live, 1}, "withdrawn": {withdrawn, 0}} {
		tasks := []MaintenanceTask{{PageID: c.page.ID, Revision: c.page.Revision, Key: "key"}}
		if err := f.b.addOutcomeReviews(tasks, []Page{c.page}); err != nil || tasks[0].OutcomeTotal != c.want || len(tasks[0].FeedbackKeys) != c.want {
			t.Fatalf("%s: the reported failure reaches a task only for a live procedure (want %d): %+v %v", name, c.want, tasks[0], err)
		}
	}
}

// Asking for the review of a withdrawn article is answered with nothing: its own sources and the pages around it need not be readable.
func TestAWithdrawnArticleSelectedForReviewNeedsNoReadableSources(t *testing.T) {
	f := newOutcomeFixture(t)
	live := f.page("howto", "Live procedure", "Keep the approval condition.", "Synthetic archive", "Owner approval is required.")
	_, withdrawn := withdrawnProcedure(t, f)
	f.lose(f.source)
	if tasks, err := f.b.MaintenanceForPages([]string{live.ID}); err == nil {
		t.Fatalf("a live article that lost its source is not a completed review: %+v", tasks)
	}
	if tasks, err := f.b.MaintenanceForPages([]string{withdrawn.ID}); err != nil || len(tasks) != 0 {
		t.Fatalf("a withdrawn article needs no review, so its lost source does not stop it: %+v %v", tasks, err)
	}
}

// A new article cannot rest its learning on a withdrawn article: the connection would cite what is no longer handed out.
func TestLearningDoesNotConnectToAWithdrawnArticle(t *testing.T) {
	b, source := growthIndependentFixture(t)
	one := growthIndependentPage(t, b, source, "Synthetic experience one", "One unique passage that stays.")
	two := growthIndependentPage(t, b, source, "Synthetic experience two", "Another unique passage that stays.")
	goes := growthIndependentPage(t, b, source, "Synthetic experience that goes", "One unique passage that goes.")
	goes, err := b.Withdraw(Withdrawal{ID: goes.ID, Revision: goes.Revision, Sources: []string{source}, Reason: "the request was deleted"})
	if err != nil {
		t.Fatal(err)
	}
	connect := func(title string, cited ...Page) error {
		var connections []ExperienceConnection
		for _, p := range cited {
			connections = append(connections, ExperienceConnection{ID: p.ID, Revision: p.Revision, Quote: p.Body, Conditions: "Synthetic condition", Reason: "Synthetic reason", Relation: "supports"})
		}
		_, err := b.Ingest(Draft{Title: title, Lesson: "Synthetic synthesis", AppliesTo: "Synthetic workflow", Kind: "fact", Scene: "Synthetic work", Sources: []string{source}, Learning: &Learning{Connections: connections}})
		return err
	}
	if err := connect("Synthetic synthesis of live articles", one, two); err != nil {
		t.Fatalf("live articles are connected: %v", err)
	}
	if err := connect("Synthetic synthesis with a withdrawn article", one, goes); !errors.Is(err, ErrArticleConflict) {
		t.Fatalf("a withdrawn article is not connected: %v", err)
	}
}

// Patch requires the revisions of the articles it was written against, and a withdrawn article is not one that can be required.
func TestPatchDoesNotRequireAWithdrawnArticle(t *testing.T) {
	b := Book{Root: t.TempDir(), Scope: "/work/project"}
	s := seed(t, b, 1)
	target, err := b.Ingest(draft(s, "The article the patch edits"))
	if err != nil {
		t.Fatal(err)
	}
	_, gone := withdrawnArticle(t, b, "The article the patch relied on", []string{workSource(t, b, "agent", 2)})
	edit := Patch{ID: target.Page.ID, Revision: target.Page.Revision, Sources: []string{s}, ChangeReason: "fix", Edits: []TextEdit{{Field: "lesson", Before: target.Page.Lesson, After: "Changed lesson."}}}
	edit.Requires = []ArticleReference{{ID: gone.ID, Revision: gone.Revision}}
	if _, err := b.Patch(edit); !errors.Is(err, ErrArticleConflict) {
		t.Fatalf("a patch that requires a withdrawn article is refused: %v", err)
	}
	edit.Requires = []ArticleReference{{ID: target.Page.ID, Revision: target.Page.Revision}}
	if _, err := b.Patch(edit); err != nil {
		t.Fatalf("a patch that requires a live article is saved: %v", err)
	}
}

// A patch of a withdrawn article is refused as a conflict before its edits are looked at: the same answer whatever it says.
func TestPatchOfAWithdrawnArticleIsRefusedBeforeItsEdits(t *testing.T) {
	b := Book{Root: t.TempDir(), Scope: "/work/project"}
	src := workSource(t, b, "agent", 1)
	_, gone := withdrawnArticle(t, b, "Waiting for a person", []string{src})
	for name, edits := range map[string][]TextEdit{
		"edit that fits":      {{Field: "lesson", Before: gone.Lesson, After: "Changed lesson."}},
		"edit that is absent": {{Field: "lesson", Before: "text that is not in the lesson", After: "Changed lesson."}},
	} {
		if _, err := b.Patch(Patch{ID: gone.ID, Revision: gone.Revision, Sources: []string{src}, ChangeReason: "fix", Edits: edits}); !errors.Is(err, ErrArticleConflict) {
			t.Fatalf("%s: a withdrawn article is refused as a conflict: %v", name, err)
		}
	}
}

// The id of an article comes from its first title. After a rename and a withdrawal or a deletion, a lesson under the former title and one
// under the new title are both refused: the article cannot be updated, so the former title's refusal does not send the agent to update it,
// and the new title is not taken again by a live article.
func TestAGoneRenamedArticleKeepsItsTitlesFromANewLesson(t *testing.T) {
	for _, state := range []string{"withdrawn", "deleted"} {
		t.Run(state, func(t *testing.T) {
			b := Book{Root: t.TempDir(), Scope: "/work/project"}
			src := workSource(t, b, "agent", 1)
			first, err := b.Ingest(draft(src, "Commit rules"))
			if err != nil {
				t.Fatal(err)
			}
			renamed, err := b.Patch(Patch{ID: first.Page.ID, Revision: first.Page.Revision, Sources: []string{src}, ChangeReason: "the article is about pushing", Edits: []TextEdit{{Field: "title", Before: "Commit rules", After: "Push rules"}}})
			if err != nil {
				t.Fatal(err)
			}
			if state == "withdrawn" {
				_, err = b.Withdraw(Withdrawal{ID: renamed.Page.ID, Revision: renamed.Page.Revision, Sources: []string{src}, Reason: "the request was deleted"})
			} else {
				_, err = b.Manage(Change{ID: renamed.Page.ID, Revision: renamed.Page.Revision, Action: "delete"})
			}
			if err != nil {
				t.Fatal(err)
			}
			other := workSource(t, b, "agent", 2)
			if _, err := b.Ingest(draft(other, "Commit rules")); !errors.Is(err, ErrArticleConflict) || strings.Contains(err.Error(), "update that article") {
				t.Fatalf("the former title is refused without sending the agent to update a gone article: %v", err)
			}
			if _, err := b.Ingest(draft(other, "Push rules")); !errors.Is(err, ErrArticleConflict) {
				t.Fatalf("the new title of a gone article is not taken by a live one: %v", err)
			}
			if live, err := b.Live(); err != nil || len(live) != 0 {
				t.Fatalf("nothing is live: %+v %v", live, err)
			}
		})
	}
}

// The index names a withdrawn article as withdrawn even when another article names it in replaces; a live one that was replaced is
// "updated by a later lesson".
func TestTheIndexKeepsAWithdrawnArticleWithdrawnEvenWhenNamedInReplaces(t *testing.T) {
	pages := []Page{
		{ID: "0000000000000001", Title: "Withdrawn", Status: StatusWithdrawn},
		{ID: "0000000000000002", Title: "Live and replaced", Status: "hypothesis"},
		{ID: "0000000000000003", Title: "The replacement", Status: "hypothesis", Replaces: []string{"0000000000000001", "0000000000000002"}},
	}
	got := string(index(pages))
	for _, want := range []string{"[0000000000000001](0000000000000001.md) \u2014 \"Withdrawn\" / 0 sources / withdrawn\n", "\"Live and replaced\" / 0 sources / updated by a later lesson\n"} {
		if !strings.Contains(got, want) {
			t.Fatalf("the index lacks %q:\n%s", want, got)
		}
	}
}

// A memory upgrade does not start with a withdrawn article as a target, and a target withdrawn during the upgrade closes as excluded,
// never as reviewed.
func TestMemoryUpgradeExcludesWithdrawnArticles(t *testing.T) {
	b := Book{Root: t.TempDir(), Scope: "/synthetic-project"}
	_, before := withdrawnArticle(t, b, "Withdrawn before the upgrade", []string{workSource(t, b, "agent", 1)})
	during, err := b.Ingest(draft(workSource(t, b, "agent", 2), "Withdrawn during the upgrade"))
	if err != nil {
		t.Fatal(err)
	}
	u, err := b.MemoryUpgrade()
	if err != nil || u.Total != 1 || u.Withdrawn != 0 || len(u.Tasks) != 1 || u.Tasks[0].PageID != during.Page.ID {
		t.Fatalf("only the article that was not withdrawn is a target: %+v %v (withdrawn before: %s)", u, err, before.ID)
	}
	if _, err := b.Withdraw(Withdrawal{ID: during.Page.ID, Revision: during.Page.Revision, Sources: []string{during.Page.Sources[0].ID}, Reason: "the request was deleted"}); err != nil {
		t.Fatal(err)
	}
	u, err = b.MemoryUpgrade()
	if err != nil || u.Status != "complete" || u.Total != 1 || u.Reviewed != 0 || u.Withdrawn != 1 {
		t.Fatalf("a target withdrawn during the upgrade closes as excluded: %+v %v", u, err)
	}
}

// A person's edit of a withdrawn article, and a second withdrawal of it, are refused as a conflict before their own fields and sources are
// looked at: the same answer whatever they say.
func TestEditOrWithdrawalOfAWithdrawnArticleIsRefusedAsAConflict(t *testing.T) {
	b := Book{Root: t.TempDir(), Scope: "/work/project"}
	src := workSource(t, b, "agent", 1)
	_, gone := withdrawnArticle(t, b, "Waiting for a person", []string{src})
	if _, err := b.Manage(Change{ID: gone.ID, Revision: gone.Revision, Action: "edit", Title: "Edited", Lesson: "Edited", AppliesTo: "Docs", Kind: "rule", Scene: "always"}); !errors.Is(err, ErrArticleConflict) {
		t.Fatalf("an edit that fits is refused as a conflict: %v", err)
	}
	if _, err := b.Manage(Change{ID: gone.ID, Revision: gone.Revision, Action: "edit"}); !errors.Is(err, ErrArticleConflict) {
		t.Fatalf("an edit without a summary is refused as a conflict, not for what it lacks: %v", err)
	}
	if _, err := b.Withdraw(Withdrawal{ID: gone.ID, Revision: gone.Revision, Sources: []string{src}, Reason: "again"}); !errors.Is(err, ErrArticleConflict) {
		t.Fatalf("a withdrawal of a withdrawn article is a conflict, not a question about its sources: %v", err)
	}
}

// A reconciliation of a withdrawn article is unavailable, as for a deleted one; it does not go on to a preview that the save refuses.
func TestReconcileOfAWithdrawnArticleIsUnavailable(t *testing.T) {
	b := Book{Root: t.TempDir(), Scope: "/work/project"}
	src := workSource(t, b, "agent", 1)
	_, gone := withdrawnArticle(t, b, "Waiting for a person", []string{src})
	got, err := b.Reconcile(Patch{ID: gone.ID, Revision: gone.Revision, Sources: []string{src}, ChangeReason: "fix", Edits: []TextEdit{{Field: "lesson", Before: gone.Lesson, After: "Changed lesson."}}})
	if err != nil || got.Status != "unavailable" || got.Patch != nil {
		t.Fatalf("a withdrawn article is unavailable for a reconciliation: %+v %v", got, err)
	}
}

// A procedure chosen for articles is current only while they are live: a withdrawn article at its latest revision does not keep it current.
func TestAProcedureIsNotCurrentOnAWithdrawnArticle(t *testing.T) {
	b := Book{Root: t.TempDir(), Scope: "/work/project"}
	live, err := b.Ingest(draft(workSource(t, b, "agent", 1), "A live article"))
	if err != nil {
		t.Fatal(err)
	}
	_, gone := withdrawnArticle(t, b, "A withdrawn article", []string{workSource(t, b, "agent", 2)})
	basis := func(p Page) ProcedureBasis {
		return ProcedureBasis{Topic: "Update the README", Articles: []ArticleReference{{ID: p.ID, Revision: p.Revision}}}
	}
	if !b.procedureCurrent(basis(*live.Page)) {
		t.Fatal("a live article keeps a procedure current")
	}
	if b.procedureCurrent(basis(gone)) {
		t.Fatal("a withdrawn article at its latest revision keeps a procedure current")
	}
}
