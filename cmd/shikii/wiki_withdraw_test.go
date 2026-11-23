package main

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/notebook"
)

// shikii wiki withdraw deletes an article the caller made, from the command line only: HTTP and MCP do not run it.
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
	if err := json.Unmarshal([]byte(out), &got); err != nil || got.Status != "deleted" {
		t.Fatalf("withdraw: %v %s", err, out)
	}
	root, scope, err := currentProjectStore()
	if err != nil {
		t.Fatal(err)
	}
	if live, err := (notebook.Book{Root: root, Scope: scope}).Live(); err != nil || len(live) != 0 {
		t.Fatalf("the withdrawn article is not delivered: %v %v", live, err)
	}
}
