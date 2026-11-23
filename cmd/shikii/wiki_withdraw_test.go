package main

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/notebook"
)

// shikii wiki withdraw takes an article the caller made out of delivery, from the command line only: HTTP and MCP do not run it. The
// article stays as a withdrawn one; restoring or deleting it is the person's, on the Knowledge page, and no operation of the command
// line or MCP does it.
func TestWikiWithdrawRunsOnlyFromTheCommandLine(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("HOME", filepath.Join(dir, "home"))
	capture(t, func() error { return cmdInit(nil) })
	in, _ := json.Marshal(notebook.Observation{Kind: "agent", Title: "a task reached its goal", Content: "the report", Reference: "chiten:t3/run1", Version: "chiten", ObservedAt: time.Now()})
	var out string
	withStdin(t, string(in), func() { out = capture(t, func() error { return cmdNotebook([]string{"capture"}) }) })
	var src notebook.Observation
	if err := json.Unmarshal([]byte(out), &src); err != nil || src.ID == "" {
		t.Fatalf("capture: %v %s", err, out)
	}
	draft, _ := json.Marshal(map[string]any{"sources": []string{src.ID}, "title": "a task reached its goal", "lesson": "a task reached its goal", "applies_to": "chiten \u306e\u4ed5\u4e8b\u3092\u5b9f\u884c\u3059\u308b\u3068\u304d", "kind": "history", "scene": "chiten \u306e\u30bf\u30b9\u30af\u304c\u5230\u9054\u3057\u305f\u3068\u304d", "body": "the report"})
	withStdin(t, string(draft), func() { out = capture(t, func() error { return cmdNotebook([]string{"ingest"}) }) })
	var ingested struct {
		Page struct {
			ID, Revision string
		} `json:"page"`
	}
	if err := json.Unmarshal([]byte(out), &ingested); err != nil || ingested.Page.ID == "" {
		t.Fatalf("ingest: %v %s", err, out)
	}
	w, _ := json.Marshal(notebook.Withdrawal{ID: ingested.Page.ID, Revision: ingested.Page.Revision, Sources: []string{src.ID}, Reason: "the request was deleted"})

	h := newServer(serveOpts{storeDir: filepath.Join(dir, ".shikii")})
	_, body, err := callTool(h, "shikii_wiki", json.RawMessage(`{"operation":"withdraw","id":"`+ingested.Page.ID+`"}`))
	if err == nil && !strings.Contains(string(body), "usage: wiki") {
		t.Fatalf("HTTP runs withdraw: %s", body)
	}

	withStdin(t, string(w), func() { out = capture(t, func() error { return cmdNotebook([]string{"withdraw"}) }) })
	var got notebook.Page
	if err := json.Unmarshal([]byte(out), &got); err != nil || got.Status != "withdrawn" {
		t.Fatalf("withdraw: %v %s", err, out)
	}
	root, scope, err := currentProjectStore()
	if err != nil {
		t.Fatal(err)
	}
	if live, err := (notebook.Book{Root: root, Scope: scope}).Live(); err != nil || len(live) != 0 {
		t.Fatalf("the withdrawn article is not delivered: %v %v", live, err)
	}
	// What an agent can reach does not bring it back or delete it: no operation of the command line or MCP restores or deletes an article,
	// and a patch and a lesson saved under its title leave it withdrawn.
	for _, op := range []string{"restore", "delete", "manage", "article", "unwithdraw"} {
		if err := cmdNotebook([]string{op}); err == nil {
			t.Fatalf("the command line has an operation %q", op)
		}
		if _, body, err := callTool(h, "shikii_wiki", json.RawMessage(`{"operation":"`+op+`","id":"`+ingested.Page.ID+`"}`)); err == nil && !strings.Contains(string(body), "usage: wiki") {
			t.Fatalf("MCP runs %q: %s", op, body)
		}
	}
	patch, _ := json.Marshal(map[string]any{"id": got.ID, "revision": got.Revision, "sources": []string{src.ID}, "change_reason": "fix", "edits": []map[string]string{{"field": "lesson", "before": got.Lesson, "after": "changed"}}})
	withStdin(t, string(patch), func() { _, _ = captureErr(func() error { return cmdNotebook([]string{"patch"}) }) })
	withStdin(t, string(draft), func() { _, _ = captureErr(func() error { return cmdNotebook([]string{"ingest"}) }) })
	view, err := (notebook.Book{Root: root, Scope: scope}).Query("lesson:" + got.ID)
	if err != nil || len(view.Lessons) != 1 || view.Lessons[0].Status != "withdrawn" || view.Lessons[0].Revision != got.Revision {
		t.Fatalf("nothing an agent can run changes the withdrawn article: %+v %v", view.Lessons, err)
	}
}
