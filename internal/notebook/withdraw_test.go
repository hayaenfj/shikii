package notebook

import (
	"errors"
	"strings"
	"testing"
)

// The program that saved an article's sources withdraws it when the work it came from is deleted: the article stops being delivered,
// and only an article whose every source the caller names can be withdrawn.
func TestWithdrawDeletesOnlyAnArticleWhoseSourcesTheCallerNames(t *testing.T) {
	b := Book{Root: t.TempDir(), Scope: "/work/project"}
	mine, other := seed(t, b, 1), seed(t, b, 2)
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
	if _, err := b.Withdraw(Withdrawal{ID: p.ID, Revision: p.Revision, Sources: []string{mine}, Reason: strings.Repeat("削", 301)}); err == nil {
		t.Fatal("a reason that does not fit the article's change reason is refused")
	}
	if _, err := b.Withdraw(Withdrawal{ID: p.ID, Revision: "0000000000000000", Sources: []string{mine}, Reason: "the request was deleted"}); !errors.Is(err, ErrArticleConflict) {
		t.Fatalf("a stale revision is refused: %v", err)
	}
	got, err := b.Withdraw(Withdrawal{ID: p.ID, Revision: p.Revision, Sources: []string{mine}, Reason: "the request was deleted"})
	if err != nil || got.Status != "deleted" || got.Previous != p.Revision {
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
