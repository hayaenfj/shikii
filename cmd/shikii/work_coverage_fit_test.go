package main

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/notebook"
	"github.com/hayaenfj/shikii/internal/store"
)

// coverageFitBudget is what limitWorkPreparation compares a reply with at max_bytes 8192: the bound less the newline that the
// encoders add.
const coverageFitBudget = 8192 - 1

// coverageFitReply is a reply with one article whose text is body bytes long and rows named open coverage rows, as the unit tests of
// the bound need it: the entry has the shape prepare gives an article with its text (a notebook.Page, body_included true).
func coverageFitReply(body, rows int) map[string]any {
	page := notebook.Page{ID: "0123456789abcdef", Revision: "fedcba9876543210", Title: "Export filters", Lesson: "Check the export filters first.", AppliesTo: "export audit", Body: strings.Repeat("b", body)}
	entry := map[string]any{"ref": "knowledge1", "id": page.ID, "revision": page.Revision, "article": page, "body_included": true, "query": map[string]any{"operation": "query", "query": "lesson:" + page.ID + "@" + page.Revision}}
	list := []any{}
	for i := range rows {
		list = append(list, map[string]any{"area": fmt.Sprintf("open area %d %s", i+1, strings.Repeat("x", 80)), "source": "00000000000000a1"})
	}
	return map[string]any{"receipt": "r", "how_to_record": strings.Repeat("h", 1200), "knowledge": []any{entry}, "coverage": map[string]any{"open": rows + 2, "rows": list, "next_action": workCoverageNext}}
}

func coverageFitAreas(reply map[string]any) []string {
	areas := []string{}
	for _, raw := range reply["coverage"].(map[string]any)["rows"].([]any) {
		areas = append(areas, raw.(map[string]any)["area"].(string))
	}
	return areas
}

// A reply that fits its bound without the open coverage rows but not with them names fewer rows, the newest first and as many as
// still fit, and every article keeps its text. Before, such a reply took the path of a reply that does not fit: every article
// went down to its summary and the rows stayed (audit of 0.38.0, A-7). The rows that are not named are still open: open counts
// them as it does when more than five areas are open.
func TestLimitWorkPreparationNamesFewerCoverageRowsBeforeAnArticleLosesItsText(t *testing.T) {
	rowSize := workContextSize(coverageFitReply(1, 1)["coverage"].(map[string]any)["rows"].([]any)[0]) + 1 // the row and its comma
	sizeOf := func(body int) int { return workContextSize(coverageFitReply(body, 5)) }
	full := coverageFitReply(0, 5)
	all := coverageFitAreas(full)
	for want := 4; want >= 1; want-- { // the most rows that fit
		t.Run(fmt.Sprintf("the newest %d of 5 rows fit", want), func(t *testing.T) {
			// over the bound by more than the rows that are left out but one take, and by less than all of them take
			body := coverageFitBudget + (5-want-1)*rowSize + rowSize/2 - sizeOf(1) + 1
			reply := coverageFitReply(body, 5)
			if n := workContextSize(reply); n <= coverageFitBudget || n-(5-want-1)*rowSize <= coverageFitBudget || n-(5-want)*rowSize > coverageFitBudget {
				t.Fatalf("the fixture is not sized for %d rows: %d bytes, a row takes %d, the bound is %d", want, n, rowSize, coverageFitBudget)
			}
			read, err := limitWorkPreparation(notebook.Book{}, &workState{}, reply, map[string]string{}, 8192)
			if err != nil {
				t.Fatal(err)
			}
			entry := reply["knowledge"].([]any)[0].(map[string]any)
			if page, ok := entry["article"].(notebook.Page); !ok || len(page.Body) != body || entry["body_included"] != true || entry["attention_limited"] != nil || len(read) != 1 {
				t.Fatalf("the article lost its text before the rows were named fewer: %v (read %v)", entry, read)
			}
			if reply["output"] != nil {
				t.Fatalf("a reply that fits by naming fewer rows has no output: %v", reply["output"])
			}
			coverage := reply["coverage"].(map[string]any)
			if got := coverageFitAreas(reply); !slices.Equal(got, all[:want]) || coverage["open"] != 7 {
				t.Fatalf("the reply names the first %d (newest) rows and open still counts all 7: %q open %v", want, got, coverage["open"])
			}
			if n := workContextSize(reply); n > coverageFitBudget {
				t.Fatalf("the reply is %d bytes, over the bound %d", n, coverageFitBudget)
			}
			// as many rows as fit: one more does not
			more := coverageFitReply(body, 5)
			more["coverage"].(map[string]any)["rows"] = more["coverage"].(map[string]any)["rows"].([]any)[:want+1]
			if n := workContextSize(more); n <= coverageFitBudget {
				t.Fatalf("%d rows would have fit (%d bytes): fewer rows than needed were named", want+1, n)
			}
		})
	}
}

// When even one row does not fit, the reply takes the path it always took: the articles go down to their summaries, with output
// that says so, and all the rows are back as they were (coverage still goes after attention and before pending, only when the
// bound needs it: TestLimitWorkPreparationDropsCoverageAndEarlierSearchInTheirPlace).
func TestLimitWorkPreparationRestoresTheRowsWhenEvenOneDoesNotFit(t *testing.T) {
	reply := coverageFitReply(coverageFitBudget, 5)
	all := coverageFitAreas(reply)
	if _, err := limitWorkPreparation(notebook.Book{}, &workState{}, reply, map[string]string{}, 8192); err != nil {
		t.Fatal(err)
	}
	output, _ := reply["output"].(map[string]any)
	entry := reply["knowledge"].([]any)[0].(map[string]any)
	if output["limited"] != true || entry["body_included"] != false || entry["attention_limited"] != true || entry["article"] != nil {
		t.Fatalf("a reply that does not fit even with one row shortens its articles: %v %v", output, entry)
	}
	if omitted, _ := output["omitted"].([]string); slices.Contains(omitted, "coverage") {
		t.Fatalf("the articles were enough, so coverage stays: %v", omitted)
	}
	if got := coverageFitAreas(reply); !slices.Equal(got, all) || reply["coverage"].(map[string]any)["open"] != 7 {
		t.Fatalf("all the rows are back in coverage: %q", got)
	}
}

// A reply that fits as it is stays as it is: no row is taken off to fit a bound that is met.
func TestLimitWorkPreparationLeavesAReplyThatFitsAsItIs(t *testing.T) {
	reply := coverageFitReply(200, 5)
	before := fmt.Sprint(reply)
	if n := workContextSize(reply); n > coverageFitBudget {
		t.Fatalf("the fixture is over the bound: %d bytes", n)
	}
	if _, err := limitWorkPreparation(notebook.Book{}, &workState{}, reply, map[string]string{}, 8192); err != nil {
		t.Fatal(err)
	}
	if after := fmt.Sprint(reply); after != before || reply["output"] != nil || len(coverageFitAreas(reply)) != 5 {
		t.Fatalf("a reply that fits was changed:\n%s\n%s", before, after)
	}
}

// preparedWithArticle prepares a work of its own on the article, at max_bytes 8192, with the article's text made body bytes long (a
// new revision of the article each time).
func (f coverageFlow) preparedWithArticle(page notebook.Page, body int) (map[string]any, notebook.Page) {
	f.t.Helper()
	page = loopPage(f.t, f.b, page.Sources[0].ID, map[string]any{"id": page.ID, "body": strings.Repeat("b", body)})
	value, err := runWork(f.b.Root, f.b.Scope, "prepare", workRequest{Query: "lesson:" + page.ID, Request: "Audit the export.", MaxBytes: 8192})
	if err != nil {
		f.t.Fatal(err)
	}
	return loopObject(f.t, value), page
}

// The whole flow, with a real wiki and real ledger: seven areas are open (the preparation names five), and an article whose text
// grows. At some size the reply names fewer rows than are open (and than it could) while the article keeps its text; the work that
// is handed that reply is handed the rows the reply named, and the rows it did not name stay open for the next preparation. At no
// size does a reply both take the text from the article and leave rows that did not cost it (the rows are named all, or fewer with
// the text, or the reply is the shortened one with all its rows).
func TestWorkCoveragePreparationNamesFewerRowsBeforeAnArticleLosesItsTextAndHandsOverOnlyThose(t *testing.T) {
	f := newCoverageFlow(t)
	areas := []string{"export one: the filters", "export two: the totals", "export three: the dates", "export four: the headers", "export five: the footers", "export six: the quotes", "export seven: the encodings"}
	prepared := []map[string]any{}
	for range areas {
		prepared = append(prepared, f.prepare()) // all prepared before any records: no work is handed an area
	}
	for i, area := range areas {
		f.record(prepared[i], withRemaining(coverageResult(), area))
	}
	source, err := f.b.CaptureObserved(notebook.Observation{Kind: "test", Title: "Synthetic evidence", Content: "Check the export filters first.", Reference: "synthetic", Version: "1", ObservedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	article := loopPage(t, f.b, source.ID, map[string]any{"title": "Export audit filters", "lesson": "Check the export filters first.", "applies_to": "export audit"})
	named := func(reply map[string]any) int { return len(openAreasOf(t, reply)) }
	textKept := func(reply map[string]any) bool {
		entries, _ := reply["knowledge"].([]any)
		return len(entries) == 1 && entries[0].(map[string]any)["body_included"] == true
	}
	// every size: the text is lost only with all the rows named (or the rows left out whole); never with rows that are named
	// fewer; and the rows named do not grow as the text does
	sweep := func(body int) (reply map[string]any) {
		t.Helper()
		reply, article = f.preparedWithArticle(article, body)
		if !textKept(reply) && reply["coverage"] != nil && named(reply) != workCoverageShown {
			t.Fatalf("body %d: the article lost its text while the reply names %d of %d rows: a reply that shortens its articles names all the rows, or none", body, named(reply), workCoverageShown)
		}
		if coverage, _ := reply["coverage"].(map[string]any); coverage != nil && coverage["open"] != float64(len(areas)) {
			t.Fatalf("body %d: open is %v, want all %d", body, coverage["open"], len(areas))
		}
		return reply
	}
	previous := workCoverageShown
	for body := 250; body <= 3250; body += 250 {
		reply := sweep(body)
		if textKept(reply) {
			if n := named(reply); n > previous {
				t.Fatalf("body %d: the reply names %d rows, more than the %d of a smaller text", body, n, previous)
			} else {
				previous = n
			}
		}
	}
	// the smallest text at which the reply no longer names all five rows: the rows go first, the text stays
	low, high := 250, 3250 // low names all five rows with the text; high does not
	if reply := sweep(low); !textKept(reply) || named(reply) != workCoverageShown {
		t.Fatalf("the fixture: a small article is delivered whole with five rows: %d rows, text %v", named(reply), textKept(reply))
	}
	if reply := sweep(high); named(reply) == workCoverageShown && textKept(reply) {
		t.Fatalf("the fixture: a large article does not fit with five rows")
	}
	for high-low > 1 {
		middle := (low + high) / 2
		if reply := sweep(middle); textKept(reply) && named(reply) == workCoverageShown {
			low = middle
		} else {
			high = middle
		}
	}
	reply := sweep(high)
	listed := openAreasOf(t, reply)
	if !textKept(reply) || len(listed) == 0 || len(listed) >= workCoverageShown {
		t.Fatalf("no text size shows the rows named fewer with the text kept: at %d bytes the text is kept %v and %d rows are named (%q)", high, textKept(reply), len(listed), listed)
	}
	if reply["output"] != nil || reply["coverage"].(map[string]any)["open"] != float64(len(areas)) {
		t.Fatalf("a reply that names fewer rows is not a shortened one, and open counts every open area: %v %v", reply["output"], reply["coverage"])
	}
	if want := []string{"export seven: the encodings", "export six: the quotes", "export five: the footers", "export four: the headers", "export three: the dates"}; !slices.Equal(listed, want[:len(listed)]) {
		t.Fatalf("the rows named are the newest: %q", listed)
	}
	if shown := f.state(reply).CoverageShown; !slices.Equal(shown, listed) {
		t.Fatalf("the work remembers the rows the reply named (%q), not the five it asked for: %q", listed, shown)
	}
	// the work that was handed this reply is handed those rows, and no other
	saved := f.record(reply, workCompletion{Changed: "Read the export.", Evidence: []string{"new1"}})
	handed := []string{}
	for _, raw := range handedOverOf(saved) {
		handed = append(handed, raw.(string))
	}
	slices.Sort(handed)
	if want := slices.Sorted(slices.Values(listed)); !slices.Equal(handed, want) {
		t.Fatalf("the work is handed over the rows its reply named: %q, want %q", handed, want)
	}
	open, err := (store.Store{Dir: f.b.Root}).OpenCoverage(f.b.Scope)
	if err != nil {
		t.Fatal(err)
	}
	stillOpen := []string{}
	for _, row := range open {
		stillOpen = append(stillOpen, row.Area)
	}
	slices.Sort(stillOpen)
	if want := slices.Sorted(func(yield func(string) bool) {
		for _, area := range areas {
			if !slices.Contains(listed, area) && !yield(area) {
				return
			}
		}
	}); !slices.Equal(stillOpen, want) {
		t.Fatalf("the rows the reply did not name are still open: %q, want %q", stillOpen, want)
	}
	if next := f.prepare(); next["coverage"].(map[string]any)["open"] != float64(len(areas)-len(listed)) {
		t.Fatalf("the next preparation counts what is still open: %v", next["coverage"])
	}
}
