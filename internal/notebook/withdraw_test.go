package notebook

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

// workSource saves a source of the given kind the way a tool saves what it saw in its own work, and returns its id.
func workSource(t *testing.T, b Book, kind string, n int) string {
	t.Helper()
	o, err := b.CaptureObserved(Observation{Kind: kind, Title: fmt.Sprintf("Work record %d", n), Content: fmt.Sprintf("what the work did, %d", n), Reference: "work record", Version: "1", ObservedAt: time.Unix(int64(1000+n), 0)})
	if err != nil {
		t.Fatal(err)
	}
	return o.ID
}

// The program that saved an article's sources withdraws it when the work it came from is deleted: the article stops being delivered
// but stays, and only an article whose every source the caller names can be withdrawn.
func TestWithdrawDeletesOnlyAnArticleWhoseSourcesTheCallerNames(t *testing.T) {
	b := Book{Root: t.TempDir(), Scope: "/work/project"}
	mine, other := workSource(t, b, "agent", 1), workSource(t, b, "agent", 2)
	own, err := b.Ingest(draft(mine, "Reached work of a deleted request"))
	if err != nil {
		t.Fatal(err)
	}
	shared := draft(mine, "Article that also cites another source")
	shared.Sources = []string{mine, other}
	both, err := b.Ingest(shared)
	if err != nil {
		t.Fatal(err)
	}
	p, q := *own.Page, *both.Page
	if _, err := b.Withdraw(Withdrawal{ID: q.ID, Revision: q.Revision, Sources: []string{mine}, Reason: "the request was deleted"}); !errors.Is(err, ErrNotWithdrawable) {
		t.Fatalf("an article citing a source the caller did not name stays: %v", err)
	}
	if _, err := b.Withdraw(Withdrawal{ID: p.ID, Revision: p.Revision, Sources: []string{mine}}); err == nil {
		t.Fatal("a withdrawal says why")
	}
	if _, err := b.Withdraw(Withdrawal{ID: p.ID, Revision: p.Revision, Sources: []string{mine}, Reason: strings.Repeat("\u524a", 301)}); err == nil {
		t.Fatal("a reason that does not fit the article's change reason is refused")
	}
	if _, err := b.Withdraw(Withdrawal{ID: p.ID, Revision: "0000000000000000", Sources: []string{mine}, Reason: "the request was deleted"}); !errors.Is(err, ErrArticleConflict) {
		t.Fatalf("a stale revision is refused: %v", err)
	}
	got, err := b.Withdraw(Withdrawal{ID: p.ID, Revision: p.Revision, Sources: []string{mine}, Reason: "the request was deleted"})
	if err != nil || got.Status != "withdrawn" || got.Previous != p.Revision {
		t.Fatalf("withdraw: %+v %v", got, err)
	}
	pages, err := b.Live()
	if err != nil || len(pages) != 1 || pages[0].ID != q.ID {
		t.Fatalf("only the other article is delivered: %+v %v", pages, err)
	}
	if pending, err := b.PendingObservations(); err != nil || len(pending) != 0 {
		t.Fatalf("the withdrawal needs no ingestion: %v %v", pending, err)
	}
}

// withdrawnArticle builds a rule that applies to every request from the given sources and withdraws it, naming all of them. It returns
// the article as it was before and as the withdrawal left it.
func withdrawnArticle(t *testing.T, b Book, title string, sources []string) (before, after Page) {
	t.Helper()
	d := draft(sources[0], title)
	d.Sources, d.Scene = sources, "always"
	res, err := b.Ingest(d)
	if err != nil {
		t.Fatal(err)
	}
	before = *res.Page
	after, err = b.Withdraw(Withdrawal{ID: before.ID, Revision: before.Revision, Sources: sources, Reason: "the request was deleted"})
	if err != nil {
		t.Fatalf("withdraw: %v", err)
	}
	return before, after
}

// delivered says whether the article reaches the agent by any of the ways the wiki hands articles out.
func delivered(t *testing.T, b Book, id string) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	for name, read := range map[string]func() ([]Page, error){
		"live": b.Live, "always": b.AlwaysRules,
		"recall": func() ([]Page, error) { return b.Recall("README installation instructions") },
		"query":  func() ([]Page, error) { v, err := b.Query("README installation instructions"); return v.Lessons, err },
		"list":   func() ([]Page, error) { v, err := b.Query(""); return v.Lessons, err },
	} {
		pages, err := read()
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		for _, p := range pages {
			if p.ID == id {
				out[name] = true
			}
		}
	}
	return out
}

// A withdrawal does not delete: the article stays as a withdrawn one, with its text, history and sources, and is not handed out. This holds
// for an article built from a person's words too, because the person can restore or delete it.
func TestWithdrawKeepsTheArticleAsWithdrawnAndDoesNotDeliverIt(t *testing.T) {
	b := Book{Root: t.TempDir(), Scope: "/work/project"}
	words := []string{seed(t, b, 1), workSource(t, b, "human", 2), workSource(t, b, "agent", 3)}
	before, got := withdrawnArticle(t, b, "A rule from a person's words", words)
	if got.Status != "withdrawn" || got.Previous != before.Revision || got.Lesson != before.Lesson || got.Body != before.Body || len(got.Sources) != len(before.Sources)+1 || !strings.Contains(got.ChangeReason, "the request was deleted") {
		t.Fatalf("a withdrawn article keeps its text and sources and says why: %+v", got)
	}
	if before := delivered(t, b, before.ID); len(before) != 0 {
		t.Fatalf("a withdrawn article is handed out by %v", before)
	}
	read, err := b.Query("lesson:" + got.ID)
	if err != nil || len(read.Lessons) != 1 || read.Lessons[0].Status != "withdrawn" || read.Lessons[0].Revision != got.Revision {
		t.Fatalf("a withdrawn article is still readable by its id: %+v %v", read, err)
	}
	if old, err := b.Query("lesson:" + before.ID + "@" + before.Revision); err != nil || len(old.Lessons) != 1 || old.Lessons[0].Status != "hypothesis" {
		t.Fatalf("its history stays: %+v %v", old, err)
	}
	if _, err := b.Withdraw(Withdrawal{ID: got.ID, Revision: got.Revision, Sources: []string{words[0], words[1], words[2]}, Reason: "again"}); !errors.Is(err, ErrNotWithdrawable) && !errors.Is(err, ErrArticleConflict) {
		t.Fatalf("a withdrawn article is not withdrawn again: %v", err)
	}
}

// A person restores a withdrawn article and it is handed out again; a restore of the revision the person did not see is refused.
func TestAPersonRestoresAWithdrawnArticle(t *testing.T) {
	b := Book{Root: t.TempDir(), Scope: "/work/project"}
	before, got := withdrawnArticle(t, b, "A rule from a person's words", []string{seed(t, b, 1)})
	if _, err := b.Manage(Change{ID: got.ID, Revision: before.Revision, Action: "restore"}); !errors.Is(err, ErrArticleConflict) {
		t.Fatalf("a restore of another revision than the withdrawn one is refused: %v", err)
	}
	if live := delivered(t, b, got.ID); len(live) != 0 {
		t.Fatalf("a refused restore hands nothing out: %v", live)
	}
	restored, err := b.Manage(Change{ID: got.ID, Revision: got.Revision, Action: "restore"})
	if err != nil || restored.Status != "hypothesis" || restored.Previous != got.Revision || restored.Lesson != before.Lesson {
		t.Fatalf("restore: %+v %v", restored, err)
	}
	for _, way := range []string{"live", "always", "recall", "query", "list"} {
		if !delivered(t, b, got.ID)[way] {
			t.Fatalf("a restored article is handed out by %s", way)
		}
	}
	if _, err := b.Manage(Change{ID: got.ID, Revision: restored.Revision, Action: "restore"}); !errors.Is(err, ErrArticleConflict) {
		t.Fatalf("an article that is not withdrawn is not restored: %v", err)
	}
}

// A person deletes a withdrawn article as the page deletes any article; only the revision the person saw is deleted, and a deleted
// article cannot be restored.
func TestAPersonDeletesAWithdrawnArticle(t *testing.T) {
	b := Book{Root: t.TempDir(), Scope: "/work/project"}
	before, got := withdrawnArticle(t, b, "A rule from a person's words", []string{seed(t, b, 1)})
	if _, err := b.Manage(Change{ID: got.ID, Revision: before.Revision, Action: "delete"}); !errors.Is(err, ErrArticleConflict) {
		t.Fatalf("a delete of another revision than the withdrawn one is refused: %v", err)
	}
	if read, _ := b.Query("lesson:" + got.ID); len(read.Lessons) != 1 || read.Lessons[0].Status != "withdrawn" {
		t.Fatalf("a refused delete leaves the article withdrawn: %+v", read.Lessons)
	}
	deleted, err := b.Manage(Change{ID: got.ID, Revision: got.Revision, Action: "delete"})
	if err != nil || deleted.Status != "deleted" || deleted.Previous != got.Revision {
		t.Fatalf("delete: %+v %v", deleted, err)
	}
	if _, err := b.Manage(Change{ID: got.ID, Revision: deleted.Revision, Action: "restore"}); !errors.Is(err, ErrArticleConflict) {
		t.Fatalf("a deleted article is not restored: %v", err)
	}
	if live := delivered(t, b, got.ID); len(live) != 0 {
		t.Fatalf("a deleted article is handed out by %v", live)
	}
}

// Only Withdraw makes a withdrawn article: a change that asks for it by name, an edit of a withdrawn article, a lesson saved under its
// title and a patch of it are all refused and leave it withdrawn.
func TestOnlyAWithdrawalWithdrawsAndNothingElseTouchesAWithdrawnArticle(t *testing.T) {
	b := Book{Root: t.TempDir(), Scope: "/work/project"}
	src := seed(t, b, 1)
	res, err := b.Ingest(draft(src, "A live rule"))
	if err != nil {
		t.Fatal(err)
	}
	live := *res.Page
	if _, err := b.Manage(Change{ID: live.ID, Revision: live.Revision, Action: "withdraw"}); err == nil {
		t.Fatal("a change sent by name withdraws an article")
	}
	if read, _ := b.Query("lesson:" + live.ID); len(read.Lessons) != 1 || read.Lessons[0].Status != "hypothesis" {
		t.Fatalf("the article is still live: %+v", read.Lessons)
	}
	got, err := b.Withdraw(Withdrawal{ID: live.ID, Revision: live.Revision, Sources: []string{src}, Reason: "the request was deleted"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.Manage(Change{ID: got.ID, Revision: got.Revision, Action: "edit", Title: "Edited", Lesson: "Edited", AppliesTo: "Docs", Kind: "rule", Scene: "always"}); !errors.Is(err, ErrArticleConflict) {
		t.Fatalf("a withdrawn article is not edited: %v", err)
	}
	if _, err := b.Ingest(draft(src, "A live rule")); !errors.Is(err, ErrArticleConflict) {
		t.Fatalf("a lesson saved under the title of a withdrawn article does not bring it back: %v", err)
	}
	if _, err := b.Patch(Patch{ID: got.ID, Revision: got.Revision, Sources: []string{src}, ChangeReason: "fix", Edits: []TextEdit{{Field: "lesson", Before: got.Lesson, After: "changed"}}}); err == nil {
		t.Fatal("a withdrawn article is patched")
	}
	if read, _ := b.Query("lesson:" + got.ID); len(read.Lessons) != 1 || read.Lessons[0].Status != "withdrawn" || read.Lessons[0].Revision != got.Revision {
		t.Fatalf("the article is still withdrawn, unchanged: %+v", read.Lessons)
	}
}

// The Knowledge page lists the articles a work withdrew and nobody has decided on, with why and the work they came from; an article the
// person restored or deleted, and one nobody withdrew, is not in the list.
func TestWithdrawnListsOnlyTheArticlesWaitingForAPerson(t *testing.T) {
	b := Book{Root: t.TempDir(), Scope: "/work/project"}
	if none, err := b.Withdrawn(); err != nil || len(none) != 0 {
		t.Fatalf("a wiki without withdrawals lists none: %v %v", none, err)
	}
	record, conversation := workSource(t, b, "agent", 1), seed(t, b, 2)
	_, waiting := withdrawnArticle(t, b, "Waiting for a person", []string{record, conversation})
	_, restored := withdrawnArticle(t, b, "Restored by a person", []string{workSource(t, b, "agent", 3)})
	_, deleted := withdrawnArticle(t, b, "Deleted by a person", []string{workSource(t, b, "agent", 4)})
	if _, err := b.Manage(Change{ID: restored.ID, Revision: restored.Revision, Action: "restore"}); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Manage(Change{ID: deleted.ID, Revision: deleted.Revision, Action: "delete"}); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Ingest(draft(workSource(t, b, "agent", 5), "Nobody withdrew this")); err != nil {
		t.Fatal(err)
	}
	list, err := b.Withdrawn()
	if err != nil || len(list) != 1 || list[0].ID != waiting.ID || list[0].Revision != waiting.Revision || list[0].Status != "withdrawn" {
		t.Fatalf("only the article waiting for a person is listed: %+v %v", list, err)
	}
	if list[0].Withdrawal.Reason != "the request was deleted" || len(list[0].Withdrawal.Work) != 1 || list[0].Withdrawal.Work[0] != "Work record 1" || len(list[0].Sources) != 3 {
		t.Fatalf("the list says why and from which work (a source that is a recorded conversation has no title): %+v", list[0])
	}
	if live := delivered(t, b, waiting.ID); len(live) != 0 {
		t.Fatalf("a listed article is not delivered: %v", live)
	}
}

// The inventory kept with a delivery names a withdrawn article as withdrawn, apart from a deleted one, and hands none of it out.
func TestADeliveryInventoryNamesAWithdrawnArticleAsWithdrawn(t *testing.T) {
	b := Book{Root: t.TempDir(), Scope: "/work/project"}
	_, got := withdrawnArticle(t, b, "A rule from a person's words", []string{seed(t, b, 1)})
	pages, c, err := b.DeliverySnapshot(nil)
	if err != nil || len(pages) != 0 || len(c.Entries) != 1 || c.Entries[0].ID != got.ID || c.Entries[0].Retrieval != "withdrawn" {
		t.Fatalf("a withdrawn article is in the inventory as withdrawn and is not eligible: %+v %+v %v", pages, c.Entries, err)
	}
	c.Complete = true
	if _, err := b.HistoricalPages(c); err != nil {
		t.Fatalf("a withdrawn entry is skipped when a case is compared: %v", err)
	}
}
