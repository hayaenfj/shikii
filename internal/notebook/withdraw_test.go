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

// The program that saved an article's sources withdraws it when the work it came from is deleted: the article stops being delivered,
// and only an article whose every source the caller names can be withdrawn.
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

// Naming the sources does not show an article is the tool's own: the ids of a person's article are printed by every query. Only an article
// whose every source is a record of a tool's work (kind agent, code or test) is withdrawn; one source of a person's words keeps it.
func TestWithdrawRefusesAnArticleThatCitesAPersonsWords(t *testing.T) {
	b := Book{Root: t.TempDir(), Scope: "/work/project"}
	cases := []struct {
		name    string
		sources []string
		ok      bool
	}{
		{"a conversation the hook recorded", []string{seed(t, b, 1)}, false},
		{"an observation of kind human", []string{workSource(t, b, "human", 2)}, false},
		{"a work record and an observation of kind human", []string{workSource(t, b, "agent", 3), workSource(t, b, "human", 4)}, false},
		{"a work record and a recorded conversation", []string{workSource(t, b, "code", 5), seed(t, b, 6)}, false},
		{"an observation of kind release", []string{workSource(t, b, "release", 7)}, false},
		{"records of kind agent, code and test", []string{workSource(t, b, "agent", 8), workSource(t, b, "code", 9), workSource(t, b, "test", 10)}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := draft(c.sources[0], "Article of "+c.name)
			d.Sources = c.sources
			res, err := b.Ingest(d)
			if err != nil {
				t.Fatal(err)
			}
			p := *res.Page
			got, err := b.Withdraw(Withdrawal{ID: p.ID, Revision: p.Revision, Sources: c.sources, Reason: "the request was deleted"})
			if c.ok {
				if err != nil || got.Status != "deleted" {
					t.Fatalf("the tool's own article is withdrawn: %+v %v", got, err)
				}
				return
			}
			if !errors.Is(err, ErrNotWithdrawable) {
				t.Fatalf("an article that cites a person's words stays: %+v %v", got.Status, err)
			}
			live, _ := b.Live()
			for _, l := range live {
				if l.ID == p.ID {
					return
				}
			}
			t.Fatal("the refused article is no longer delivered")
		})
	}
}
