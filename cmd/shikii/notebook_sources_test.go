package main

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/notebook"
)

func TestNotebookCapturesObservedSourcesThroughHTTPAndCLI(t *testing.T) {
	repo := gitRepo(t)
	root := hookEnv(t, nil, repo)
	t.Chdir(repo)
	h := newServer(serveOpts{storeDir: root, projectScope: feedbackScope(repo)})
	request := `{"operation":"capture","observation":{"kind":"test","title":"Package smoke check","content":"go test ./...: PASS","reference":"go test ./...","version":"fixture-v1","observed_at":"2026-01-01T00:00:00Z"}}`
	r := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/wiki/notebook", strings.NewReader(request))
	req.Header.Set("Content-Type", "application/json")
	h.ServeHTTP(r, req)
	if r.Code != 200 {
		t.Fatalf("a check result can be saved with its kind, version and time: %d %s", r.Code, r.Body.String())
	}
	var saved map[string]any
	if err := json.Unmarshal(r.Body.Bytes(), &saved); err != nil {
		t.Fatal(err)
	}
	id := saved["id"].(string)
	got := notebookCommand(t, "query", id)
	if got["observations"].([]any)[0].(map[string]any)["kind"] != "test" {
		t.Fatal("keeps the kind of the source")
	}
	view, err := reflectQuery(root, feedbackScope(repo))
	data, _ := json.Marshal(view)
	if err != nil || !strings.Contains(string(data), id) {
		t.Fatal("passes observations to the usual reflection", err)
	}
	notebookCommand(t, "ingest", `{"sources":["`+id+`"],"title":"Smoke checks","lesson":"Run package smoke checks","kind":"rule","scene":"テストの時","applies_to":"Package maintenance"}`)
	got = notebookCommand(t, "query", "Smoke checks")
	if len(got["lessons"].([]any)) != 1 {
		t.Fatal("a check result can be the source of a lesson")
	}
	all := notebookCommand(t, "query", "")
	task := all["maintenance"].([]any)[0].(map[string]any)
	review := map[string]any{"page_id": task["page_id"], "revision": task["revision"], "key": task["key"], "reason": "Checked the captured smoke result against the article."}
	data, _ = json.Marshal(review)
	notebookCommand(t, "maintain", string(data))
	after, err := reflectQuery(root, feedbackScope(repo))
	if err != nil || len(after.Maintenance) != 0 {
		t.Fatal("saves the review result through the CLI and removes it from the reflection", err)
	}
	cli := notebookCommand(t, "capture", `{"kind":"code","title":"Source excerpt","content":"package fixture","reference":"example.go:1","version":"fixture-v1","observed_at":"2026-01-01T00:00:00Z"}`)
	if cli["kind"] != "code" {
		t.Fatal("the same save operation can be used from the CLI too")
	}
}

func TestNotebookContinuationAcrossCLIHTTPAndMCP(t *testing.T) {
	repo := gitRepo(t)
	root := hookEnv(t, nil, repo)
	t.Chdir(repo)
	scope := feedbackScope(repo)
	book := notebook.Book{Root: root, Scope: scope}
	ids := []string{}
	for i := range 12 {
		o, err := book.Capture(notebook.Observation{Kind: "test", Title: fmt.Sprint("Fixture ", i), Content: strings.Repeat("x", 9000), Reference: "fixture", Version: "v1", ObservedAt: time.Unix(1, 0)})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, o.ID)
	}
	args := append([]string{"query"}, ids...)
	first := capture(t, func() error { return cmdWiki(args) })
	var one struct {
		Next         string                 `json:"next_cursor"`
		Observations []notebook.Observation `json:"observations"`
	}
	if err := json.Unmarshal([]byte(first), &one); err != nil || one.Next == "" || len(one.Observations) >= 12 {
		t.Fatal("the CLI splits the result and returns a continuation", err)
	}
	cli := capture(t, func() error { return cmdWiki(append(args, "continue:"+one.Next)) })
	data, _ := json.Marshal(map[string]any{"operation": "query", "sources": ids, "cursor": one.Next})
	h := newServer(serveOpts{storeDir: root, projectScope: scope})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/wiki/notebook", strings.NewReader(string(data)))
	req.Header.Set("Content-Type", "application/json")
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatal("the same continuation can be read over HTTP", rec.Code, rec.Body.String())
	}
	var a, b any
	if json.Unmarshal([]byte(cli), &a) != nil || json.Unmarshal(rec.Body.Bytes(), &b) != nil {
		t.Fatal("the continuation is JSON")
	}
	ja, _ := json.Marshal(a)
	jb, _ := json.Marshal(b)
	if string(ja) != string(jb) {
		t.Fatal("CLI and HTTP return the same continuation")
	}
	send, _ := mcpSession(t, "true", map[string]float64{"change_request": .9, "question": .1, "other": 0}, root)
	m := result(t, send(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{"name": "shikii_wiki", "arguments": map[string]any{"operation": "query", "sources": ids, "cursor": one.Next}}}))
	if m["isError"] == true || m["structuredContent"] == nil {
		t.Fatal("MCP handles the continuation input too", m)
	}
	jm, _ := json.Marshal(m["structuredContent"])
	if string(jm) != string(ja) {
		t.Fatal("MCP returns the same sources too")
	}
	for i, id := range ids {
		body := strings.Repeat("Detailed evidence. ", 550)
		if _, err := book.Ingest(notebook.Draft{Title: fmt.Sprint("Topic ", i), Lesson: "Checked", Kind: "rule", Scene: "テストの時", AppliesTo: "Topic maintenance", Sources: []string{id}, Body: &body}); err != nil {
			t.Fatal(err)
		}
	}
	seen, cursor := map[string]bool{}, ""
	for calls := 0; ; calls++ {
		if calls > len(ids) {
			t.Fatal("the continuation of the page's list does not move forward")
		}
		r := httptest.NewRecorder()
		h.ServeHTTP(r, httptest.NewRequest("GET", "/v1/wiki?cursor="+cursor, nil))
		if r.Code != 200 {
			t.Fatal("fetching the page's continuation", r.Body.String())
		}
		var page struct {
			Lessons []notebook.Page `json:"lessons"`
			Next    string          `json:"next_cursor"`
		}
		if err := json.Unmarshal(r.Body.Bytes(), &page); err != nil {
			t.Fatal(err)
		}
		for _, p := range page.Lessons {
			if seen[p.ID] {
				t.Fatal("a page appeared twice in the page's list")
			}
			seen[p.ID] = true
		}
		if page.Next == "" {
			break
		}
		cursor = page.Next
	}
	if len(seen) != len(ids) {
		t.Fatal("the page reaches every page too", len(seen))
	}
}

func TestNotebookCLIReadsSourcesTogether(t *testing.T) {
	repo := gitRepo(t)
	root := hookEnv(t, nil, repo)
	t.Chdir(repo)
	book := notebook.Book{Root: root, Scope: feedbackScope(repo)}
	ids := []string{}
	for _, content := range []string{"返品の承認済み規定", "送料を除くという確認結果"} {
		source, err := book.Capture(notebook.Observation{Kind: "test", Title: content, Content: content, Reference: "fixture", Version: "v1", ObservedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, source.ID)
	}
	text := capture(t, func() error { return cmdWiki([]string{"query", ids[0], ids[1], ids[0]}) })
	var got struct {
		Observations []notebook.Observation `json:"observations"`
	}
	if err := json.Unmarshal([]byte(text), &got); err != nil || len(got.Observations) != 2 {
		t.Fatalf("one CLI call reads the sources with duplicates removed: %s %v", text, err)
	}
	seen := map[string]bool{}
	for _, source := range got.Observations {
		if (source.ID != ids[0] && source.ID != ids[1]) || seen[source.ID] || source.Content == "" || source.Version != "v1" {
			t.Fatalf("returns each given raw text and version once: %+v", source)
		}
		seen[source.ID] = true
	}
	// Search words are split as before. Do not confuse them with arguments that are only IDs.
	body := "送料を除いた支払額を返す。"
	if _, err := book.Ingest(notebook.Draft{Sources: ids, Title: "返品 送料", Lesson: body, Kind: "rule", Scene: "テストの時", AppliesTo: "返品の対応", Body: &body}); err != nil {
		t.Fatal(err)
	}
	text = capture(t, func() error { return cmdWiki([]string{"query", "返品", "送料"}) })
	if !strings.Contains(text, `"lessons":[{`) {
		t.Fatalf("several search words keep searching: %s", text)
	}
	for _, args := range [][]string{
		{"query", ids[0], "0000000000000000"},
	} {
		if err := cmdWiki(args); err == nil {
			t.Fatal("sources of another project are not let through to the search")
		}
	}
	// Reading several at once still detects tampering with the cited source.
	paths, err := filepath.Glob(filepath.Join(root, "records", "wiki-sources", "*", ids[0]+".json"))
	if err != nil || len(paths) != 1 {
		t.Fatal("cannot find the saved source", err)
	}
	data, err := os.ReadFile(paths[0])
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths[0], append(data, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	if err := cmdWiki([]string{"query", ids[0], ids[1]}); err == nil {
		t.Fatal("reading several at once does not skip the hash check of the cited source")
	}
}
