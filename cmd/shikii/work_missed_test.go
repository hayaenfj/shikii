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

// goodResultAbout is goodResult whose report also says that it searched the wiki for the words: a result is tied to the misses of the
// words its text speaks of (notebook.Book.NoteMissResult), so a test that follows a tie records a result of this kind.
func goodResultAbout(words ...string) *workCompletion {
	c := goodResult()
	c.Checked += " Searched the wiki for " + strings.Join(words, " and ") + "."
	return c
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

// journalMiss is the time and the result tied to the miss of the words, as the journal file holds them (plain JSON); found is false
// when the journal has no miss of those words.
func (f missedFlow) journalMiss(query string) (at, result string, found bool) {
	f.t.Helper()
	data, err := os.ReadFile(f.journalPath())
	if err != nil {
		f.t.Fatal(err)
	}
	var file struct {
		Misses []struct{ Query, At, Result string } `json:"misses"`
	}
	if err := json.Unmarshal(data, &file); err != nil {
		f.t.Fatal(err)
	}
	for _, m := range file.Misses {
		if m.Query == query {
			return m.At, m.Result, true
		}
	}
	return "", "", false
}

// resultID is the id of the saved result a record's reply names, where both places that name it agree.
func resultID(t *testing.T, saved map[string]any) string {
	t.Helper()
	storage, _ := saved["storage"].(map[string]any)
	id, _ := storage["result_source"].(string)
	for _, raw := range saved["saved"].([]any) {
		if item := raw.(map[string]any); item["kind"] == "result" && item["id"] != id {
			t.Fatalf("the record names two results: %v and %v", item["id"], id)
		}
	}
	if id == "" {
		t.Fatalf("the record saved no result: %v", saved)
	}
	return id
}

// earlierSearchOf is the earlier_search of a reply; a reply without it fails the test where it is read.
func earlierSearchOf(t *testing.T, reply map[string]any) map[string]any {
	t.Helper()
	earlier, ok := reply["earlier_search"].(map[string]any)
	if !ok {
		t.Fatalf("the reply has no earlier_search: %v", reply)
	}
	return earlier
}

// savedResultText reads the saved result by its id as an agent does over MCP: a query whose text is the id, and returns the reply as
// text.
func (f missedFlow) savedResultText(id string) string {
	f.t.Helper()
	value, err := runNotebook(f.b.Root, f.b.Scope, notebookRequest{Operation: "query", Query: id})
	if err != nil {
		f.t.Fatal(err)
	}
	return jsonText(value)
}

// addMissAt puts a miss with a time of the test's choosing into the existing journal file, as the oldest; the result is the id it is
// tied to, when one is given.
func (f missedFlow) addMissAt(query, at string, result ...string) {
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
	miss := map[string]any{"query": query, "at": at}
	if len(result) > 0 {
		miss["result"] = result[0]
	}
	file["misses"] = append([]any{miss}, old...)
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
	f.wiki(f.source) // a saved source id is read, not searched: an id that names no source is refused (TestQueryThatIsASourceIDReadsTheSource)
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
	if _, err := f.b.OpenMisses(time.Time{}, 8, ""); err == nil {
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

// A record ties to the saved result of its work the open misses made since its preparation that its result speaks of, the ones the
// reply left out for the limit of three included (the fourth, alpha bravo): not a miss from before its preparation. The id it ties
// is the result's own, and a query of it returns the saved result.
func TestRecordTiesTheMissesItNamesToTheSavedResultOfItsWork(t *testing.T) {
	f := newMissedFlow(t)
	prepared := f.prepare("export audit", "")
	f.addMissAt("topic before the work", "2020-01-01T00:00:00Z")
	for _, query := range []string{"alpha bravo", "charlie delta", "echo foxtrot", "golf hotel"} {
		f.wiki(query)
	}
	saved := f.record(prepared, goodResultAbout("topic before the work", "alpha bravo", "charlie delta", "echo foxtrot", "golf hotel"))
	if got := missedQueriesOf(t, saved); !slices.Equal(got, []string{"golf hotel", "echo foxtrot", "charlie delta"}) {
		t.Fatalf("the record names the three newest misses: %q", got)
	}
	id := resultID(t, saved)
	for _, query := range []string{"golf hotel", "echo foxtrot", "charlie delta", "alpha bravo"} {
		if _, result, found := f.journalMiss(query); !found || result != id {
			t.Fatalf("%q is an open miss of this work that the result speaks of, named or not, and is tied to the saved result %s: %q %v", query, id, result, found)
		}
	}
	if _, result, found := f.journalMiss("topic before the work"); !found || result != "" {
		t.Fatalf("a miss from before the preparation is tied to nothing: %q %v", result, found)
	}
	if text := f.savedResultText(id); !strings.Contains(text, "Audited the export.") {
		t.Fatalf("a query of the id returns the saved result: %s", text)
	}
}

// Another session that searches the same words (the spacing does not matter) and finds nothing is told where the result of the
// earlier work is, by wiki query and by a prepare that carries a receipt; the note names a result a query of its id reads.
func TestTheSameWordsMissingAgainAreToldWhereTheEarlierResultIs(t *testing.T) {
	f := newMissedFlow(t)
	id := resultID(t, f.recordAMissedSearch("saffron lanterns", goodResultAbout("saffron lanterns")))
	receipt := f.prepare("export audit", "")["receipt"].(string) // another session
	at, _, _ := f.journalMiss("saffron lanterns")
	earlier := earlierSearchOf(t, f.wiki("  saffron   lanterns "))
	if len(earlier) != 4 || earlier["query"] != "saffron lanterns" || earlier["at"] != at || earlier["result"] != id {
		t.Fatalf("earlier_search names the words, the time of the earlier miss and its result: %v (time %s, result %s)", earlier, at, id)
	}
	next, _ := earlier["next_action"].(string)
	if len(next) > 350 {
		t.Fatalf("the note is about 350 bytes at most: %d", len(next))
	}
	for _, part := range []string{
		"These same words found no article before, and a work that was open then recorded a result",
		"Read that result with a query of its id",
		"what was found or where the answer is written",
		"that work's own report (it may not be the work that searched), not an article and not verified",
		"confirm it before relying on it",
	} {
		if !strings.Contains(next, part) {
			t.Fatalf("the note says %q: %q", part, next)
		}
	}
	// the same search of a prepare that carries a receipt, asked after the miss above was kept again with a newer time
	at, _, _ = f.journalMiss("saffron lanterns")
	prepared := f.prepare("saffron lanterns", receipt)
	if got := earlierSearchOf(t, prepared); len(got) != 4 || got["query"] != "saffron lanterns" || got["at"] != at || got["result"] != id || got["next_action"] != next {
		t.Fatalf("the search of a prepare is told the same: %v", got)
	}
	// the id the two replies name is read by a query of it, in a prepare that carries a receipt as well
	if source, _ := f.prepare(id, receipt)["source"].(map[string]any); source == nil || !strings.Contains(jsonText(source), "Audited the export.") {
		t.Fatalf("a prepare that queries the id returns the saved result: %v", source)
	}
	// the maintenance listing names the result of a miss that has one
	listing, _ := f.wiki("")["missed_searches"].([]any)
	if len(listing) != 1 || listing[0].(map[string]any)["result"] != id {
		t.Fatalf("the listing of open misses carries the result: %v", listing)
	}
}

// The note does not say that the work whose result it names searched the words: it names the work that was open when the words
// missed and then recorded a result. A wiki query carries no session, so when several sessions of one project run side by side the
// words it missed are tied to the result of a work that was open then, whichever searched (a search through a receipt is tied to
// its own session: TestARecordNamesAndTiesOnlyTheMissesOfItsOwnSession).
func TestEarlierSearchNoteDoesNotSayTheWorkThatSearched(t *testing.T) {
	f := newMissedFlow(t)
	f.prepare("export audit", "")
	other := f.prepare("export audit", "")
	f.wiki("saffron lanterns")                                                   // a wiki query: no one knows which of the two open works asked
	idOther := resultID(t, f.record(other, goodResultAbout("saffron lanterns"))) // the work that records first while the words miss is the one they are tied to
	earlier := earlierSearchOf(t, f.wiki("saffron lanterns"))
	next, _ := earlier["next_action"].(string)
	if earlier["result"] != idOther {
		t.Fatalf("the words are tied to the result of the work that recorded while they missed: %v (want %s)", earlier, idOther)
	}
	if strings.Contains(next, "An earlier work searched") || strings.Contains(next, "earlier work searched") || !strings.Contains(next, "it may not be the work that searched") {
		t.Fatalf("the note must not say that the work of that result searched the words: %q", next)
	}
}

// The whole path of the feature as an agent meets it over MCP: a work searched words, found nothing and recorded a result; the same
// words miss again and earlier_search names the result's id; a query whose text is exactly that id, sent as the MCP call sends it,
// returns what that work recorded (the words of its own result, not a word search that finds nothing).
func TestEarlierSearchIDIsReadByAQueryOfItsText(t *testing.T) {
	f := newMissedFlow(t)
	f.recordAMissedSearch("saffron lanterns", goodResultAbout("saffron lanterns"))
	id, _ := earlierSearchOf(t, f.wiki("saffron lanterns"))["result"].(string)
	if id == "" {
		t.Fatal("the same words missing again name the id of the earlier result")
	}
	var req notebookRequest
	if err := json.Unmarshal([]byte(`{"operation":"query","query":"`+id+`"}`), &req); err != nil {
		t.Fatal(err)
	}
	value, err := runNotebook(f.b.Root, f.b.Scope, req)
	if err != nil {
		t.Fatal(err)
	}
	text := jsonText(value)
	for _, saved := range []string{"Audited the export.", "Read the export code."} { // the changed and checked parts of goodResult
		if !strings.Contains(text, saved) {
			t.Fatalf("the query of the id returns the saved result, which holds %q: %.300s", saved, text)
		}
	}
	if object := loopObject(t, value); object["search"] != nil || object["lessons"] != nil {
		t.Fatalf("the reply is the saved result, not a word search of the id: %.300s", text)
	}
}

// recordAMissedSearch is a work that searched the words in the wiki, found nothing, and recorded the result; it returns its reply.
func (f missedFlow) recordAMissedSearch(query string, result *workCompletion) map[string]any {
	f.t.Helper()
	prepared := f.prepare("export audit", "")
	f.wiki(query)
	saved := f.record(prepared, result)
	if saved["complete"] != true || !slices.Equal(missedQueriesOf(f.t, saved), []string{query}) {
		f.t.Fatalf("the work names the miss in its complete record: %v", saved)
	}
	return saved
}

// No earlier_search when the earlier miss has no result: a work that recorded none, words searched before without any work, and
// words never missed. A prepare that carries a receipt is told no differently.
func TestNoEarlierSearchWhenTheEarlierMissHasNoResult(t *testing.T) {
	f := newMissedFlow(t)
	f.wiki("saffron lanterns")
	if reply := f.wiki("saffron lanterns"); reply["earlier_search"] != nil {
		t.Fatalf("words missed twice and tied to no work: %v", reply["earlier_search"])
	}
	// a work that recorded no result does not tie its miss
	prepared := f.prepare("export audit", "")
	f.wiki("alpha bravo")
	if saved := f.record(prepared, nil); saved["complete"] != true || saved["missed_searches"] != nil {
		t.Fatalf("a record without a result names no miss: %v", saved)
	}
	if _, result, found := f.journalMiss("alpha bravo"); !found || result != "" {
		t.Fatalf("its miss is tied to nothing: %q %v", result, found)
	}
	receipt := f.prepare("export audit", "")["receipt"].(string)
	for name, reply := range map[string]map[string]any{
		"wiki query":              f.wiki("alpha bravo"),
		"prepare with a receipt":  f.prepare("alpha bravo", receipt),
		"words never missed":      f.wiki("quince orchard"),
		"a prepare, never missed": f.prepare("walnut grove", receipt),
	} {
		if reply["earlier_search"] != nil {
			t.Fatalf("%s: %v", name, reply["earlier_search"])
		}
	}
}

// What is not a search of words is never pointed at a result, even where a journal written by hand holds a tied miss of it: a
// lesson id, a source id, and the memory-upgrade queries are the agent's reads and the work's own commands.
func TestNoEarlierSearchForAQueryThatIsNotAWordSearch(t *testing.T) {
	f := newMissedFlow(t)
	f.wiki("saffron lanterns") // creates the journal
	forms := []string{"lesson:0123456789abcdef", f.source, "memory-upgrade", "memory-optimize"}
	for _, query := range forms {
		f.addMissAt(query, "2026-11-01T09:00:00Z", "fedcba9876543210")
	}
	for _, query := range forms {
		if reply := f.wiki(query); reply["earlier_search"] != nil {
			t.Fatalf("%q is no search of words: %v", query, reply["earlier_search"])
		}
	}
	f.addMissAt("kestrel harbor", "2026-11-01T09:00:00Z", "fedcba9876543210") // control: the same journal tells a search of words
	if reply := f.wiki("kestrel harbor"); reply["earlier_search"] == nil {
		t.Fatal("control: the tied miss of words is told")
	}
}

// The result stays tied through further misses of the words (a third search is told too), and a later work that searched the
// same words and found nothing replaces it with its own result.
func TestEarlierSearchSurvivesFurtherMissesAndTheLaterWorkReplacesIt(t *testing.T) {
	f := newMissedFlow(t)
	idA := resultID(t, f.recordAMissedSearch("saffron lanterns", goodResultAbout("saffron lanterns")))
	for round := 2; round <= 3; round++ {
		if earlier := earlierSearchOf(t, f.wiki("saffron lanterns")); earlier["result"] != idA {
			t.Fatalf("miss %d of the words is told the result of the first work: %v", round, earlier)
		}
		if _, result, _ := f.journalMiss("saffron lanterns"); result != idA {
			t.Fatalf("the miss keeps its result after miss %d: %q", round, result)
		}
	}
	later := &workCompletion{Changed: "Audited the export totals.", Checked: "Read the totals code. Searched the wiki for saffron lanterns.", Evidence: []string{"new1"}}
	prepared := f.prepare("export audit", "")
	if earlier := earlierSearchOf(t, f.wiki("saffron lanterns")); earlier["result"] != idA {
		t.Fatalf("the later work is told the result of the first: %v", earlier)
	}
	idB := resultID(t, f.record(prepared, later))
	if idB == idA {
		t.Fatalf("the two works saved two results: %s", idB)
	}
	if _, result, _ := f.journalMiss("saffron lanterns"); result != idB {
		t.Fatalf("the miss is tied to the result of the later work: %q, want %s", result, idB)
	}
	if earlier := earlierSearchOf(t, f.wiki("saffron lanterns")); earlier["result"] != idB {
		t.Fatalf("a later search is told the result of the later work: %v", earlier)
	}
}

// An article that answers the words ends the note: both entrances find a candidate, return no earlier_search, and the lookup of the
// wiki query removes the miss with its result.
func TestAnArticleThatAnswersTheWordsEndsTheEarlierSearch(t *testing.T) {
	f := newMissedFlow(t)
	f.recordAMissedSearch("saffron lanterns", goodResultAbout("saffron lanterns"))
	receipt := f.prepare("export audit", "")["receipt"].(string)
	loopPage(t, f.b, f.source, map[string]any{"title": "Saffron lanterns notes", "lesson": "Light saffron lanterns after the audit.", "applies_to": "saffron lanterns"})
	prepared := f.prepare("saffron lanterns", receipt)
	if prepared["earlier_search"] != nil || len(prepared["knowledge"].([]any)) == 0 {
		t.Fatalf("the search of a prepare finds the article and has no earlier_search: %v", prepared["earlier_search"])
	}
	reply := f.wiki("saffron lanterns")
	if reply["earlier_search"] != nil || len(reply["lessons"].([]any)) == 0 {
		t.Fatalf("the wiki query finds the article and has no earlier_search: %v", reply["earlier_search"])
	}
	if _, _, found := f.journalMiss("saffron lanterns"); found {
		t.Fatal("the miss and its result are gone once the lookup returned the article")
	}
}

// Tying is an addition to the record: when the journal cannot be written (its folder is read-only) the record is still complete,
// still names the miss, and the miss stays tied to nothing.
func TestRecordStaysCompleteWhenTyingTheMissesFails(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root writes into a read-only folder")
	}
	f := newMissedFlow(t)
	prepared := f.prepare("export audit", "")
	f.wiki("saffron lanterns")
	dir := filepath.Dir(f.journalPath())
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	saved := f.record(prepared, goodResultAbout("saffron lanterns"))
	if saved["complete"] != true || !slices.Equal(missedQueriesOf(t, saved), []string{"saffron lanterns"}) {
		t.Fatalf("the record is complete and names the miss although tying it failed: %v", saved)
	}
	if _, result, found := f.journalMiss("saffron lanterns"); !found || result != "" {
		t.Fatalf("the failed write left the miss tied to nothing (the folder was read-only): %q %v", result, found)
	}
}

// Only a complete record ties: one that is not complete (its result cites evidence the call does not have) may hold a result that is
// not saved for good, so the miss stays tied to nothing.
func TestRecordThatIsNotCompleteTiesNothing(t *testing.T) {
	f := newMissedFlow(t)
	prepared := f.prepare("export audit", "")
	f.wiki("charlie delta")
	bad := goodResultAbout("charlie delta")
	bad.Evidence = []string{"new2"}
	value, err := runWork(f.b.Root, f.b.Scope, "record", workRequest{Receipt: prepared["receipt"].(string), Result: bad})
	if err != nil {
		t.Fatal(err)
	}
	if saved := loopObject(t, value); saved["complete"] != false {
		t.Fatalf("the record is not complete: %v", saved)
	}
	if _, result, found := f.journalMiss("charlie delta"); !found || result != "" {
		t.Fatalf("a record that is not complete ties nothing: %q %v", result, found)
	}
}

// The search of a prepare that carries a receipt closes the miss of the same words when it returns an article, as the lookup of a
// wiki query does: the journal does not keep a gap the wiki has answered since, so a later search that finds nothing again is not
// pointed at the result of an old miss.
func TestPrepareWithAReceiptClosesTheMissOfWordsThatNowFindAnArticle(t *testing.T) {
	f := newMissedFlow(t)
	receipt := f.prepare("export audit", "")["receipt"].(string)
	f.wiki("import retries")
	f.wiki("saffron lanterns")
	if got := f.journalMisses(); !slices.Equal(got, []string{"import retries", "saffron lanterns"}) {
		t.Fatalf("both searches missed: %q", got)
	}
	loopPage(t, f.b, f.source, map[string]any{"title": "Import retries", "lesson": "Retry a failed import three times.", "applies_to": "import retries"})
	prepared := f.prepare("  import   retries ", receipt)
	if knowledge, _ := prepared["knowledge"].([]any); len(knowledge) == 0 {
		t.Fatalf("the search of the prepare returns the article: %v", prepared["knowledge_context"])
	}
	if got := f.journalMisses(); !slices.Equal(got, []string{"saffron lanterns"}) {
		t.Fatalf("the miss of the answered words is closed, and the other one stays: %q", got)
	}
	// a search that found nothing again keeps its miss, with the result it was tied to
	f.prepare("saffron lanterns", receipt)
	if got := f.journalMisses(); !slices.Equal(got, []string{"saffron lanterns"}) {
		t.Fatalf("a search without a candidate closes nothing: %q", got)
	}
}

// A prepare whose search returns an article and whose words have no miss does not write the journal: the file is not touched.
func TestPrepareWithAReceiptLeavesTheJournalAloneWhenNoMissIsClosed(t *testing.T) {
	f := newMissedFlow(t)
	receipt := f.prepare("export audit", "")["receipt"].(string)
	f.wiki("saffron lanterns")
	path := f.journalPath()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	past := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	if err := os.Chtimes(path, past, past); err != nil {
		t.Fatal(err)
	}
	prepared := f.prepare("export audit", receipt) // an article is returned, and no miss has these words
	if knowledge, _ := prepared["knowledge"].([]any); len(knowledge) == 0 {
		t.Fatalf("the search returns the article: %v", prepared["knowledge_context"])
	}
	info, err := os.Stat(path)
	after, readErr := os.ReadFile(path)
	if err != nil || readErr != nil || string(after) != string(data) || !info.ModTime().Equal(past) {
		t.Fatalf("the journal was written although no miss was closed: %v %v %s", err, readErr, after)
	}
}

// Ids are not words: a prepare that carries a receipt and asks for several ids, or for an id in upper case, is not a search of
// words that found nothing, so no miss is kept. A word of 16 characters that is not hexadecimal, and ids mixed with words, are
// searches of words and are kept.
func TestIDsAreNotKeptAsMissedSearchesOfAPrepareWithAReceipt(t *testing.T) {
	f := newMissedFlow(t)
	receipt := f.prepare("export audit", "")["receipt"].(string)
	for _, query := range []string{"0123456789abcdef 0123456789abcdee", "ABCDEF0123456789", "0123456789abcdef  0123456789ABCDEF\n"} {
		f.prepare(query, receipt)
	}
	if got := f.journalMisses(); len(got) != 0 {
		t.Fatalf("ids were kept as missed word searches: %q", got)
	}
	kept := []string{"0123456789abcdeg", "0123456789abcdef saffron"}
	for _, query := range kept {
		f.prepare(query, receipt)
	}
	if got := f.journalMisses(); !slices.Equal(got, kept) {
		t.Fatalf("a word that is not an id, and an id among words, are searches of words: %q", got)
	}
}
