package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/notebook"
)

// missedFlow is a project with one article about export audits, so that "export audit" finds a candidate and every other topic
// below finds none.
type missedFlow struct {
	t      *testing.T
	b      notebook.Book
	source string
}

func newMissedFlow(t *testing.T) missedFlow {
	t.Helper()
	b, source := loopBook(t)
	loopPage(t, b, source, map[string]any{"title": "Export audit workflow", "lesson": "Audit the export filters and totals.", "applies_to": "export audit"})
	return missedFlow{t: t, b: b, source: source}
}

// wiki is the public word search of the wiki (shikii wiki query).
func (f missedFlow) wiki(query string) map[string]any {
	f.t.Helper()
	value, err := runNotebook(f.b.Root, f.b.Scope, notebookRequest{Operation: "query", Query: query})
	if err != nil {
		f.t.Fatal(err)
	}
	return loopObject(f.t, value)
}

// prepare starts a work of its own (no hook ids); with a receipt it extends that work with a search.
func (f missedFlow) prepare(query, receipt string) map[string]any {
	f.t.Helper()
	req := workRequest{Query: query, Request: "Audit the export."}
	if receipt != "" {
		req = workRequest{Query: query, Receipt: receipt}
	}
	value, err := runWork(f.b.Root, f.b.Scope, "prepare", req)
	if err != nil {
		f.t.Fatal(err)
	}
	return loopObject(f.t, value)
}

// record sends a result that cites an observation of its call as new1; without result it sends the observation alone.
func (f missedFlow) record(prepared map[string]any, result *workCompletion) map[string]any {
	f.t.Helper()
	value, err := runWork(f.b.Root, f.b.Scope, "record", workRequest{Receipt: prepared["receipt"].(string), Result: result, Observations: []workObservation{{Kind: "test", Title: "Export check", Content: "The synthetic export kept its filters."}}})
	if err != nil {
		f.t.Fatal(err)
	}
	return loopObject(f.t, value)
}

func goodResult() *workCompletion {
	return &workCompletion{Changed: "Audited the export.", Checked: "Read the export code.", Evidence: []string{"new1"}}
}

func (f missedFlow) journalPath() string {
	f.t.Helper()
	paths, err := filepath.Glob(filepath.Join(f.b.Root, "records", "wiki-searches", "*", ".searches.json"))
	if err != nil || len(paths) != 1 {
		f.t.Fatalf("the project has one search journal: %v %v", paths, err)
	}
	return paths[0]
}

// journalMisses is the words of the misses in the journal file, read as plain JSON; none when the journal does not exist.
func (f missedFlow) journalMisses() []string {
	f.t.Helper()
	paths, _ := filepath.Glob(filepath.Join(f.b.Root, "records", "wiki-searches", "*", ".searches.json"))
	if len(paths) == 0 {
		return []string{}
	}
	data, err := os.ReadFile(paths[0])
	if err != nil {
		f.t.Fatal(err)
	}
	var file struct {
		Misses []struct {
			Query string `json:"query"`
		} `json:"misses"`
	}
	if err := json.Unmarshal(data, &file); err != nil {
		f.t.Fatal(err)
	}
	words := []string{}
	for _, m := range file.Misses {
		words = append(words, m.Query)
	}
	return words
}

// addMissAt puts a miss with a time of the test's choosing into the existing journal file, as the oldest.
func (f missedFlow) addMissAt(query, at string) {
	f.t.Helper()
	path := f.journalPath()
	data, err := os.ReadFile(path)
	if err != nil {
		f.t.Fatal(err)
	}
	var file map[string]any
	if err := json.Unmarshal(data, &file); err != nil {
		f.t.Fatal(err)
	}
	old, _ := file["misses"].([]any)
	file["misses"] = append([]any{map[string]any{"query": query, "at": at}}, old...)
	data, _ = json.Marshal(file)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		f.t.Fatal(err)
	}
}

// missedQueriesOf is the words a record's reply names; the reply without the field fails the test where it is read.
func missedQueriesOf(t *testing.T, saved map[string]any) []string {
	t.Helper()
	reply, ok := saved["missed_searches"].(map[string]any)
	if !ok {
		t.Fatalf("the reply of the record has no missed_searches: %v", saved)
	}
	words := []string{}
	for _, raw := range reply["queries"].([]any) {
		words = append(words, raw.(string))
	}
	return words
}

// The two entrances where the agent asks the wiki in words keep a search that found nothing: wiki query, and the extension of a
// prepared work with a search (a prepare that carries a receipt). A search that found a candidate, and every other form of query,
// is kept by neither.
func TestMissedSearchesAreKeptWhereTheAgentAsksTheWikiInWords(t *testing.T) {
	f := newMissedFlow(t)
	f.wiki("saffron lanterns")
	receipt := f.prepare("export audit", "")["receipt"].(string)
	f.prepare("juniper meadow", receipt)
	if got := f.journalMisses(); !slices.Equal(got, []string{"saffron lanterns", "juniper meadow"}) {
		t.Fatalf("each entrance keeps its search, oldest first: %q", got)
	}
	// a search that found a candidate, at each entrance
	f.wiki("export audit")
	f.prepare("export audit", receipt)
	// the forms that are not a search of words, at each entrance
	for _, query := range []string{"lesson:0123456789abcdef", strings.Repeat("x", 201)} {
		f.wiki(query)
		f.prepare(query, receipt)
	}
	f.wiki("0123456789abcdef")
	// the memory-upgrade queries are the work's own commands: `wiki query` is the one entrance that would record them
	f.wiki("memory-upgrade")
	f.wiki("memory-optimize")
	if got := f.journalMisses(); !slices.Equal(got, []string{"saffron lanterns", "juniper meadow"}) {
		t.Fatalf("only the two searches without a candidate are kept: %q", got)
	}
}

// The query of a standalone preparation is the topic of the task, not a question the agent asked the wiki: a young wiki finds
// nothing for most of them, so none is kept as a miss, and the record of that work has nothing to say of it. The same words asked
// with `wiki query` are a miss, so the entrance alone makes the difference.
func TestStandalonePreparationLeavesNoMissedSearch(t *testing.T) {
	f := newMissedFlow(t)
	prepared := f.prepare("kestrel harbor", "")
	if got := f.journalMisses(); len(got) != 0 {
		t.Fatalf("a standalone preparation whose query finds nothing left a miss: %q", got)
	}
	if saved := f.record(prepared, goodResult()); saved["complete"] != true || saved["missed_searches"] != nil {
		t.Fatalf("the record of that work names no missed search: %v", saved)
	}
	f.wiki("kestrel harbor")
	if got := f.journalMisses(); !slices.Equal(got, []string{"kestrel harbor"}) {
		t.Fatalf("the same words asked of the wiki are a miss: %q", got)
	}
}

// The searches the wiki makes for itself (here a consultation) leave no miss.
func TestConsultationLeavesNoMissedSearch(t *testing.T) {
	f := newMissedFlow(t)
	f.wiki("export audit") // creates the search journal
	if _, err := runNotebook(f.b.Root, f.b.Scope, notebookRequest{Operation: "consult", Request: "saffron lanterns", Proposal: "Add a lantern form"}); err != nil {
		t.Fatal(err)
	}
	if got := f.journalMisses(); len(got) != 0 {
		t.Fatalf("a consultation left a miss: %q", got)
	}
}

// A record that carries a result names the misses made since its work was prepared: one from before the preparation is left out
// (fewer than three come after it, so the limit cannot be what hides it).
func TestRecordReplyNamesOnlyTheMissesSincePreparation(t *testing.T) {
	f := newMissedFlow(t)
	prepared := f.prepare("export audit", "")
	f.addMissAt("topic before the work", "2020-01-01T00:00:00Z")
	f.wiki("alpha bravo")
	f.wiki("charlie delta")
	saved := f.record(prepared, goodResult())
	if saved["complete"] != true {
		t.Fatalf("the record is complete: %v", saved)
	}
	if got := missedQueriesOf(t, saved); !slices.Equal(got, []string{"charlie delta", "alpha bravo"}) {
		t.Fatalf("the two misses since the preparation, newest first, and not the older one: %q", got)
	}
}

// A record names at most three open misses, newest first; one an article now answers is not open, so it is not named.
func TestRecordReplyNamesAtMostThreeOpenMisses(t *testing.T) {
	f := newMissedFlow(t)
	prepared := f.prepare("export audit", "")
	for _, query := range []string{"alpha bravo", "charlie delta", "echo foxtrot", "golf hotel", "india juliet"} {
		f.wiki(query)
	}
	loopPage(t, f.b, f.source, map[string]any{"title": "India juliet notes", "lesson": "Keep india juliet in view.", "applies_to": "india juliet"})
	saved := f.record(prepared, goodResult())
	if saved["complete"] != true {
		t.Fatalf("the record is complete: %v", saved)
	}
	if got := missedQueriesOf(t, saved); !slices.Equal(got, []string{"golf hotel", "echo foxtrot", "charlie delta"}) {
		t.Fatalf("the three newest of the four open misses, without the answered one: %q", got)
	}
	reply := saved["missed_searches"].(map[string]any)
	next, _ := reply["next_action"].(string)
	for _, part := range []string{
		"found no article while this work was open",
		"found the answer to one of them with evidence",
		"the next search of those words will miss again",
		"add a short note (the answer, or where it is written) in a work record with the same receipt",
		"If it did not, leave it as it is",
		"not an instruction to search again",
	} {
		if !strings.Contains(next, part) {
			t.Fatalf("the note says %q: %q", part, next)
		}
	}
	if len(reply) != 2 {
		t.Fatalf("the reply holds the queries and the note only: %v", reply)
	}
}

// Without an open miss since the preparation, or without a complete record that carries a result, the reply has no such field.
func TestRecordReplyHasNoMissedSearchesUnlessItIsDue(t *testing.T) {
	f := newMissedFlow(t)
	if saved := f.record(f.prepare("export audit", ""), goodResult()); saved["complete"] != true || saved["missed_searches"] != nil {
		t.Fatalf("no search found nothing, yet the reply has missed_searches: %v", saved)
	}
	f.addMissAt("topic before the work", "2020-01-01T00:00:00Z")
	if saved := f.record(f.prepare("export audit", ""), goodResult()); saved["complete"] != true || saved["missed_searches"] != nil {
		t.Fatalf("only a miss from before the preparation exists, yet the reply has missed_searches: %v", saved)
	}
	// a record without a result, and then the record with one, of the same work
	prepared := f.prepare("export audit", "")
	f.wiki("alpha bravo")
	if saved := f.record(prepared, nil); saved["complete"] != true || saved["missed_searches"] != nil {
		t.Fatalf("a record without a result carries no missed_searches: %v", saved)
	}
	if saved := f.record(prepared, goodResult()); saved["complete"] != true || !slices.Equal(missedQueriesOf(t, saved), []string{"alpha bravo"}) {
		t.Fatalf("the complete record with a result names the open miss: %v", saved)
	}
	// a record that is not complete: its result cites evidence the call does not have
	prepared = f.prepare("export audit", "")
	f.wiki("charlie delta")
	bad := goodResult()
	bad.Evidence = []string{"new2"}
	value, err := runWork(f.b.Root, f.b.Scope, "record", workRequest{Receipt: prepared["receipt"].(string), Result: bad})
	if err != nil {
		t.Fatal(err)
	}
	if saved := loopObject(t, value); saved["complete"] != false || saved["missed_searches"] != nil {
		t.Fatalf("an incomplete record carries no missed_searches: complete=%v %v", saved["complete"], saved["missed_searches"])
	}
}

// The misses are an addition to the maintenance listing: a search journal the reader refuses (here a lookup whose id is not the
// hash of its content, written by hand; a damaged miss no longer refuses the journal) lists none, and the addition never fails the
// listing it is added to. The listing reads the same journal for its own purposes (Maintenance) and answers an unreadable journal
// with its own refusal before the addition is reached, so the addition is pinned where it is made.
func TestListedMissedSearchesAreBestEffort(t *testing.T) {
	f := newMissedFlow(t)
	f.wiki("alpha lanterns") // creates the journal
	if got := listedMissedSearches(f.b); len(got) != 1 || got[0].Query != "alpha lanterns" {
		t.Fatalf("control: a readable journal lists its open miss: %+v", got)
	}
	if err := os.WriteFile(f.journalPath(), []byte(`{"lookups":[{"id":"0123456789abcdef","query":"alpha lanterns"}],"misses":[{"query":"alpha lanterns","at":"2026-11-01T09:00:00Z"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := f.b.OpenMisses(time.Time{}, 8); err == nil {
		t.Fatal("control: the journal is one the reader refuses")
	}
	if got := listedMissedSearches(f.b); len(got) != 0 {
		t.Fatalf("a journal that cannot be read lists no misses: %+v", got)
	}
}

// The wordless query (the maintenance listing) lists the open misses, newest first, at most eight, with their words and times. A
// query with words does not carry them.
func TestWordlessQueryListsAtMostEightOpenMisses(t *testing.T) {
	f := newMissedFlow(t)
	if listing := f.wiki(""); listing["missed_searches"] != nil {
		t.Fatalf("a project without a miss lists none: %v", listing["missed_searches"])
	}
	words := []string{"alpha", "bravo", "charlie", "delta", "echo", "foxtrot", "golf", "hotel", "india", "juliet"}
	for _, query := range words {
		f.wiki(query + " lanterns")
	}
	listing := f.wiki("")
	rows, _ := listing["missed_searches"].([]any)
	got := []string{}
	for _, raw := range rows {
		row := raw.(map[string]any)
		if at, _ := row["at"].(string); at == "" || len(row) != 2 {
			t.Fatalf("a listed miss has its words and its time: %v", row)
		}
		got = append(got, row["query"].(string))
	}
	if want := []string{"juliet lanterns", "india lanterns", "hotel lanterns", "golf lanterns", "foxtrot lanterns", "echo lanterns", "delta lanterns", "charlie lanterns"}; !slices.Equal(got, want) {
		t.Fatalf("the eight newest open misses: %q", got)
	}
	if searched := f.wiki("alpha lanterns"); searched["missed_searches"] != nil {
		t.Fatalf("a query with words does not list the misses: %v", searched["missed_searches"])
	}
}
