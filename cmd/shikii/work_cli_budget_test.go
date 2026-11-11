package main

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
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
	// a task too long for the bound is refused with the bound and how to ask for more, so one more call is enough
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
