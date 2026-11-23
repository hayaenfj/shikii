package notebook

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/store"
)

// sourceReadsFixture is a wiki whose articles cite sources that other articles cite too, one of them (a conversation) with its
// archived long text, so that the reads of the sources of a search can be told from the citations that need them. Every article has a
// kind and a scene.
type sourceReadsFixture struct {
	book         Book
	pages        []Page   // the live articles, in id order
	plain        []string // the four sources without a long text, in the order they were saved
	records      []string // the distinct sources the articles cite (each has a record)
	originals    []string // those of them that have an archived long text
	citations    int      // how many times the articles cite a source: more than the distinct sources, or sharing proves nothing
	conversation string   // the source with a long text
	dir          string   // the sources directory
}

func newSourceReadsFixture(t *testing.T) sourceReadsFixture {
	t.Helper()
	b := Book{Root: t.TempDir(), Scope: "/work/source-reads"}
	var plain []string
	for i := 1; i <= 4; i++ {
		o, err := b.capture(Observation{Kind: "test", Title: fmt.Sprintf("Source %d", i), Content: fmt.Sprintf("The text of source number %d.", i), Reference: fmt.Sprintf("synthetic-%d", i), Version: "1", ObservedAt: time.Unix(1, 0)})
		if err != nil {
			t.Fatal(err)
		}
		plain = append(plain, o.ID)
	}
	st := store.Store{Dir: b.Root}
	request, summary := "harbor pilots", strings.Repeat("A", 1500)
	rec := store.Record{TraceID: "0000000000000012", SpecID: "done-as-asked", Inputs: map[string]string{"request": request, "summary": summary}, At: time.Unix(1, 0)}
	rec.InputsHash = store.HashInputs(rec.Inputs)
	if err := st.AppendRecord(rec); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveRecordText(rec, map[string]string{"summary": summary + strings.Repeat("B", 4000) + "END-OF-ORIGINAL"}); err != nil {
		t.Fatal(err)
	}
	conversation := store.Feedback{ID: "0000000000000013", Request: request, Summary: summary, Reply: "Check the tide table", Targets: []store.ReviewTarget{{Spec: rec.SpecID, TraceID: rec.TraceID}}}
	if err := st.AppendFeedback(b.Scope, conversation); err != nil {
		t.Fatal(err)
	}
	for _, a := range []struct {
		title, lesson string
		cites         []string
	}{
		{"Alpha harbor notes", "Check the harbor tide before the first boat.", []string{plain[0], plain[1]}},
		{"Beta harbor notes", "Check the harbor lights after the last boat.", []string{plain[1], plain[2]}},
		{"Gamma harbor notes", "Check the harbor chains in the winter.", []string{plain[2], plain[0]}},
		{"Delta pilot notes", "Ask the pilot about the sandbank.", []string{plain[3], conversation.ID}},
		{"Epsilon pilot notes", "Ask the pilot about the fog horn.", []string{conversation.ID}},
	} {
		if _, err := b.Ingest(Draft{Title: a.title, Lesson: a.lesson, AppliesTo: a.title, Kind: "howto", Scene: "reading notes", Sources: a.cites}); err != nil {
			t.Fatal(err)
		}
	}
	live, err := b.Live()
	if err != nil || len(live) != 5 {
		t.Fatalf("control: the wiki holds five articles: %d %v", len(live), err)
	}
	dir, err := b.directory(false, "sources")
	if err != nil {
		t.Fatal(err)
	}
	f := sourceReadsFixture{book: b, pages: live, plain: plain, conversation: conversation.ID, dir: dir}
	for _, p := range live {
		for _, s := range p.Sources {
			f.citations++
			if !slices.Contains(f.records, s.ID) {
				f.records = append(f.records, s.ID)
			}
			if s.TextHash != "" && !slices.Contains(f.originals, s.ID) {
				f.originals = append(f.originals, s.ID)
			}
		}
	}
	if len(f.records) != 5 || len(f.originals) != 1 || f.originals[0] != conversation.ID || f.citations != 9 {
		t.Fatalf("control: five sources are cited nine times, one of them with a long text: %d %d %d", len(f.records), len(f.originals), f.citations)
	}
	return f
}

// opens is how many source files the calls of run opened.
func opens(run func()) uint64 {
	before := sourceOpens.Load()
	run()
	return sourceOpens.Load() - before
}

// A search of the wiki opens each source its articles cite once, however many articles cite it and however many steps the reply is
// made of: the search of the articles, the list of those that still lack a kind and a scene, the words searched alone. The count of
// opens is the measure, so this does not depend on how fast a machine is. A search of words that find nothing and of words that find
// an article are the same in this, and so is a reference to one article (the last by id, so that every article is checked on the way).
func TestOneSearchOpensEachCitedSourceOnce(t *testing.T) {
	f := newSourceReadsFixture(t)
	want := uint64(len(f.records) + len(f.originals)) // each record once, and the long text of the one source that has it
	last := f.pages[len(f.pages)-1].ID
	for _, c := range []struct {
		name, query string
		check       func(View, []string) bool
	}{
		{"words that find nothing", "alphaword harbor betaword gammaword", func(v View, alone []string) bool {
			return v.Search != nil && v.Search.Total == 0 && slices.Equal(alone, []string{"harbor"})
		}},
		{"words that find an article", "harbor", func(v View, _ []string) bool { return v.Search != nil && v.Search.Total == 3 }},
		{"a reference to one article", "lesson:" + last, func(v View, _ []string) bool { return len(v.Lessons) == 1 && v.Lessons[0].ID == last }},
	} {
		t.Run(c.name, func(t *testing.T) {
			var view View
			var alone []string
			got := opens(func() {
				var err error
				if view, err = f.book.QueryPage(c.query, ""); err != nil {
					t.Fatal(err)
				}
				if alone, err = f.book.WordsFoundAlone(c.query, "", view); err != nil {
					t.Fatal(err)
				}
			})
			if !c.check(view, alone) {
				t.Fatalf("control: the search of %q answers as expected: %+v %q", c.query, view.Search, alone)
			}
			if got != want {
				t.Fatalf("a search of %q opens each cited source once (%d: %d records and %d long text), got %d opens for %d citations", c.query, want, len(f.records), len(f.originals), got, f.citations)
			}
		})
	}
}

// The list of the articles that still lack a kind and a scene checks the sources of those articles only: a wiki whose articles all have
// them opens none, and one without them opens the sources of that article, once, however many articles cite those sources.
func TestRemainingUnclassifiedOpensOnlyTheSourcesOfUnclassifiedArticles(t *testing.T) {
	f := newSourceReadsFixture(t)
	if got := opens(func() {
		if r := f.book.RemainingUnclassified(); r.UnclassifiedCount != 0 {
			t.Fatalf("control: every article has a kind and a scene: %+v", r)
		}
	}); got != 0 {
		t.Fatalf("a wiki whose articles all have a kind and a scene opens no source to list the unclassified ones, got %d opens", got)
	}

	var delta Page // cites a source of its own and the conversation, which has a long text
	for _, p := range f.pages {
		if p.Title == "Delta pilot notes" {
			delta = p
		}
	}
	if err := f.book.RewriteWithoutKindScene(delta.ID); err != nil {
		t.Fatal(err)
	}
	var remaining Remaining
	got := opens(func() { remaining = f.book.RemainingUnclassified() })
	if remaining.UnclassifiedCount != 1 || len(remaining.Unclassified) != 1 || remaining.Unclassified[0].ID != delta.ID {
		t.Fatalf("control: the article without a kind and a scene is listed: %+v", remaining)
	}
	if want := uint64(len(delta.Sources) + 1); got != want { // the two records and the long text of the conversation
		t.Fatalf("the sources of the one unclassified article are opened once each (%d), got %d opens", want, got)
	}

	// an unclassified article whose source changed is not listed (it is not usable in search), as before
	path := filepath.Join(f.dir, f.plain[3]+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(strings.Replace(string(data), "source number", "source NUMBER", 1)), 0o600); err != nil {
		t.Fatal(err)
	}
	if r := f.book.RemainingUnclassified(); r.UnclassifiedCount != 0 {
		t.Fatalf("an unclassified article whose source changed is not listed: %+v", r)
	}
}

// The sources one search shares end with the search, they are not a cache of the process: a second search opens every source again, and
// sees a source that was removed, changed or restored between the two.
func TestTheSourceReadsASearchSharesEndWithTheSearch(t *testing.T) {
	f := newSourceReadsFixture(t)
	want := uint64(len(f.records) + len(f.originals))
	query := "alphaword harbor betaword gammaword"
	search := func() (View, uint64) {
		var view View
		got := opens(func() {
			var err error
			if view, err = f.book.QueryPage(query, ""); err != nil {
				t.Fatal(err)
			}
			if _, err = f.book.WordsFoundAlone(query, "", view); err != nil {
				t.Fatal(err)
			}
		})
		return view, got
	}
	if view, got := search(); got != want || len(view.Issues) != 0 {
		t.Fatalf("control: the first search opens each source once and finds no issue: %d opens, %+v", got, view.Issues)
	}
	shared := f.plain[1] // cited by two articles, so one missing record is two issues
	path := filepath.Join(f.dir, shared+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	view, got := search()
	if got != want {
		t.Fatalf("the second search opens each source again, even the one that is gone: %d opens, want %d", got, want)
	}
	if issues := kindsOf(view.Issues); !slices.Equal(issues, []string{"missing-source", "missing-source"}) {
		t.Fatalf("the second search sees the source that was removed, for each article that cites it: %+v", view.Issues)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if view, got = search(); got != want || len(view.Issues) != 0 {
		t.Fatalf("the third search sees the source restored: %d opens, %+v", got, view.Issues)
	}
}

func kindsOf(issues []Issue) []string {
	out := []string{}
	for _, issue := range issues {
		out = append(out, issue.Kind)
	}
	return out
}

// A source that is removed or changed, or a long text that is removed or changed, is reported in every form of a search as it is when
// the pages are checked one at a time: the same issues for each article that cites it, in the order of the articles and of their
// sources, and no article that cites it is returned or counted. A source cited by several articles is reported for each of them.
func TestChangedSourcesAreReportedInEveryFormOfASearch(t *testing.T) {
	for _, c := range []struct {
		name   string
		source func(sourceReadsFixture) string // the file that changes
		change func(path string, data []byte) error
		kind   string
		citing func(f sourceReadsFixture) []string // titles of the articles that cite it
	}{
		{"a source record that is removed", func(f sourceReadsFixture) string { return filepath.Join(f.dir, f.plain[1]+".json") },
			func(path string, _ []byte) error { return os.Remove(path) }, "missing-source", func(sourceReadsFixture) []string { return []string{"Alpha harbor notes", "Beta harbor notes"} }},
		{"a source record that is changed", func(f sourceReadsFixture) string { return filepath.Join(f.dir, f.plain[2]+".json") },
			func(path string, data []byte) error {
				return os.WriteFile(path, []byte(strings.Replace(string(data), "source number", "source NUMBER", 1)), 0o600)
			}, "changed-source", func(sourceReadsFixture) []string { return []string{"Beta harbor notes", "Gamma harbor notes"} }},
		{"a long text that is changed", func(f sourceReadsFixture) string { return filepath.Join(f.dir, f.conversation+".text.json") },
			func(path string, _ []byte) error { return os.WriteFile(path, []byte(`[]`), 0o600) }, "changed-source-text", func(sourceReadsFixture) []string { return []string{"Delta pilot notes", "Epsilon pilot notes"} }},
		{"a long text that is removed", func(f sourceReadsFixture) string { return filepath.Join(f.dir, f.conversation+".text.json") },
			func(path string, _ []byte) error { return os.Remove(path) }, "changed-source-text", func(sourceReadsFixture) []string { return []string{"Delta pilot notes", "Epsilon pilot notes"} }},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := newSourceReadsFixture(t)
			path := c.source(f)
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := c.change(path, data); err != nil {
				t.Fatal(err)
			}
			source := strings.TrimSuffix(strings.TrimSuffix(filepath.Base(path), ".json"), ".text")
			titles := c.citing(f)
			want, bad := []Issue{}, map[string]bool{}
			for _, p := range f.pages { // in id order, each article's sources in the order it cites them
				if !slices.Contains(titles, p.Title) {
					continue
				}
				bad[p.ID] = true
				for _, s := range p.Sources {
					if s.ID == source {
						want = append(want, Issue{Kind: c.kind, Page: p.ID, Source: s.ID})
					}
				}
			}
			if len(bad) != 2 || len(want) != 2 {
				t.Fatalf("control: two articles cite the source that changed: %v %v", bad, want)
			}
			// a reference to one article stops at it: it reports the issues of the articles before it, in id order. The last article whose
			// sources are intact is the one referred to
			targetAt := -1
			for i, p := range f.pages {
				if !bad[p.ID] {
					targetAt = i
				}
			}
			if targetAt < 0 {
				t.Fatal("control: an article whose sources are intact")
			}
			target, upTo := f.pages[targetAt].ID, []Issue{}
			for _, issue := range want {
				for i, p := range f.pages {
					if p.ID == issue.Page && i < targetAt {
						upTo = append(upTo, issue)
					}
				}
			}
			for _, q := range []string{"alphaword harbor betaword gammaword", "harbor pilot", "", "lesson:" + target} {
				view, err := f.book.QueryPage(q, "")
				if err != nil {
					t.Fatal(err)
				}
				wantHere := want
				if strings.HasPrefix(q, "lesson:") {
					wantHere = upTo
				}
				if got, wantJSON := jsonOf(t, view.Issues), jsonOf(t, wantHere); got != wantJSON {
					t.Fatalf("a search of %q reports the changed source for each article that cites it\n got %s\nwant %s", q, got, wantJSON)
				}
				for _, p := range view.Lessons {
					if bad[p.ID] {
						t.Fatalf("a search of %q returns an article whose source changed: %s", q, p.Title)
					}
				}
				if view.Search != nil {
					for _, candidate := range view.Search.Candidates {
						if slices.Contains(titles, candidate.Title) {
							t.Fatalf("a search of %q counts an article whose source changed: %s", q, candidate.Title)
						}
					}
				}
				if q == "" && len(view.Lessons) != len(f.pages)-len(bad) {
					t.Fatalf("a search of no words lists the %d articles whose sources are intact, got %d", len(f.pages)-len(bad), len(view.Lessons))
				}
			}
		})
	}
}

func jsonOf(t *testing.T, v any) string {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// What the sources of the pages of a search find is what each page finds alone: for a seeded mix of damage to the sources (a record
// removed or changed, a long text removed or changed, the same source cited by several articles), the issues of the search and the
// articles it lists are those of checking each page for itself, with nothing shared. The counts of what the draw produced are checked,
// so that an agreement of two empty answers cannot pass.
func TestSourcesCheckedTogetherFindWhatEachPageFindsAlone(t *testing.T) {
	rng := rand.New(rand.NewSource(20261123))
	damaged := map[string]int{}
	for round := 0; round < 12; round++ {
		f := newSourceReadsFixture(t)
		for _, id := range f.records {
			if rng.Intn(3) != 0 {
				continue
			}
			path := filepath.Join(f.dir, id+".json")
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if rng.Intn(2) == 0 {
				damaged["record removed"]++
				err = os.Remove(path)
			} else {
				damaged["record changed"]++
				err = os.WriteFile(path, append(append([]byte{}, data...), ' '), 0o600)
			}
			if err != nil {
				t.Fatal(err)
			}
		}
		if rng.Intn(2) == 0 {
			path := filepath.Join(f.dir, f.conversation+".text.json")
			var err error
			if rng.Intn(2) == 0 {
				damaged["long text removed"]++
				err = os.Remove(path)
			} else {
				damaged["long text changed"]++
				err = os.WriteFile(path, []byte(`[]`), 0o600)
			}
			if err != nil {
				t.Fatal(err)
			}
		}
		alone := Book{Root: f.book.Root, Scope: f.book.Scope}
		wantIssues, wantLive := []Issue{}, []string{}
		for _, p := range f.pages { // each page for itself: a check that shares nothing with another page
			if issues := alone.sourceIssues(p); len(issues) > 0 {
				wantIssues = append(wantIssues, issues...)
			} else {
				wantLive = append(wantLive, p.ID)
			}
		}
		view, err := f.book.QueryPage("", "")
		if err != nil {
			t.Fatal(err)
		}
		gotLive := []string{}
		for _, p := range view.Lessons {
			gotLive = append(gotLive, p.ID)
		}
		if got, want := jsonOf(t, view.Issues), jsonOf(t, wantIssues); got != want {
			t.Fatalf("round %d: the issues of the search are those of checking each page alone\n got %s\nwant %s", round, got, want)
		}
		if !slices.Equal(gotLive, wantLive) {
			t.Fatalf("round %d: the search lists the pages whose sources are intact: %v, want %v", round, gotLive, wantLive)
		}
		live, err := f.book.Live()
		if err != nil || len(live) != len(wantLive) {
			t.Fatalf("round %d: Live agrees: %d %v, want %d", round, len(live), err, len(wantLive))
		}
		if len(wantIssues) > 0 {
			damaged["rounds with an issue"]++
		}
	}
	for _, kind := range []string{"record removed", "record changed", "long text removed", "long text changed", "rounds with an issue"} {
		if damaged[kind] < 2 {
			t.Fatalf("the draw has each kind of damage: %v", damaged)
		}
	}
}
