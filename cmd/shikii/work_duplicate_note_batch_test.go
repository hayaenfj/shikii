package main

import (
	"encoding/json"
	"runtime/debug"
	"strings"
	"testing"

	"github.com/hayaenfj/shikii/internal/notebook"
)

// Two notes in one batch that differ only in their sources share one required-note reference. After the first is published and its
// obligation is retired, placing the second must not dereference the retired obligation. The batch goes through the MCP tool call, as
// an agent sends it: that path has no recover, so a panic there ends the MCP server.
func TestWorkBatchWithTwoNotesDifferingOnlyInSourcesDoesNotPanic(t *testing.T) {
	repo, root, _ := planningFixture(t)
	b := notebook.Book{Root: root, Scope: feedbackScope(repo)}
	prep, err := workCall(t, b, "prepare", map[string]any{"query": "invoice routing", "request": "Record what the routing checks showed."})
	if err != nil {
		t.Fatal(err)
	}
	note := func(source string) map[string]any {
		return map[string]any{"title": "Invoice routing procedure", "lesson": "Check totals before routing invoices.", "applies_to": "invoice routing", "kind": "howto", "scene": "routing invoices", "sources": []string{source}}
	}
	input := map[string]any{"receipt": prep["receipt"],
		"observations": []any{
			map[string]any{"kind": "test", "title": "First routing check", "content": "The first synthetic routing check passed."},
			map[string]any{"kind": "test", "title": "Second routing check", "content": "The second synthetic routing check passed."},
		},
		"notes": []any{note("new1"), note("new2")},
	}
	var reply map[string]any
	func() {
		defer func() {
			if r := recover(); r != nil {
				// Name the frames of this package so the failing line is visible in the test output.
				frames := []string{}
				for _, line := range strings.Split(string(debug.Stack()), "\n") {
					if strings.Contains(line, "/cmd/shikii/work") {
						frames = append(frames, strings.TrimSpace(line))
					}
				}
				t.Fatalf("recording the batch panicked: %v\n%s", r, strings.Join(frames, "\n"))
			}
		}()
		params, _ := json.Marshal(map[string]any{"name": "shikii_wiki", "arguments": map[string]any{"operation": "record", "work": input}})
		answer := handleRPC(rpcRequest{JSONRPC: "2.0", ID: json.RawMessage("1"), Method: "tools/call", Params: params}, newServer(serveOpts{storeDir: root, projectScope: b.Scope}))
		result, _ := answer["result"].(map[string]any)
		if result == nil || result["isError"] != false {
			t.Fatalf("the tool call must answer with the recording result: %v", answer)
		}
		reply = loopObject(t, result["structuredContent"])
	}()
	t.Logf("complete=%v saved=%v pending=%v errors=%v", reply["complete"], reply["saved"], reply["pending"], reply["errors"])
	pages, err := b.Live()
	if err != nil || len(pages) != 1 {
		t.Fatalf("the first note must be published once: pages=%d err=%v", len(pages), err)
	}
	// The second note is either published or waits for its placement; it must be reported, not dropped.
	pending, _ := reply["pending"].([]any)
	notes := 0
	for _, raw := range reply["saved"].([]any) {
		if raw.(map[string]any)["kind"] == "note" {
			notes++
		}
	}
	if errs, _ := reply["errors"].([]any); notes+len(pending)+len(errs) < 2 {
		t.Fatalf("both notes must be accounted for: %v", reply)
	}
	if len(pending) == 0 {
		return
	}
	// A note left waiting for its placement stays an obligation of the turn and can still be resolved by its reference.
	if reasoning := loopObject(t, reply["reasoning"]); reasoning["wiki_pending"] != true || len(loopObject(t, reasoning["pending_notes"])) != 1 {
		t.Fatalf("the waiting note must keep its own obligation with the original draft: %v", reply["reasoning"])
	}
	candidate := pending[0].(map[string]any)["candidates"].([]any)[0].(map[string]any)["ref"]
	edit := map[string]any{"field": "lesson", "before": "Check totals before routing invoices.", "after": "Check totals and owners before routing invoices."}
	resolved, err := workCall(t, b, "record", map[string]any{"receipt": prep["receipt"], "notes": []any{map[string]any{"ref": pending[0].(map[string]any)["ref"], "target": candidate, "edits": []any{edit}}}})
	if err != nil || resolved["complete"] != true || loopObject(t, resolved["reasoning"])["wiki_pending"] != false {
		t.Fatalf("the waiting note must be resolvable by its reference: %v err=%v", resolved, err)
	}
	if pages, err = b.Live(); err != nil || len(pages) != 1 || pages[0].Lesson != "Check totals and owners before routing invoices." || len(pages[0].Sources) != 2 {
		t.Fatalf("resolving into the candidate keeps one article with both sources: %v err=%v", pages, err)
	}
}
