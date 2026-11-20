package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/notebook"
)

func TestMCPNotificationCannotDeliverOrMutateWork(t *testing.T) {
	for _, id := range []json.RawMessage{nil, json.RawMessage("null")} {
		calls := 0
		target := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls++
			writeJSON(w, 200, map[string]any{"saved": true})
		})
		params := json.RawMessage(`{"name":"shikii_wiki","arguments":{"operation":"prepare","work":{"query":"synthetic","request":"Check an unacknowledged delivery."}}}`)
		reply := handleRPC(rpcRequest{JSONRPC: "2.0", ID: id, Method: "tools/call", Params: params}, target)
		if reply != nil || calls != 0 {
			t.Fatal("a tool notification has no response and cannot establish delivery or mutate work")
		}
	}
}

// mcpWikiFlow drives the shikii_wiki tool the way an agent does: a JSON-RPC tools/call that runMCP answers from the real HTTP handler
// of a project's store. Nothing here calls runNotebook or runWork directly, so the MCP layer (the project routing, the strict
// decoding, the reply sizing and the text the agent reads) is in the path of every assertion.
type mcpWikiFlow struct {
	t    *testing.T
	book notebook.Book
	h    http.Handler
}

// mcpAnswer is what the agent receives of one tool call: the structured result and the text of the content.
type mcpAnswer struct {
	Structured map[string]any
	Text       string
	IsError    bool
}

func newMCPWikiFlow(t *testing.T) mcpWikiFlow {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	root := filepath.Join(dir, ".shikii")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	return mcpWikiFlow{t: t, book: notebook.Book{Root: root, Scope: dir}, h: newServer(serveOpts{storeDir: root, projectScope: dir})}
}

// call sends one shikii_wiki tools/call and reads the answer; a JSON-RPC error fails the test, a tool error is returned as IsError.
func (f mcpWikiFlow) call(arguments map[string]any) mcpAnswer {
	f.t.Helper()
	request := mcpCall(1, "shikii_wiki", arguments)
	request["jsonrpc"] = "2.0"
	var in, out bytes.Buffer
	if err := json.NewEncoder(&in).Encode(request); err != nil {
		f.t.Fatal(err)
	}
	if err := runMCP(bufio.NewReader(&in), &out, f.h); err != nil {
		f.t.Fatal(err)
	}
	var response struct {
		Result map[string]any `json:"result"`
		Error  any            `json:"error"`
	}
	if err := json.Unmarshal(out.Bytes(), &response); err != nil || response.Error != nil || response.Result == nil {
		f.t.Fatalf("a tool call is answered with a result: %v %v %s", err, response.Error, out.String())
	}
	content, _ := response.Result["content"].([]any)
	if len(content) != 1 {
		f.t.Fatalf("the result carries one text: %v", response.Result)
	}
	text, _ := content[0].(map[string]any)["text"].(string)
	answer := mcpAnswer{Text: text, IsError: response.Result["isError"] == true}
	answer.Structured, _ = response.Result["structuredContent"].(map[string]any)
	return answer
}

// tool is call for a tool call that must succeed and carry a structured result.
func (f mcpWikiFlow) tool(arguments map[string]any) mcpAnswer {
	f.t.Helper()
	answer := f.call(arguments)
	if answer.IsError || answer.Structured == nil {
		f.t.Fatalf("the call %v failed: %s", arguments, answer.Text)
	}
	return answer
}

func (f mcpWikiFlow) query(words string) mcpAnswer {
	f.t.Helper()
	return f.tool(map[string]any{"operation": "query", "query": words})
}

// prepare is a standalone preparation (the agent has no hook): every call is a session of its own.
func (f mcpWikiFlow) prepare() mcpAnswer {
	f.t.Helper()
	return f.tool(map[string]any{"operation": "prepare", "work": map[string]any{"query": "export audit", "request": "Audit the export."}})
}

// record sends a result that cites an observation of its call as new1.
func (f mcpWikiFlow) record(prepared mcpAnswer, result map[string]any) mcpAnswer {
	f.t.Helper()
	receipt, _ := prepared.Structured["receipt"].(string)
	if receipt == "" {
		f.t.Fatalf("the preparation names a receipt: %s", prepared.Text)
	}
	return f.tool(map[string]any{"operation": "record", "work": map[string]any{
		"receipt": receipt, "result": result,
		"observations": []any{map[string]any{"kind": "test", "title": "Export check", "content": "The synthetic export kept its filters."}},
	}})
}

// holds fails the test unless the answer carries the key in both forms the agent can read it in (structured and text) and the text
// holds every given word.
func (a mcpAnswer) holds(t *testing.T, key string, words ...string) {
	t.Helper()
	if a.Structured[key] == nil || !strings.Contains(a.Text, `"`+key+`"`) {
		t.Fatalf("the agent receives %q in both the structured result and the text: %v | %.300s", key, a.Structured[key], a.Text)
	}
	for _, word := range words {
		if !strings.Contains(a.Text, word) {
			t.Fatalf("the text the agent reads of %q holds %q: %.400s", key, word, a.Text)
		}
	}
}

// An agent that sends {"operation":"query","query":"<id>"} over MCP reads the saved source: the id was saved over MCP too, and the
// content asserted is the text that was saved.
func TestMCPQueryOfASourceIDReadsTheSavedSource(t *testing.T) {
	f := newMCPWikiFlow(t)
	const saved = "Saffron lanterns burn for six hours."
	id, _ := f.tool(map[string]any{"operation": "capture", "observation": map[string]any{"kind": "test", "title": "Lantern notes", "content": saved, "reference": "fixture", "version": "v1", "observed_at": "2026-01-01T00:00:00Z"}}).Structured["id"].(string)
	if id == "" {
		t.Fatal("capture names the id of the saved source")
	}
	answer := f.query(id)
	observations, _ := answer.Structured["observations"].([]any)
	if len(observations) != 1 || observations[0].(map[string]any)["content"] != saved || !strings.Contains(answer.Text, saved) {
		t.Fatalf("a query of the id returns the saved text, in the structured result and in the text: %.300s", answer.Text)
	}
	if answer.Structured["search"] != nil || answer.Structured["lessons"] != nil {
		t.Fatalf("the reply is a source read, not the empty word search the agents mistook for an empty result: %.300s", answer.Text)
	}
	// an id that names no saved source is a tool error the agent reads, not an empty word search
	if unknown := f.call(map[string]any{"operation": "query", "query": "0123456789abcdef"}); !unknown.IsError || !strings.Contains(unknown.Text, "does not belong to this project") {
		t.Fatalf("an unknown id is refused as a tool error: %v %.200s", unknown.IsError, unknown.Text)
	}
	// a word search over MCP is still a word search
	if words := f.query("lantern"); words.Structured["search"] == nil || words.Structured["observations"] != nil {
		t.Fatalf("words keep searching: %.300s", words.Text)
	}
}

// An agent that sends the id of an article as the whole query over MCP reads the tool error: it keeps the refusal at the front and
// says how to read an article (lesson:<id>) and how to search for the id as a word. The first form is then sent over MCP too.
func TestMCPQueryOfAnArticleIDTellsHowToReadTheArticle(t *testing.T) {
	f := newMCPWikiFlow(t)
	saved, err := f.book.CaptureObserved(notebook.Observation{Kind: "test", Title: "Lantern notes", Content: "Saffron lanterns burn for six hours.", Reference: "fixture", Version: "v1", ObservedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatal(err)
	}
	article := loopPage(t, f.book, saved.ID, nil)
	refused := f.call(map[string]any{"operation": "query", "query": article.ID})
	// the text of a tool error is JSON, which writes < and > as \u003c and \u003e; the agent reads the decoded sentence
	var body struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal([]byte(refused.Text), &body); err != nil || !refused.IsError {
		t.Fatalf("an article id sent as the query is a tool error the agent reads: %v %v %.400s", refused.IsError, err, refused.Text)
	}
	for _, word := range []string{"wiki source does not belong to this project; ", "lesson:", "a query made only of 16-digit ids reads saved sources: for an article send lesson:<id>, and to search for the id as a word add another word"} {
		if !strings.Contains(body.Error, word) {
			t.Fatalf("the tool error the agent reads holds %q: %.400s", word, body.Error)
		}
	}
	read := f.query("lesson:" + article.ID)
	if lessons, _ := read.Structured["lessons"].([]any); len(lessons) != 1 || lessons[0].(map[string]any)["id"] != article.ID || !strings.Contains(read.Text, article.ID) {
		t.Fatalf("lesson:<id> over MCP returns the article: %.300s", read.Text)
	}
}

// The path of the missed-search feature as an agent meets it, over MCP only: a word query finds nothing, the work records a result,
// the same words from another session are told where that result is, and a query of exactly that id returns it.
func TestMCPEarlierSearchPointsToAResultThatAQueryOfItsIDReads(t *testing.T) {
	f := newMCPWikiFlow(t)
	first := f.prepare()
	if miss := f.query("saffron lanterns"); miss.Structured["earlier_search"] != nil || len(miss.Structured["lessons"].([]any)) != 0 {
		t.Fatalf("the first search finds nothing and has no earlier work to point at: %.300s", miss.Text)
	}
	saved := f.record(first, map[string]any{"changed": "Audited the export.", "checked": "Read the export code.", "evidence": []any{"new1"}})
	saved.holds(t, "missed_searches", "saffron lanterns")
	id := resultID(t, saved.Structured)
	f.prepare() // another session
	again := f.query("  saffron   lanterns ")
	again.holds(t, "earlier_search", "saffron lanterns", id)
	earlier, _ := again.Structured["earlier_search"].(map[string]any)
	if earlier["result"] != id || earlier["query"] != "saffron lanterns" {
		t.Fatalf("earlier_search names the words and the id of the saved result: %v", earlier)
	}
	read := f.tool(map[string]any{"operation": "query", "query": earlier["result"].(string)})
	for _, recorded := range []string{"Audited the export.", "Read the export code."} {
		if !strings.Contains(read.Text, recorded) || !strings.Contains(jsonText(read.Structured), recorded) {
			t.Fatalf("a query of the id in earlier_search returns the result the work recorded, which holds %q: %.300s", recorded, read.Text)
		}
	}
	if read.Structured["search"] != nil || read.Structured["lessons"] != nil {
		t.Fatalf("the reply is the saved result, not a word search of the id: %.300s", read.Text)
	}
}

// The handover of what a work left unexamined, over MCP only: the area is opened by the first record (it names it in coverage.open),
// the next preparation lists it as from remaining, the work that was handed it closes it by recording (coverage.handed_over), and a
// third preparation lists nothing.
func TestMCPCoverageHandoverFromRemainingToTheNextWork(t *testing.T) {
	f := newMCPWikiFlow(t)
	const area = "The import path was not read."
	first := f.record(f.prepare(), map[string]any{"changed": "Audited the export.", "checked": "Read the export code.", "evidence": []any{"new1"}, "remaining": area})
	first.holds(t, "coverage", area)
	opened, _ := first.Structured["coverage"].(map[string]any)["open"].([]any)
	if first.Structured["complete"] != true || len(opened) != 1 || opened[0] != area {
		t.Fatalf("the record names the area it opened: %.300s", first.Text)
	}
	second := f.prepare()
	second.holds(t, "coverage", area, `"from":"remaining"`)
	rows, _ := second.Structured["coverage"].(map[string]any)["rows"].([]any)
	if len(rows) != 1 || rows[0].(map[string]any)["area"] != area || rows[0].(map[string]any)["from"] != "remaining" {
		t.Fatalf("another session's preparation lists the area as from remaining: %.300s", second.Text)
	}
	closed := f.record(second, map[string]any{"changed": "Read the import path.", "evidence": []any{"new1"}})
	closed.holds(t, "coverage", area, "handed_over")
	handed, _ := closed.Structured["coverage"].(map[string]any)["handed_over"].([]any)
	if len(handed) != 1 || handed[0] != area {
		t.Fatalf("the work that was handed the area says it took it over: %.300s", closed.Text)
	}
	if third := f.prepare(); third.Structured["coverage"] != nil || strings.Contains(third.Text, area) {
		t.Fatalf("the area is closed: a third preparation lists none: %.300s", third.Text)
	}
}

// A result that sends coverage rows and no open one hands its remaining over, over MCP only: the record names the area in
// coverage.open next to rows, the next preparation lists it as from remaining, and the clear row did not become an open area.
func TestMCPCoverageRowsDoNotKeepTheRemainingFromTheNextWork(t *testing.T) {
	f := newMCPWikiFlow(t)
	const area = "The import path was not read."
	first := f.record(f.prepare(), map[string]any{"changed": "Audited the export.", "checked": "Read the export code.", "evidence": []any{"new1"}, "remaining": area,
		"coverage": []any{map[string]any{"area": "export filters", "outcome": "clear", "evidence": "new1"}}})
	first.holds(t, "coverage", area)
	reply, _ := first.Structured["coverage"].(map[string]any)
	opened, _ := reply["open"].([]any)
	if first.Structured["complete"] != true || reply["rows"] != float64(1) || len(opened) != 1 || opened[0] != area {
		t.Fatalf("the record names its row and the area it opened from its remaining: %.300s", first.Text)
	}
	second := f.prepare()
	second.holds(t, "coverage", area, `"from":"remaining"`)
	rows, _ := second.Structured["coverage"].(map[string]any)["rows"].([]any)
	if len(rows) != 1 || rows[0].(map[string]any)["area"] != area || rows[0].(map[string]any)["from"] != "remaining" || strings.Contains(second.Text, "export filters") {
		t.Fatalf("another session's preparation lists the area of the remaining, from remaining, and only it: %.300s", second.Text)
	}
}

// The reply of a record is not reshaped by the MCP layer: a work that missed words and left an area unexamined gets both
// missed_searches and coverage in the structured result and in the text the agent reads.
func TestMCPRecordReplyKeepsMissedSearchesAndCoverage(t *testing.T) {
	f := newMCPWikiFlow(t)
	prepared := f.prepare()
	f.query("juniper meadow")
	const area = "The totals were not read."
	saved := f.record(prepared, map[string]any{"changed": "Audited the export.", "checked": "Read the export code.", "evidence": []any{"new1"}, "remaining": area})
	if saved.Structured["complete"] != true {
		t.Fatalf("the record is complete: %.300s", saved.Text)
	}
	saved.holds(t, "missed_searches", "juniper meadow")
	saved.holds(t, "coverage", area)
	queries, _ := saved.Structured["missed_searches"].(map[string]any)["queries"].([]any)
	if len(queries) != 1 || queries[0] != "juniper meadow" {
		t.Fatalf("the structured result names the missed words as the record produced them: %v", saved.Structured["missed_searches"])
	}
}
