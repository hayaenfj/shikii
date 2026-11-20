package main

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/notebook"
)

// foundAloneQuery is five words, of which only "export" is in the one article of newMissedFlow: no article holds two of them, so the
// search finds nothing, and "export" alone finds the article.
const foundAloneQuery = "saffron lanterns export kestrel harbor"

// foundAloneOf is the found_alone of a reply: its words and its note. A reply without it fails the test where it is read.
func foundAloneOf(t *testing.T, reply map[string]any) ([]string, string) {
	t.Helper()
	alone, ok := reply["found_alone"].(map[string]any)
	if !ok {
		t.Fatalf("the reply has no found_alone: %v", reply)
	}
	if len(alone) != 2 {
		t.Fatalf("found_alone holds the words and the note only: %v", alone)
	}
	words := []string{}
	for _, raw := range alone["words"].([]any) {
		words = append(words, raw.(string))
	}
	next, _ := alone["next_action"].(string)
	return words, next
}

// A wiki query of several words that finds nothing says which of its words find an article alone, and still records the miss of the
// whole query. The note is about 300 bytes at most and says what to do with it. A search that finds an article, a miss of one word, and
// a miss no word of which finds an article carry none.
func TestWikiQueryNamesTheWordsThatFindAnArticleAlone(t *testing.T) {
	f := newMissedFlow(t)
	reply := f.wiki(foundAloneQuery)
	words, next := foundAloneOf(t, reply)
	if !slices.Equal(words, []string{"export"}) || next != foundAloneNext {
		t.Fatalf("the one word that finds the article alone, and the note: %q %q", words, next)
	}
	if len(next) > 300 {
		t.Fatalf("the note is about 300 bytes at most: %d", len(next))
	}
	for _, part := range []string{
		"No article has these words together",
		"a search of several words usually needs at least two of them in one article",
		"each word in `words` finds an article",
		"search one of them",
		"Search alternatives for one thing one word at a time",
		"not an instruction to keep searching",
	} {
		if !strings.Contains(next, part) {
			t.Fatalf("the note says %q: %q", part, next)
		}
	}
	if lessons, _ := reply["lessons"].([]any); len(lessons) != 0 || reply["earlier_search"] != nil {
		t.Fatalf("the search itself is as it was: %v", reply)
	}
	if got := f.journalMisses(); !slices.Equal(got, []string{foundAloneQuery}) {
		t.Fatalf("the miss of the whole query is recorded as before: %q", got)
	}
	// an array, not a map: the journal keeps its misses in the order they were searched, and the check below reads that order
	for _, c := range []struct{ name, query string }{
		{"a search that finds an article", "export audit kestrel"},
		{"a miss of one word", "saffron"},
		{"words none of which finds an article", "saffron lanterns"},
	} {
		if reply := f.wiki(c.query); reply["found_alone"] != nil {
			t.Fatalf("%s carries no found_alone: %v", c.name, reply["found_alone"])
		}
	}
	if got := f.journalMisses(); !slices.Equal(got, []string{foundAloneQuery, "saffron", "saffron lanterns"}) {
		t.Fatalf("each search is recorded as before, and the one that found an article is not: %q", got)
	}
}

// The search of a prepare that carries a receipt says the same; the standalone preparation (its query is the topic of a task, not a
// question) and a search that finds an article say nothing. The reply still names the miss it recorded.
func TestPrepareWithAReceiptNamesTheWordsThatFindAnArticleAlone(t *testing.T) {
	f := newMissedFlow(t)
	if standalone := f.prepare(foundAloneQuery, ""); standalone["found_alone"] != nil {
		t.Fatalf("the topic of a standalone preparation is not a search that was asked: %v", standalone["found_alone"])
	}
	receipt := f.prepare("export audit", "")["receipt"].(string)
	prepared := f.prepare(foundAloneQuery, receipt)
	words, next := foundAloneOf(t, prepared)
	if !slices.Equal(words, []string{"export"}) || next != foundAloneNext {
		t.Fatalf("the search of a prepare is told the same: %q %q", words, next)
	}
	if got := f.journalMisses(); !slices.Equal(got, []string{foundAloneQuery}) {
		t.Fatalf("its miss is recorded as before: %q", got)
	}
	if found := f.prepare("export audit", receipt); found["found_alone"] != nil {
		t.Fatalf("a search of a prepare that finds an article carries none: %v", found["found_alone"])
	}
	if one := f.prepare("saffron", receipt); one["found_alone"] != nil {
		t.Fatalf("a miss of one word carries none: %v", one["found_alone"])
	}
}

// The same over MCP, where the agent reads it in the structured result and in the text: a query, and the search of a prepare that
// carries a receipt.
func TestMCPNamesTheWordsThatFindAnArticleAlone(t *testing.T) {
	f := newMCPWikiFlow(t)
	saved, err := f.book.CaptureObserved(notebook.Observation{Kind: "test", Title: "Export notes", Content: "The export kept its filters.", Reference: "fixture", Version: "v1", ObservedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatal(err)
	}
	loopPage(t, f.book, saved.ID, map[string]any{"title": "Export audit workflow", "lesson": "Audit the export filters and totals.", "applies_to": "export audit"})
	answer := f.query(foundAloneQuery)
	answer.holds(t, "found_alone", "export", foundAloneNext[:40])
	words, next := foundAloneOf(t, answer.Structured)
	if !slices.Equal(words, []string{"export"}) || next != foundAloneNext {
		t.Fatalf("the query over MCP: %q %q", words, next)
	}
	if found := f.query("export audit kestrel"); found.Structured["found_alone"] != nil {
		t.Fatalf("a search that finds an article carries none: %.300s", found.Text)
	}
	receipt, _ := f.prepare().Structured["receipt"].(string)
	prepared := f.tool(map[string]any{"operation": "prepare", "work": map[string]any{"receipt": receipt, "query": foundAloneQuery}})
	prepared.holds(t, "found_alone", "export", foundAloneNext[:40])
	if words, _ := foundAloneOf(t, prepared.Structured); !slices.Equal(words, []string{"export"}) {
		t.Fatalf("the prepare over MCP: %q", words)
	}
}

// A preparation whose bound is tight leaves found_alone out where it leaves earlier_search out (the same step, right after it and
// before pending and observations), only as far as the bound needs, and names it in output.omitted.
func TestLimitWorkPreparationDropsFoundAloneWithEarlierSearch(t *testing.T) {
	pad := func(n int) string { return strings.Repeat("x", n) }
	for _, c := range []struct {
		name           string
		earlier, alone int // the size of the note of each; 0 is not in the reply
		omitted        []string
	}{
		{"a small found_alone stays when coverage was enough", 0, 200, []string{"reasoning", "reading", "attention", "coverage"}},
		{"a large found_alone goes right after coverage", 0, 1400, []string{"reasoning", "reading", "attention", "coverage", "found_alone"}},
		{"found_alone goes right after earlier_search", 1400, 1400, []string{"reasoning", "reading", "attention", "coverage", "earlier_search", "found_alone"}},
		{"earlier_search goes alone while it is enough", 1400, 200, []string{"reasoning", "reading", "attention", "coverage", "earlier_search"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			reply := map[string]any{
				"receipt": "r", "how_to_record": pad(2000),
				"reasoning": map[string]any{"p": pad(30)}, "reading": map[string]any{"p": pad(30)}, "attention": map[string]any{"p": pad(30)},
				"coverage": map[string]any{"open": 3, "rows": []any{map[string]any{"area": pad(1500), "source": "s"}}},
				"pending":  []any{map[string]any{"p": pad(2500)}}, "observations": []any{map[string]any{"p": pad(2500)}},
			}
			if c.earlier > 0 {
				reply["earlier_search"] = map[string]any{"query": "q", "result": "0123456789abcdef", "next_action": pad(c.earlier)}
			}
			if c.alone > 0 {
				reply["found_alone"] = map[string]any{"words": []any{"w"}, "next_action": pad(c.alone)}
			}
			if _, err := limitWorkPreparation(notebook.Book{}, &workState{}, reply, map[string]string{}, 8192); err != nil {
				t.Fatal(err)
			}
			output, _ := reply["output"].(map[string]any)
			omitted, _ := output["omitted"].([]string)
			if !slices.Equal(omitted, c.omitted) {
				t.Fatalf("omitted is %v, want %v", omitted, c.omitted)
			}
			if reply["pending"] == nil || reply["observations"] == nil || (reply["found_alone"] != nil) != (c.alone > 0 && !slices.Contains(omitted, "found_alone")) {
				t.Fatalf("pending and observations stay, found_alone goes only when named: %v", reply)
			}
		})
	}
}
