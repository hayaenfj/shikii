package notebook

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// opensToCheckEachOnce is how many source files a reply that checks every article opens when each source the articles cite is read once:
// each record, and the archived long text of the one conversation that has it.
func (f sourceReadsFixture) opensToCheckEachOnce() uint64 {
	return uint64(len(f.records) + len(f.originals))
}

// opensOfAPlainBook is how many source files a Book that shares no view opens to check the pages one at a time (each page for itself):
// every citation, and every long text, again for each article that cites it.
func (f sourceReadsFixture) opensOfAPlainBook() uint64 {
	n := f.citations
	for _, p := range f.pages {
		for _, s := range p.Sources {
			if s.TextHash != "" {
				n++
			}
		}
	}
	return uint64(n)
}

// withSourceDamaged changes the file of a source, runs f, and puts the file back.
func withSourceDamaged(t *testing.T, path string, change func(path string, data []byte) error, f func()) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := change(path, data); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}()
	f()
}

func removeFile(path string, _ []byte) error { return os.Remove(path) }

// A list of the wiki without words (the reply of `shikii wiki query` with no word) is made of steps that each check the sources of the
// articles: the articles it lists, the pending observations, the maintenance tasks, the articles that still lack a kind and a scene.
// The sources are opened once for the whole reply, not once for each step: the count of opens is the measure, so this does not depend
// on how fast a machine is. The same holds when an article lacks a kind and a scene, which gives the last step work to do.
func TestAWordlessListOpensEachCitedSourceOnce(t *testing.T) {
	for _, c := range []struct {
		name         string
		unclassified bool
	}{
		{"every article has a kind and a scene", false},
		{"one article lacks a kind and a scene", true},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := newSourceReadsFixture(t)
			if c.unclassified {
				for _, p := range f.pages {
					if p.Title == "Delta pilot notes" {
						if err := f.book.RewriteWithoutKindScene(p.ID); err != nil {
							t.Fatal(err)
						}
					}
				}
			}
			want := f.opensToCheckEachOnce()
			var view View
			if got := opens(func() {
				var err error
				if view, err = f.book.QueryPage("", ""); err != nil {
					t.Fatal(err)
				}
			}); got != want {
				t.Fatalf("a list of the wiki opens each cited source once (%d: %d records and %d long text), got %d opens for %d citations", want, len(f.records), len(f.originals), got, f.citations)
			}
			if len(view.Lessons) != len(f.pages) || len(view.Issues) != 0 || len(view.Maintenance) == 0 || (view.UnclassifiedCount == 1) != c.unclassified {
				t.Fatalf("control: the list holds the five articles, no issue, the maintenance tasks and the unclassified article when there is one: %d lessons, %+v, %d tasks, %d unclassified", len(view.Lessons), view.Issues, len(view.Maintenance), view.UnclassifiedCount)
			}
			if got := opens(func() {
				if _, err := f.book.Query(""); err != nil {
					t.Fatal(err)
				}
			}); got != want {
				t.Fatalf("Query with no word opens each cited source once (%d), got %d opens", want, got)
			}
		})
	}
}

// The steps of a request that share one view check each source once between them, whichever step comes first and however many articles
// cite it. A Book that shares no view does not: each of its reads checks the pages one at a time, as before.
func TestTheStepsOfOneViewShareTheReadsOfTheSources(t *testing.T) {
	f := newSourceReadsFixture(t)
	view := f.book.ReadView()
	if got := opens(func() {
		if live, err := view.Live(); err != nil || len(live) != len(f.pages) {
			t.Fatalf("control: Live lists every article: %d %v", len(live), err)
		}
		if tasks, err := view.Maintenance(); err != nil || len(tasks) == 0 {
			t.Fatalf("control: Maintenance has tasks to do: %d %v", len(tasks), err)
		}
		if keys, err := view.MaintenanceKeys(); err != nil || len(keys) != len(f.pages) {
			t.Fatalf("control: MaintenanceKeys has a key for every article: %d %v", len(keys), err)
		}
		if _, err := view.Unclassified(); err != nil {
			t.Fatal(err)
		}
		if rules, err := view.AlwaysRules(); err != nil || len(rules) != 0 {
			t.Fatalf("control: no article is a rule that always applies: %d %v", len(rules), err)
		}
		for _, p := range f.pages {
			if _, ok, err := view.CurrentPage(p.ID); err != nil || !ok {
				t.Fatalf("control: CurrentPage finds %s: %v %v", p.ID, ok, err)
			}
		}
	}); got != f.opensToCheckEachOnce() {
		t.Fatalf("the steps of one view open each cited source once between them (%d), got %d opens for %d citations", f.opensToCheckEachOnce(), got, f.citations)
	}

	// no view: every read checks the pages one at a time, and a second read starts again
	want := f.opensOfAPlainBook()
	for round := 1; round <= 2; round++ {
		if got := opens(func() {
			if _, err := f.book.Maintenance(); err != nil {
				t.Fatal(err)
			}
		}); got != want {
			t.Fatalf("a Book with no view checks each page for itself, in round %d (%d opens for %d citations), got %d", round, want, f.citations, got)
		}
	}
}

// A source that changes in the middle of a request is seen as the request first read it, in every step of it: one reply does not say
// that an article is usable in one place and that its source is gone in another. The next request sees the change. This is true of a
// record that is removed and of a long text that is changed.
func TestASourceThatChangesInTheMiddleOfARequestIsSeenAsItWasFirstRead(t *testing.T) {
	for _, c := range []struct {
		name   string
		path   func(sourceReadsFixture) string
		change func(path string, data []byte) error
		kind   string
		citing []string
	}{
		{"a source record that is removed", func(f sourceReadsFixture) string { return filepath.Join(f.dir, f.plain[1]+".json") }, removeFile, "missing-source", []string{"Alpha harbor notes", "Beta harbor notes"}},
		{"a source record that is changed", func(f sourceReadsFixture) string { return filepath.Join(f.dir, f.plain[2]+".json") }, func(path string, data []byte) error {
			return os.WriteFile(path, []byte(strings.Replace(string(data), "source number", "source NUMBER", 1)), 0o600)
		}, "changed-source", []string{"Beta harbor notes", "Gamma harbor notes"}},
		{"a long text that is changed", func(f sourceReadsFixture) string { return filepath.Join(f.dir, f.conversation+".text.json") }, func(path string, _ []byte) error {
			return os.WriteFile(path, []byte(`[]`), 0o600)
		}, "changed-source-text", []string{"Delta pilot notes", "Epsilon pilot notes"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := newSourceReadsFixture(t)
			view := f.book.ReadView()
			if live, err := view.Live(); err != nil || len(live) != len(f.pages) {
				t.Fatalf("control: the view first reads every article usable: %d %v", len(live), err)
			}
			withSourceDamaged(t, c.path(f), c.change, func() {
				// the same view, later in the request: nothing is opened again and everything is as it was first read
				var later View
				if got := opens(func() {
					var err error
					if later, err = view.QueryPage("", ""); err != nil {
						t.Fatal(err)
					}
				}); got != 0 {
					t.Fatalf("the view reads no source again, got %d opens", got)
				}
				if len(later.Issues) != 0 || len(later.Lessons) != len(f.pages) {
					t.Fatalf("the view reports what it first read: %d lessons, issues %+v", len(later.Lessons), later.Issues)
				}
				if live, err := view.Live(); err != nil || len(live) != len(f.pages) {
					t.Fatalf("Live in the same view agrees: %d %v", len(live), err)
				}
				// the next request sees it, in a new view and in the plain forms
				next, err := f.book.ReadView().Live()
				if err != nil || len(next) != len(f.pages)-len(c.citing) {
					t.Fatalf("a new view sees the change: %d articles usable, want %d (%v)", len(next), len(f.pages)-len(c.citing), err)
				}
				fresh, err := f.book.QueryPage("", "")
				if err != nil || len(fresh.Lessons) != len(f.pages)-len(c.citing) || len(fresh.Issues) != len(c.citing) {
					t.Fatalf("the next list sees the change: %d lessons, issues %+v (%v)", len(fresh.Lessons), fresh.Issues, err)
				}
				for _, issue := range fresh.Issues {
					if issue.Kind != c.kind {
						t.Fatalf("the next list reports %s: %+v", c.kind, fresh.Issues)
					}
				}
			})
		})
	}
}

// What a request reads of the sources is not kept for the next one, in any form of view: a view made after a source changed sees it,
// whether it is made by ReadView, by the first step of a search, or by the preparation of a snapshot, which checks its sources again at
// the commit besides. The views of a process do not share what they read.
func TestTheReadsOfTheSourcesOfAViewEndWithTheView(t *testing.T) {
	f := newSourceReadsFixture(t)
	record := filepath.Join(f.dir, f.plain[1]+".json")
	usable := len(f.pages)
	lessThan := usable - 2 // the two articles that cite the record

	views := map[string]func() Book{
		"ReadView":      func() Book { return f.book.ReadView() },
		"sharingParses": func() Book { return f.book.sharingParses() },
		"PrepareReadSnapshot": func() Book {
			b, err := f.book.PrepareReadSnapshot()
			if err != nil {
				t.Fatal(err)
			}
			return b
		},
	}
	for _, name := range []string{"ReadView", "sharingParses", "PrepareReadSnapshot"} {
		t.Run(name, func(t *testing.T) {
			first := views[name]()
			if live, err := first.Live(); err != nil || len(live) != usable {
				t.Fatalf("control: the first view reads every article usable: %d %v", len(live), err)
			}
			withSourceDamaged(t, record, removeFile, func() {
				second := views[name]()
				if live, err := second.Live(); err != nil || len(live) != lessThan {
					t.Fatalf("a view made after the record was removed sees it: %d articles usable, want %d (%v)", len(live), lessThan, err)
				}
				if name == "PrepareReadSnapshot" {
					if err := first.ValidateRead(); err == nil {
						t.Fatal("a snapshot that read the record rejects the commit once the record is gone")
					}
				}
			})
		})
	}
}

// A snapshot shares its reads through sharedCheck, as before: a step after the first opens no source again, and the sources it read
// are checked again at the commit.
func TestASnapshotKeepsSharingItsOwnReads(t *testing.T) {
	f := newSourceReadsFixture(t)
	snapshot, err := f.book.PrepareReadSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	if live, err := snapshot.Live(); err != nil || len(live) != len(f.pages) {
		t.Fatalf("control: the snapshot reads every article usable: %d %v", len(live), err)
	}
	if got := opens(func() {
		if _, err := snapshot.Maintenance(); err != nil {
			t.Fatal(err)
		}
	}); got != 0 {
		t.Fatalf("a snapshot opens no source it has read, got %d opens", got)
	}
	if got := opens(func() {
		if err := snapshot.ValidateRead(); err != nil {
			t.Fatal(err)
		}
	}); got < f.opensToCheckEachOnce() {
		t.Fatalf("the commit reads every source the snapshot read again: at least %d opens, got %d", f.opensToCheckEachOnce(), got)
	}
}

// A view keeps what it read of a source, not what it failed to read: a source that was not there when the view first looked (a record,
// an archived long text, or the whole sources folder) and is saved in the middle of the request is seen by the next step of the request.
// Sources are written once and never rewritten, so a read that succeeded stays true, and a read that failed may not. Once it is read,
// the view keeps it, so the step after that opens nothing.
func TestASourceSavedInTheMiddleOfARequestIsSeenByTheNextStep(t *testing.T) {
	for _, c := range []struct {
		name   string
		hidden func(sourceReadsFixture) string // the file or folder that is not there at first
		kind   string
		lost   int // how many articles cannot be used while it is not there
	}{
		{"a source record that is saved", func(f sourceReadsFixture) string { return filepath.Join(f.dir, f.plain[1]+".json") }, "missing-source", 2},
		{"an archived long text that is saved", func(f sourceReadsFixture) string { return filepath.Join(f.dir, f.conversation+".text.json") }, "changed-source-text", 2},
		{"a sources folder that is created", func(f sourceReadsFixture) string { return f.dir }, "missing-source", 5},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := newSourceReadsFixture(t)
			path, away := c.hidden(f), filepath.Join(t.TempDir(), "saved-later")
			if err := os.Rename(path, away); err != nil {
				t.Fatal(err)
			}
			view := f.book.ReadView()
			first, err := view.Live()
			if err != nil || len(first) != len(f.pages)-c.lost {
				t.Fatalf("control: while it is missing, %d articles are usable, got %d (%v)", len(f.pages)-c.lost, len(first), err)
			}
			listed := c.lost < len(f.pages) // with the folder gone the list cannot read the pending sources, so only the others ask for it
			if listed {
				if v, err := view.QueryPage("", ""); err != nil || len(v.Issues) == 0 || v.Issues[0].Kind != c.kind {
					t.Fatalf("control: the list reports %s while it is missing: %+v (%v)", c.kind, v.Issues, err)
				}
			}
			if err := os.Rename(away, path); err != nil { // the source is saved
				t.Fatal(err)
			}
			if got, err := view.Live(); err != nil || len(got) != len(f.pages) {
				t.Fatalf("the next step of the same view sees the source that was saved: %d articles usable, want %d (%v)", len(got), len(f.pages), err)
			}
			if listed {
				if v, err := view.QueryPage("", ""); err != nil || len(v.Issues) != 0 || len(v.Lessons) != len(f.pages) {
					t.Fatalf("the list of the same view no longer reports it: %d lessons, %+v (%v)", len(v.Lessons), v.Issues, err)
				}
			}
			if got := opens(func() {
				if live, err := view.Live(); err != nil || len(live) != len(f.pages) {
					t.Fatalf("control: the step after that agrees: %d %v", len(live), err)
				}
			}); got != 0 {
				t.Fatalf("what the view read after the source was saved is kept: the next step opens no source, got %d opens", got)
			}
		})
	}
}
