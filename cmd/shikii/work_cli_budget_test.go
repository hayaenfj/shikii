package main

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/notebook"
)

// An agent reads the CLI's reply through its shell tool, which shows a bounded part of a command's output and cuts the middle of a
// longer one: Claude Code shows 30,000 characters, Codex 10,000 tokens (about 40,000 bytes). The CLI's default bound was 64 KiB,
// twice what the MCP server sends by default (its bound counts the text and the structured copy of the same reply). Measured on
// 2026-11-11 with that default: 14 to 28% of the prepare replies an agent read directly were cut by the tool, and most agents had
// taken to saving the reply to a file and reading it back with a script. The CLI now bounds a prepare reply to 28 KiB unless
// max_bytes asks for another size: what does not fit is left out by shikii, which names it in output and keeps the way back to it,
// instead of by the tool, which says nothing. The HTTP endpoint and the MCP server keep their bound.
func TestCLIPrepareReplyFitsWhatAnAgentsShellShows(t *testing.T) {
	repo := gitRepo(t)
	root := hookEnv(t, nil, repo)
	t.Chdir(repo)
	b := notebook.Book{Root: root, Scope: feedbackScope(repo)}
	source, err := b.Capture(notebook.Observation{Kind: "test", Title: "Synthetic check", Content: "The CSV export keeps its filters.", Reference: "synthetic", Version: "1", ObservedAt: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 40; i++ {
		body := fmt.Sprintf("Page %d. ", i) + strings.Repeat("The export keeps the filters in every format. ", 12)
		applies := fmt.Sprintf("CSV export filters, case %d: ", i) + strings.Repeat("when a report is exported with its filters ", 12)
		if _, err := b.Ingest(notebook.Draft{Sources: []string{source.ID}, Title: fmt.Sprintf("CSV export filters %d", i), Lesson: "Keep the filters when exporting CSV.", AppliesTo: applies, Kind: "rule", Scene: "when exporting CSV", Body: &body}); err != nil {
			t.Fatal(err)
		}
	}
	prepare := func(extra string) (string, map[string]any) {
		t.Helper()
		var out string
		withStdin(t, `{"query":"CSV export filters","request":"Check the CSV export"`+extra+`}`, func() { out = capture(t, func() error { return cmdWork([]string{"prepare"}) }) })
		var reply map[string]any
		if err := json.Unmarshal([]byte(out), &reply); err != nil {
			t.Fatalf("%v: %.200s", err, out)
		}
		return out, reply
	}
	out, reply := prepare("")
	if len(out) > 28<<10 {
		t.Errorf("the CLI reply of work prepare is %d bytes without max_bytes, over the 28 KiB an agent's shell shows whole", len(out))
	}
	output, _ := reply["output"].(map[string]any)
	if reply["receipt"] == nil || reply["how_to_record"] == nil || len(reply["knowledge"].([]any)) == 0 {
		t.Errorf("the bounded reply keeps the receipt, how to record and the lessons: %.300s", out)
	}
	if output == nil || output["limited"] != true || output["max_bytes"] != float64(28<<10) || !strings.Contains(fmt.Sprint(output["omitted"]), "knowledge_context") ||
		!strings.Contains(fmt.Sprint(output["next_action"]), "max_bytes") || !strings.Contains(fmt.Sprint(output["next_action"]), "knowledge_query") {
		t.Errorf("the bounded reply names its bound, what it left out and the way back: %v", output)
	}
	if reply["knowledge_query"] == nil {
		t.Errorf("the search that finds the candidates left out stays in the reply: %.300s", out)
	}
	// asking for more gives more
	large, largeReply := prepare(`,"max_bytes":65536`)
	if len(large) <= 28<<10 || largeReply["knowledge_context"] == nil {
		t.Errorf("max_bytes 65536 returns the candidates too (%d bytes)", len(large))
	}
	// a task too long for a bound the caller named is refused with the bound and how to ask for more, so one more call is enough
	// (a task is never cut)
	var refusal error
	withStdin(t, `{"query":"CSV export filters","max_bytes":8192,"request":"`+strings.Repeat("Check the CSV export. ", 600)+`"}`, func() {
		_, refusal = captureErr(func() error { return cmdWork([]string{"prepare"}) })
	})
	if refusal == nil || !strings.Contains(refusal.Error(), "exceed max_bytes (8192)") || !strings.Contains(refusal.Error(), "prepare again with a larger max_bytes (up to 393216)") {
		t.Errorf("a task that cannot fit the bound names the bound and the next call: %v", refusal)
	}
	withStdin(t, `{"query":"CSV export filters","request":"`+strings.Repeat("Check the CSV export. ", 1500)+`"}`, func() {
		_, refusal = captureErr(func() error { return cmdWork([]string{"prepare"}) })
	})
	if refusal == nil || !strings.Contains(refusal.Error(), "request takes at most 16000 bytes (got 33000)") {
		t.Errorf("a task text over its own bound names that bound: %v", refusal)
	}
	// the HTTP endpoint keeps its own bound
	h := newServer(serveOpts{storeDir: root, projectScope: feedbackScope(repo)})
	r := httptest.NewRequest("POST", "/v1/wiki/notebook", strings.NewReader(`{"operation":"prepare","work":{"query":"CSV export filters","request":"Check the CSV export"}}`))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 || w.Body.Len() <= 28<<10 {
		t.Errorf("the HTTP endpoint keeps its 64 KiB bound: %d, %d bytes", w.Code, w.Body.Len())
	}
}

// A reply that did not fit its bound lost the whole list of search candidates at once (about half of a real reply), although the
// lessons and the rest fitted: the agent saw no title of any other page that matched. The list is now reduced in steps, the least
// useful part first: the heads of other pending conversations (pending carries them), then each candidate's applies_to (its title,
// id and exact query stay), then the candidates at the end of the ranking, with a cursor in knowledge_query that continues right
// after the last one shown. output names each step taken. Only when not even the top candidates fit is the list left out whole.
func TestABoundedReplyKeepsTheTopSearchCandidates(t *testing.T) {
	repo := gitRepo(t)
	root := hookEnv(t, nil, repo)
	t.Chdir(repo)
	b := notebook.Book{Root: root, Scope: feedbackScope(repo)}
	source, err := b.Capture(notebook.Observation{Kind: "test", Title: "Synthetic check", Content: "The CSV export keeps its filters.", Reference: "synthetic", Version: "1", ObservedAt: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 40; i++ {
		body := fmt.Sprintf("Page %d. ", i) + strings.Repeat("The export keeps the filters in every format. ", 12)
		applies := fmt.Sprintf("CSV export filters, case %d: ", i) + strings.Repeat("when a report is exported with its filters ", 12)
		if _, err := b.Ingest(notebook.Draft{Sources: []string{source.ID}, Title: fmt.Sprintf("CSV export filters %d", i), Lesson: "Keep the filters when exporting CSV.", AppliesTo: applies, Kind: "rule", Scene: "when exporting CSV", Body: &body}); err != nil {
			t.Fatal(err)
		}
	}
	prepare := func(extra string) (string, map[string]any) {
		t.Helper()
		var out string
		withStdin(t, `{"query":"CSV export filters","request":"Check the CSV export"`+extra+`}`, func() { out = capture(t, func() error { return cmdWork([]string{"prepare"}) }) })
		var reply map[string]any
		if err := json.Unmarshal([]byte(out), &reply); err != nil {
			t.Fatalf("%v: %.200s", err, out)
		}
		return out, reply
	}
	candidates := func(reply map[string]any) ([]string, float64) {
		t.Helper()
		context, _ := reply["knowledge_context"].(map[string]any)
		search, _ := context["search"].(map[string]any)
		list, _ := search["candidates"].([]any)
		var ids []string
		for _, raw := range list {
			ids = append(ids, raw.(map[string]any)["id"].(string))
		}
		total, _ := search["total"].(float64)
		return ids, total
	}
	_, whole := prepare(`,"max_bytes":393216`)
	all, total := candidates(whole)
	if len(all) < 40 || int(total) != len(all) {
		t.Fatalf("precondition: the unbounded reply lists every candidate: %d of %v", len(all), total)
	}
	for _, c := range []struct {
		name, extra string
		bound       int
		cut         bool
	}{
		{"the CLI's own bound", ``, 28 << 10, false},
		{"a bound of 12000 bytes", `,"max_bytes":12000`, 12000, true},
	} {
		out, reply := prepare(c.extra)
		shown, shownTotal := candidates(reply)
		output, _ := reply["output"].(map[string]any)
		omitted := fmt.Sprint(output["omitted"])
		if len(out) > c.bound {
			t.Errorf("%s: the reply is %d bytes", c.name, len(out))
		}
		if len(shown) == 0 || int(shownTotal) != len(all) || !reflect.DeepEqual(shown, all[:len(shown)]) {
			t.Errorf("%s: the reply keeps the top candidates in their order and the total: %d shown, total %v of %d", c.name, len(shown), shownTotal, len(all))
			continue
		}
		first := reply["knowledge_context"].(map[string]any)["search"].(map[string]any)["candidates"].([]any)[0].(map[string]any)
		if first["title"] == nil || first["query"] == nil || first["applies_to"] != nil || !strings.Contains(omitted, "knowledge_context.search.candidates[].applies_to") || !strings.Contains(omitted, "knowledge_context.pending_sources") {
			t.Errorf("%s: a shown candidate keeps its title and exact query, and output names what was taken off: %v, omitted %s", c.name, first, omitted)
		}
		if strings.Contains(omitted, " knowledge_context ") || strings.HasSuffix(strings.TrimSuffix(omitted, "]"), " knowledge_context") {
			t.Errorf("%s: the candidates are not left out whole: %s", c.name, omitted)
		}
		for _, raw := range reply["knowledge"].([]any) {
			if entry := raw.(map[string]any); entry["title"] == nil {
				t.Errorf("%s: a lesson in the reply keeps its title: %v", c.name, entry)
			}
		}
		query, _ := reply["knowledge_query"].(map[string]any)
		cursor, _ := query["cursor"].(string)
		if c.cut != (len(shown) < len(all)) || (len(shown) < len(all)) != (cursor != "") {
			t.Errorf("%s: %d of %d candidates shown, cursor %q", c.name, len(shown), len(all), cursor)
			continue
		}
		if cursor == "" {
			continue
		}
		if !strings.Contains(omitted, fmt.Sprintf("knowledge_context.search.candidates after the first %d", len(shown))) {
			t.Errorf("%s: output says where the list was cut: %s", c.name, omitted)
		}
		// the cursor continues right after the last candidate shown, without a gap or a repeat
		rest := append([]string{}, shown...)
		for cursor != "" {
			var page struct {
				Search struct {
					Candidates []struct {
						ID string `json:"id"`
					} `json:"candidates"`
				} `json:"search"`
				NextCursor string `json:"next_cursor"`
			}
			text := capture(t, func() error { return cmdWiki([]string{"query", "CSV", "export", "filters", "continue:" + cursor}) })
			if err := json.Unmarshal([]byte(text), &page); err != nil || len(page.Search.Candidates) == 0 {
				t.Fatalf("%s: the cursor reads on: %v %.200s", c.name, err, text)
			}
			for _, candidate := range page.Search.Candidates {
				rest = append(rest, candidate.ID)
			}
			cursor = page.NextCursor
		}
		if !reflect.DeepEqual(rest, all) {
			t.Errorf("%s: the candidates shown and the pages after them are the whole ranking once: %d of %d", c.name, len(rest), len(all))
		}
	}
	// a bound so small that not even one candidate fits beside the lessons still leaves the list out whole, as before
	_, tiny := prepare(`,"max_bytes":8192`)
	if output, _ := tiny["output"].(map[string]any); tiny["knowledge_context"] != nil || !strings.Contains(fmt.Sprint(output["omitted"]), "knowledge_context") || strings.Contains(fmt.Sprint(output["omitted"]), "knowledge_context.") {
		t.Errorf("the smallest bound leaves the candidates out whole and says only that: %v", tiny["output"])
	}
	// lessons too long for a small bound are down to their references, and each still carries its title
	for i := 0; i < 6; i++ {
		long := fmt.Sprintf("Ledger totals %d: ", i) + strings.Repeat("compare the ledger totals with the bank statement before closing the month. ", 20)
		body := "Checked."
		if _, err := b.Ingest(notebook.Draft{Sources: []string{source.ID}, Title: fmt.Sprintf("Ledger totals %d", i), Lesson: long, AppliesTo: long[:400], Kind: "rule", Scene: "when closing the month", Body: &body}); err != nil {
			t.Fatal(err)
		}
	}
	var out string
	withStdin(t, `{"query":"ledger totals bank statement","request":"Close the month","max_bytes":8192}`, func() { out = capture(t, func() error { return cmdWork([]string{"prepare"}) }) })
	var small map[string]any
	if err := json.Unmarshal([]byte(out), &small); err != nil {
		t.Fatal(err)
	}
	lessons, _ := small["knowledge"].([]any)
	if len(lessons) < 2 || len(out) > 8192 {
		t.Fatalf("the smallest bound still names the lessons: %d lessons, %d bytes", len(lessons), len(out))
	}
	reduced := 0
	for i, raw := range lessons {
		entry := raw.(map[string]any)
		if entry["summary"] != nil || entry["orientation"] != nil || entry["article"] != nil {
			if reduced > 0 {
				t.Errorf("the lessons lose their text from the end of the ranking: entry %d is whole after a reduced one", i)
			}
			continue
		}
		reduced++
		if title, _ := entry["title"].(string); !strings.HasPrefix(title, "Ledger totals") || entry["query"] == nil {
			t.Errorf("a lesson down to its references keeps its title and its exact query: %v", entry)
		}
	}
	if reduced == 0 || !strings.Contains(fmt.Sprint(small["output"]), "knowledge[] text and summaries, the last entries first") {
		t.Fatalf("precondition: at the smallest bound some long lesson is down to its references, and output says so: %d reduced, %v", reduced, small["output"])
	}
}
