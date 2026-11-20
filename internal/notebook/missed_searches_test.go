package notebook

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

type diskMiss struct {
	Query string    `json:"query"`
	At    time.Time `json:"at"`
}

// journalPath is where the search journal of the book is; the directory may not exist yet.
func journalPath(t *testing.T, b Book) string {
	t.Helper()
	dir, err := b.directory(true, "searches")
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(dir, ".searches.json")
}

// missesOnDisk reads the misses from the journal file as plain JSON, so that the tests do not depend on the reader under test.
func missesOnDisk(t *testing.T, b Book) []diskMiss {
	t.Helper()
	data, err := os.ReadFile(journalPath(t, b))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var file struct {
		Misses []diskMiss `json:"misses"`
	}
	if err := json.Unmarshal(data, &file); err != nil {
		t.Fatal(err)
	}
	return file.Misses
}

func diskQueries(misses []diskMiss) []string {
	out := []string{}
	for _, m := range misses {
		out = append(out, m.Query)
	}
	return out
}

func openQueries(misses []Miss) []string {
	out := []string{}
	for _, m := range misses {
		out = append(out, m.Query)
	}
	return out
}

// missFixture is a project with one article about Quartz exports.
func missFixture(t *testing.T) Book {
	t.Helper()
	b := Book{Root: t.TempDir(), Scope: "/work/missed-searches"}
	addQuartzArticle(t, b)
	return b
}

func addQuartzArticle(t *testing.T, b Book) {
	t.Helper()
	o, err := b.capture(Observation{Kind: "test", Title: "Quartz source", Content: "The raw parser kept every Quartz export row.", Reference: "synthetic", Version: "1", ObservedAt: time.Unix(1, 0)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.Ingest(Draft{Title: "Quartz export workflow", Lesson: "Check Quartz exports with the raw parser.", AppliesTo: "Quartz exports", Kind: "howto", Scene: "checking an export", Sources: []string{o.ID}}); err != nil {
		t.Fatal(err)
	}
}

// emptyView is what a word search that found nothing returns.
func emptyView() View {
	return View{Lessons: []Page{}, Search: &SearchResult{Candidates: []SearchCandidate{}}}
}

// A search that found no candidate is kept (words normalized); a search that found one is not. The same real page of a missing search
// and of a found search show that the page, not the words, decides.
func TestRecordMissKeepsASearchThatFoundNothing(t *testing.T) {
	b := missFixture(t)
	for _, c := range []struct{ name, query string }{{"found", "quartz exports"}, {"missing", "saffron lanterns"}} {
		view, err := b.QueryPage(c.query, "")
		if err != nil {
			t.Fatal(err)
		}
		if (c.name == "found") != (view.Search.Total > 0) {
			t.Fatalf("the fixture changed: %s search has %d candidates", c.name, view.Search.Total)
		}
		if err := b.RecordMiss(c.query, "", view); err != nil {
			t.Fatal(err)
		}
	}
	got := missesOnDisk(t, b)
	if len(got) != 1 || got[0].Query != "saffron lanterns" || got[0].At.IsZero() {
		t.Fatalf("only the search without a candidate is kept: %+v", got)
	}
}

// What a miss is kept for is a list of allowed forms; every other form is refused by the form alone, with the same empty page.
func TestRecordMissAllowsOnlyWordSearchesOfTheListedForm(t *testing.T) {
	found := View{Lessons: []Page{}, Search: &SearchResult{Total: 1, Candidates: []SearchCandidate{{}}}}
	returned := View{Lessons: []Page{{ID: "0123456789abcdef"}}, Search: &SearchResult{Candidates: []SearchCandidate{}}}
	for _, c := range []struct {
		name   string
		query  string
		cursor string
		view   View
		want   []string // the words kept, or nil when nothing is
	}{
		{"words", "saffron lanterns", "", emptyView(), []string{"saffron lanterns"}},
		{"white space is trimmed and collapsed", "  saffron \t  lanterns \n", "", emptyView(), []string{"saffron lanterns"}},
		{"200 bytes", strings.Repeat("x", 200), "", emptyView(), []string{strings.Repeat("x", 200)}},
		{"201 bytes", strings.Repeat("x", 201), "", emptyView(), nil},
		{"empty", "", "", emptyView(), nil},
		{"blank", " \n\t ", "", emptyView(), nil},
		{"lesson id", "lesson:0123456789abcdef", "", emptyView(), nil},
		{"lesson revision", "lesson:0123456789abcdef@fedcba9876543210", "", emptyView(), nil},
		{"lesson id after white space", "  lesson:0123456789abcdef", "", emptyView(), nil},
		{"source id", "0123456789abcdef", "", emptyView(), nil},
		{"source id after white space", " 0123456789abcdef\n", "", emptyView(), nil},
		{"continuation of a search", "saffron lanterns", "bmV4dA", emptyView(), nil},
		{"search that found a candidate", "saffron lanterns", "", found, nil},
		{"search that returned an article", "saffron lanterns", "", returned, nil},
		{"page without a search result", "saffron lanterns", "", View{Lessons: []Page{}}, nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			b := Book{Root: t.TempDir(), Scope: "/work/missed-searches"}
			if err := b.RecordMiss(c.query, c.cursor, c.view); err != nil {
				t.Fatal(err)
			}
			if got := diskQueries(missesOnDisk(t, b)); !slices.Equal(got, append([]string{}, c.want...)) {
				t.Fatalf("kept %q, want %q", got, c.want)
			}
		})
	}
}

// The same words count once, whatever the spacing, with the newer time; the journal keeps 64 and drops the oldest first.
func TestRecordMissMergesTheSameWordsAndKeepsSixtyFour(t *testing.T) {
	b := Book{Root: t.TempDir(), Scope: "/work/missed-searches"}
	base := time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC)
	record := func(query string, minutes int) {
		t.Helper()
		if err := b.recordMissAt(query, "", emptyView(), base.Add(time.Duration(minutes)*time.Minute)); err != nil {
			t.Fatal(err)
		}
	}
	record("saffron lanterns", 0)
	record("other words", 1)
	record("  saffron \t lanterns ", 2)
	got := missesOnDisk(t, b)
	if len(got) != 2 || got[0].Query != "other words" || got[1].Query != "saffron lanterns" || !got[1].At.Equal(base.Add(2*time.Minute)) {
		t.Fatalf("the same words are one entry with the newer time, listed last: %+v", got)
	}
	b = Book{Root: t.TempDir(), Scope: "/work/missed-searches"}
	for i := 0; i < 64; i++ {
		record(fmt.Sprintf("topic %02d", i), i)
	}
	if got := missesOnDisk(t, b); len(got) != 64 || got[0].Query != "topic 00" || got[63].Query != "topic 63" {
		t.Fatalf("64 misses are all kept: %d, first %q", len(got), got[0].Query)
	}
	record("topic 64", 64)
	got = missesOnDisk(t, b)
	if len(got) != 64 || got[0].Query != "topic 01" || got[63].Query != "topic 64" {
		t.Fatalf("the 65th drops the oldest: %d, first %q, last %q", len(got), got[0].Query, got[len(got)-1].Query)
	}
}

// A miss closes when an article answers the same words: it is no longer open as soon as the article exists, with no write, and a
// lookup that returns an article removes it from the file under the same lock.
func TestMissClosesOnceAnArticleAnswersTheSameWords(t *testing.T) {
	b := Book{Root: t.TempDir(), Scope: "/work/missed-searches"}
	for _, query := range []string{"quartz exports", "saffron lanterns"} {
		view, err := b.QueryPage(query, "")
		if err != nil {
			t.Fatal(err)
		}
		if err := b.RecordMiss(query, "", view); err != nil {
			t.Fatal(err)
		}
	}
	open, err := b.OpenMisses(time.Time{}, 8)
	if err != nil || !slices.Equal(openQueries(open), []string{"saffron lanterns", "quartz exports"}) {
		t.Fatalf("both are open, newest first: %+v %v", open, err)
	}
	addQuartzArticle(t, b)
	before, err := os.ReadFile(journalPath(t, b))
	if err != nil {
		t.Fatal(err)
	}
	open, err = b.OpenMisses(time.Time{}, 8)
	if err != nil || !slices.Equal(openQueries(open), []string{"saffron lanterns"}) {
		t.Fatalf("the answered words are no longer open: %+v %v", open, err)
	}
	if after, _ := os.ReadFile(journalPath(t, b)); string(after) != string(before) {
		t.Fatalf("reading the open misses wrote the journal")
	}
	view, err := b.QueryPage("quartz exports", "")
	if err != nil || len(view.Lessons) == 0 {
		t.Fatalf("the article is found: %+v %v", view.Lessons, err)
	}
	if _, err := b.RecordLookup("  quartz   exports ", view.Lessons); err != nil {
		t.Fatal(err)
	}
	if got := diskQueries(missesOnDisk(t, b)); !slices.Equal(got, []string{"saffron lanterns"}) {
		t.Fatalf("a lookup that returned an article removes its miss and no other: %q", got)
	}
}

// Open misses are listed newest first, from the time asked for (that moment included), at most as many as asked for.
func TestOpenMissesAreNewestFirstSinceTheTimeAndLimited(t *testing.T) {
	b := Book{Root: t.TempDir(), Scope: "/work/missed-searches"}
	base := time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC)
	for i := 0; i < 5; i++ {
		if err := b.recordMissAt(fmt.Sprintf("topic %d", i), "", emptyView(), base.Add(time.Duration(i)*time.Hour)); err != nil {
			t.Fatal(err)
		}
	}
	for _, c := range []struct {
		name  string
		since time.Time
		limit int
		want  []string
	}{
		{"all", time.Time{}, 8, []string{"topic 4", "topic 3", "topic 2", "topic 1", "topic 0"}},
		{"since includes its own moment", base.Add(2 * time.Hour), 8, []string{"topic 4", "topic 3", "topic 2"}},
		{"since after every miss", base.Add(4*time.Hour + time.Nanosecond), 8, []string{}},
		{"limit keeps the newest", time.Time{}, 2, []string{"topic 4", "topic 3"}},
		{"limit and since", base.Add(3 * time.Hour), 1, []string{"topic 4"}},
		{"no limit", time.Time{}, 0, []string{}},
	} {
		open, err := b.OpenMisses(c.since, c.limit)
		if err != nil || !slices.Equal(openQueries(open), c.want) {
			t.Errorf("%s: got %q (%v), want %q", c.name, openQueries(open), err, c.want)
		}
	}
}

// A call examines at most 16 misses, the newest: each is a whole search of the wiki. The 4 oldest misses here are open (no article
// answers them); the newest ones are answered by the Quartz article. With 16 answered misses newer than the open ones, none of the
// 4 is reached; with 15, exactly the newest open one is the 16th miss examined; with none, all 4 are listed.
func TestOpenMissesExamineAtMostTheSixteenNewest(t *testing.T) {
	open := []string{"saffron lanterns", "kestrel harbor", "juniper meadow", "marigold canal"} // oldest first
	for _, c := range []struct {
		name     string
		answered int
		want     []string
	}{
		{"no answered miss", 0, []string{"marigold canal", "juniper meadow", "kestrel harbor", "saffron lanterns"}},
		{"15 answered misses are newer", 15, []string{"marigold canal"}},
		{"16 answered misses are newer", 16, []string{}},
		{"20 answered misses are newer", 20, []string{}},
	} {
		t.Run(c.name, func(t *testing.T) {
			b := missFixture(t)
			base := time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC)
			minute := 0
			record := func(query string) {
				t.Helper()
				minute++
				if err := b.recordMissAt(query, "", emptyView(), base.Add(time.Duration(minute)*time.Minute)); err != nil {
					t.Fatal(err)
				}
			}
			for _, query := range open {
				record(query)
			}
			for i := 0; i < c.answered; i++ {
				record(fmt.Sprintf("quartz exports %02d", i))
			}
			got, err := b.OpenMisses(time.Time{}, 8)
			if err != nil || !slices.Equal(openQueries(got), c.want) {
				t.Fatalf("the open misses among the 16 newest: %q (%v), want %q", openQueries(got), err, c.want)
			}
		})
	}
}

// Only a miss that is examined counts toward the 16: a miss before since and a line that is not a search of words are skipped
// without using one up. The file is written by hand, so that they sit among the newest lines.
func TestOpenMissesCountOnlyTheMissesTheyExamine(t *testing.T) {
	b := missFixture(t)
	line := func(query, at string) string { return fmt.Sprintf(`{"query":%q,"at":%q}`, query, at) }
	lines := []string{line("saffron lanterns", "2026-11-01T09:00:00Z"), line("kestrel harbor", "2026-11-01T09:01:00Z")} // open, oldest in the file
	for i := 0; i < 14; i++ {
		lines = append(lines, line(fmt.Sprintf("quartz exports %02d", i), "2026-11-01T09:02:00Z")) // answered
	}
	lines = append(lines, line("juniper meadow", "2026-10-01T09:00:00Z"), line("marigold canal", "2026-10-01T09:00:00Z")) // open, before since
	lines = append(lines, line("lesson:0123456789abcdef", "2026-11-01T09:03:00Z"), line("0123456789abcdef", "2026-11-01T09:03:00Z"))
	if err := os.WriteFile(journalPath(t, b), []byte(`{"misses":[`+strings.Join(lines, ",")+`]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	since := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	got, err := b.OpenMisses(since, 8)
	if want := []string{"kestrel harbor", "saffron lanterns"}; err != nil || !slices.Equal(openQueries(got), want) {
		t.Fatalf("the 14 answered misses and the 2 open ones are the 16 examined: %q (%v), want %q", openQueries(got), err, want)
	}
}

// The searches of one call share the parsing of the articles: asking about many answered misses parses the one article once. The
// count of parses is the measure, so this does not depend on how fast a machine is.
func TestOpenMissesShareTheReadingOfTheArticles(t *testing.T) {
	b := missFixture(t)
	base := time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC)
	for i := 0; i < 8; i++ {
		if err := b.recordMissAt(fmt.Sprintf("quartz exports %02d", i), "", emptyView(), base.Add(time.Duration(i)*time.Minute)); err != nil {
			t.Fatal(err)
		}
	}
	before := PageParses()
	got, err := b.OpenMisses(time.Time{}, 8)
	if err != nil || len(got) != 0 {
		t.Fatalf("all 8 are answered: %+v (%v)", got, err)
	}
	if parses := PageParses() - before; parses != 1 {
		t.Fatalf("8 searches over one article parse it once, got %d passes", parses)
	}
}

// A line of the journal that is not a search of words (a hand-written one) is not listed as open, whatever the page says of it.
func TestOpenMissesSkipWhatIsNotASearchOfWords(t *testing.T) {
	b := Book{Root: t.TempDir(), Scope: "/work/missed-searches"}
	file := `{"misses":[{"query":"lesson:0123456789abcdef","at":"2026-11-01T09:00:00Z"},{"query":"0123456789abcdef","at":"2026-11-01T09:01:00Z"},{"query":"saffron lanterns","at":"2026-11-01T09:02:00Z"}]}`
	if err := os.WriteFile(journalPath(t, b), []byte(file), 0o600); err != nil {
		t.Fatal(err)
	}
	open, err := b.OpenMisses(time.Time{}, 8)
	if err != nil || !slices.Equal(openQueries(open), []string{"saffron lanterns"}) {
		t.Fatalf("only the search of words is open: %q (%v)", openQueries(open), err)
	}
}

// The misses of the journal are read as an allow-list and never make the journal unreadable: a miss that is not in the form the
// journal writes is dropped, more than 64 are cut to the newest 64, and the other misses are read. A file from before the misses
// is read as before.
func TestSearchJournalReadsOnlyTheMissedSearchesItWouldWrite(t *testing.T) {
	b := Book{Root: t.TempDir(), Scope: "/work/missed-searches"}
	path := journalPath(t, b)
	const at = `"at":"2026-11-01T09:00:00Z"`
	miss := func(query string) string { return `{"query":"` + query + `",` + at + `}` }
	misses := func(from, to int) []string { // topic <from> .. topic <to-1>, as JSON objects
		parts := []string{}
		for i := from; i < to; i++ {
			parts = append(parts, miss(fmt.Sprintf("topic %d", i)))
		}
		return parts
	}
	topics := func(from, to int) []string {
		out := []string{}
		for i := from; i < to; i++ {
			out = append(out, fmt.Sprintf("topic %d", i))
		}
		return out
	}
	list := func(parts ...string) string {
		return `{"lookups":[],"discoveries":[],"misses":[` + strings.Join(parts, ",") + `]}`
	}
	for _, c := range []struct {
		name string
		file string
		want []string // the words of the misses that are read, oldest first
	}{
		{"file from before the misses", `{"lookups":[],"discoveries":[]}`, nil},
		{"one miss", list(misses(0, 1)...), topics(0, 1)},
		{"64 misses", list(misses(0, 64)...), topics(0, 64)},
		{"65 misses keep the newest 64", list(misses(0, 65)...), topics(1, 65)},
		{"query of 200 bytes", list(miss(strings.Repeat("x", 200))), []string{strings.Repeat("x", 200)}},
		{"query of 201 bytes", list(miss("kept before"), miss(strings.Repeat("x", 201)), miss("kept after")), []string{"kept before", "kept after"}},
		{"empty query", list(miss("kept before"), miss(""), miss("kept after")), []string{"kept before", "kept after"}},
		{"no time", list(miss("kept before"), `{"query":"no time"}`, miss("kept after")), []string{"kept before", "kept after"}},
		{"zero time", list(miss("kept before"), `{"query":"zero time","at":"0001-01-01T00:00:00Z"}`, miss("kept after")), []string{"kept before", "kept after"}},
		{"query that is not UTF-8", list(miss("kept before"), "{\"query\":\"bad \xff text\","+at+"}", miss("kept after")), []string{"kept before", "kept after"}},
		{"time that is no time", list(miss("kept before"), `{"query":"bad time","at":"yesterday"}`, miss("kept after")), []string{"kept before", "kept after"}},
		{"query that is no text", list(miss("kept before"), `{"query":5,`+at+`}`, miss("kept after")), []string{"kept before", "kept after"}},
		{"entries that are no objects", list(miss("kept before"), `"a string"`, `7`, `null`, `[]`, miss("kept after")), []string{"kept before", "kept after"}},
		{"damaged ones are dropped before the newest 64 are kept", list(append(append([]string{miss("")}, misses(0, 66)...), miss(strings.Repeat("x", 201)))...), topics(2, 66)},
		{"misses that are no list", `{"lookups":[],"discoveries":[],"misses":{"query":"topic","at":"2026-11-01T09:00:00Z"}}`, nil},
		{"misses that are null", `{"lookups":[],"discoveries":[],"misses":null}`, nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			if err := os.WriteFile(path, []byte(c.file), 0o600); err != nil {
				t.Fatal(err)
			}
			j, err := readSearchJournal(filepath.Dir(path))
			if err != nil {
				t.Fatalf("the journal is read: %v", err)
			}
			got := []string{}
			for _, m := range j.Misses {
				got = append(got, m.Query)
			}
			if want := append([]string{}, c.want...); !slices.Equal(got, want) {
				t.Fatalf("the misses read are %q, want %q", got, want)
			}
			if _, err := b.OpenMisses(time.Time{}, 8); err != nil {
				t.Fatalf("the listing of open misses: %v", err)
			}
		})
	}
	// the other collections of the journal are checked as before: only the misses are read leniently
	file := `{"lookups":[{"id":"0123456789abcdef","query":"x"}],"misses":[` + miss("topic") + `]}`
	if err := os.WriteFile(path, []byte(file), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readSearchJournal(filepath.Dir(path)); err == nil {
		t.Fatal("a lookup whose id is not its hash is still refused")
	}
}

// A journal with damaged misses written by hand (or by another version of the program) does not stop the wiki: a search of words,
// the record of a lookup, the maintenance listing and the record of a miss all succeed, and what is kept of the misses is the
// valid ones.
func TestDamagedMissedSearchesDoNotStopTheWiki(t *testing.T) {
	b := missFixture(t)
	damaged := `{"lookups":[],"discoveries":[],"misses":[` +
		`{"query":"","at":"2026-11-01T09:00:00Z"},` +
		`{"query":"older words","at":"2026-11-01T09:01:00Z"},` +
		`{"query":"` + strings.Repeat("x", 201) + `","at":"2026-11-01T09:02:00Z"},` +
		`{"query":"no time"},` +
		`{"query":"newer words","at":"2026-11-01T09:03:00Z"}]}`
	if err := os.WriteFile(journalPath(t, b), []byte(damaged), 0o600); err != nil {
		t.Fatal(err)
	}
	view, err := b.QueryPage("quartz exports", "")
	if err != nil || len(view.Lessons) == 0 {
		t.Fatalf("a search of words succeeds: %v", err)
	}
	if _, err := b.Maintenance(); err != nil {
		t.Fatalf("the maintenance listing succeeds: %v", err)
	}
	if _, err := b.RecordLookup("quartz exports", view.Lessons); err != nil {
		t.Fatalf("the record of a lookup succeeds: %v", err)
	}
	if got := diskQueries(missesOnDisk(t, b)); !slices.Equal(got, []string{"older words", "newer words"}) {
		t.Fatalf("what the write keeps of the misses is the valid ones: %q", got)
	}
	empty := emptyView()
	if err := b.RecordMiss("saffron lanterns", "", empty); err != nil {
		t.Fatalf("the record of a miss succeeds: %v", err)
	}
	if got := diskQueries(missesOnDisk(t, b)); !slices.Equal(got, []string{"older words", "newer words", "saffron lanterns"}) {
		t.Fatalf("the new miss follows the valid ones: %q", got)
	}
}

// Over the 1 MiB bound, the misses go first (oldest first), before another collection loses a record.
func TestSearchJournalTrimsMissedSearchesBeforeTheOtherRecords(t *testing.T) {
	b := Book{Root: t.TempDir(), Scope: "/work/missed-searches"}
	dir := filepath.Dir(journalPath(t, b))
	// "<" is written as six bytes in JSON, so 64 lookups of 4000 of them are over 1 MiB on their own.
	j := searchJournal{}
	for i := 0; i < 64; i++ {
		j.Lookups = append(j.Lookups, Lookup{ID: fmt.Sprintf("%016x", i), Query: strings.Repeat("<", 4000)})
		j.Misses = append(j.Misses, Miss{Query: fmt.Sprintf("topic %d", i), At: time.Date(2026, 11, 1, 9, i, 0, 0, time.UTC)})
	}
	if err := saveSearchJournal(dir, j); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, ".searches.json"))
	if err != nil || len(data) > 1<<20 {
		t.Fatalf("the journal is within the bound: %d bytes, %v", len(data), err)
	}
	var kept struct {
		Lookups []Lookup `json:"lookups"`
		Misses  []Miss   `json:"misses"`
	}
	if err := json.Unmarshal(data, &kept); err != nil {
		t.Fatal(err)
	}
	if len(kept.Misses) != 0 || len(kept.Lookups) == 0 || len(kept.Lookups) == 64 {
		t.Fatalf("the misses went first, then the lookups were trimmed: %d misses, %d lookups", len(kept.Misses), len(kept.Lookups))
	}
}

// The searches the wiki makes for itself never leave a miss: only the public entrances call RecordMiss.
func TestInternalSearchesLeaveNoMiss(t *testing.T) {
	b := missFixture(t)
	if _, err := b.Query("saffron lanterns"); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Recall("saffron lanterns kestrel harbor"); err != nil {
		t.Fatal(err)
	}
	if _, err := b.OpenMisses(time.Time{}, 8); err != nil {
		t.Fatal(err)
	}
	if got := missesOnDisk(t, b); len(got) != 0 {
		t.Fatalf("an internal search left a miss: %+v", got)
	}
}
