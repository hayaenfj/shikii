package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/hayaenfj/shikii"
	"github.com/hayaenfj/shikii/internal/notebook"
	"github.com/hayaenfj/shikii/internal/store"
)

func notebookCommand(t *testing.T, operation, input string) map[string]any {
	t.Helper()
	old := stdin
	stdin = strings.NewReader(input)
	defer func() { stdin = old }()
	args := []string{operation}
	if operation == "query" && input != "" {
		args = append(args, input)
	}
	out := capture(t, func() error { return cmdWiki(args) })
	var result map[string]any
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("the wiki result is JSON: %v %s", err, out)
	}
	return result
}

func TestNotebookVersionAndMCPContract(t *testing.T) {
	working, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := os.ReadFile(filepath.Join(working, "..", "..", ".claude-plugin", "plugin.json"))
	if err != nil {
		t.Fatal(err)
	}
	var plugin struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(manifest, &plugin); err != nil || plugin.Version != shikii.Version || shikii.Version != "0.14.0" {
		t.Fatalf("the CLI, MCP and plugin versions match: %s %+v %v", shikii.Version, plugin, err)
	}
	old := os.Args
	os.Args = []string{"shikii", "--version"}
	out := capture(t, func() error { main(); return nil })
	os.Args = old
	if strings.TrimSpace(out) != "shikii 0.14.0" {
		t.Fatalf("the version can be checked: %s", out)
	}
	repo := gitRepo(t)
	agent := hookEnv(t, nil, repo)
	t.Chdir(repo)
	const id = "0000000000000011"
	if err := (store.Store{Dir: agent}).AppendFeedback(feedbackScope(repo), store.Feedback{ID: id, Request: "README install", Summary: "done", Reply: "missing startup"}); err != nil {
		t.Fatal(err)
	}
	send, _ := mcpSession(t, "true", map[string]float64{"change_request": 0.9, "question": 0.1, "other": 0}, agent)
	init := result(t, send(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize"}))
	if init["serverInfo"].(map[string]any)["version"] != shikii.Version {
		t.Fatal("MCP returns the same version at startup too")
	}
	listed := result(t, send(map[string]any{"jsonrpc": "2.0", "id": 2, "method": "tools/list"}))
	for _, item := range listed["tools"].([]any) {
		tool := item.(map[string]any)
		if tool["name"] != "shikii_wiki" && tool["name"] != "shikii_reflect" {
			continue
		}
		properties := tool["inputSchema"].(map[string]any)["properties"].(map[string]any)
		if tool["name"] == "shikii_reflect" {
			properties = properties["wiki"].(map[string]any)["items"].(map[string]any)["properties"].(map[string]any)
		}
		if properties["body"] == nil || properties["related"] == nil {
			t.Fatal("the wiki and reflect tools can take a body and related pages", tool["name"])
		}
	}
	for _, args := range []map[string]any{
		{"operation": "query"},
		{"operation": "ingest", "sources": []string{id}, "title": "モデルの起動", "lesson": "モデルの起動手順まで確認する", "kind": "rule", "scene": "テストの時", "applies_to": "README の導入手順", "body": "## 背景\n\nARTICLE_ONLY_MARKER", "related": []string{}},
		{"operation": "query", "sources": []string{id}},
		{"operation": "lint"},
	} {
		got := result(t, send(map[string]any{"jsonrpc": "2.0", "id": 2, "method": "tools/call", "params": map[string]any{"name": "shikii_wiki", "arguments": args}}))
		if got["isError"] == true || got["structuredContent"] == nil {
			t.Fatalf("the whole wiki cycle can be called from MCP: %v", got)
		}
	}
	material := notebookCommand(t, "query", id)
	if len(material["source_material"].([]any)) != 1 {
		t.Fatal("the raw text of the source can be checked from the CLI too")
	}
	h := newServer(serveOpts{storeDir: agent, projectScope: feedbackScope(repo)})
	recorder := httptest.NewRecorder()
	h.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/v1/wiki", nil))
	var wiki map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &wiki); err != nil || len(wiki["lessons"].([]any)) != 1 || wiki["notebook_project"] != filepath.Base(repo) {
		t.Fatalf("the page also gets the same project's lessons: err=%v", err)
	}
	if wiki["lessons"].([]any)[0].(map[string]any)["body"] != "## 背景\n\nARTICLE_ONLY_MARKER" {
		t.Fatal("the body registered through MCP reaches the page too")
	}
	quote, err := memoryContext(agent, feedbackScope(repo), "README の導入手順", &hookSession{})
	if err != nil || !strings.Contains(quote, "モデルの起動手順") || strings.Contains(quote, "ARTICLE_ONLY_MARKER") {
		t.Fatal("automatic delivery quotes the summary, not the body", err)
	}
}

func TestNotebookTransportRejectsUnauthorizedFieldsAndBrowserPosts(t *testing.T) {
	repo := gitRepo(t)
	agent := hookEnv(t, nil, repo)
	t.Chdir(repo)
	h := newServer(serveOpts{storeDir: agent, projectScope: feedbackScope(repo)})
	if err := (store.Store{Dir: agent}).AppendFeedback(feedbackScope(repo), store.Feedback{ID: "0000000000000001", Request: "README install", Summary: "done", Reply: "missing startup"}); err != nil {
		t.Fatal(err)
	}
	for _, body := range []string{
		`{"operation":"ingest","sources":["../../outside"],"title":"bad","lesson":"bad","kind":"rule","scene":"テストの時","applies_to":"bad"}`,
		`{"operation":"ingest","sources":["0000000000000001"],"status":"confirmed"}`,
		`{"operation":"query","project":"/other/project"}`,
		`{"operation":"query"} {"operation":"ingest"}`,
	} {
		code, _, err := callTool(h, "shikii_wiki", json.RawMessage(body))
		if err != nil || code < 400 {
			t.Fatalf("the project or trust state cannot be forged from the input: code=%d err=%v", code, err)
		}
	}
	for _, origin := range []string{"https://other.example", "http://localhost.other.example"} {
		req := httptest.NewRequest(http.MethodPost, "/v1/wiki/notebook", strings.NewReader(`{"operation":"query"}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", origin)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != 403 {
			t.Fatal("another site cannot operate the wiki")
		}
	}
	old := stdin
	defer func() { stdin = old }()
	for _, body := range []string{`{"sources":[],"status":"confirmed"}`, `{"sources":[]}` + strings.Repeat(" ", 64<<10)} {
		stdin = strings.NewReader(body)
		if err := cmdWiki([]string{"ingest"}); err == nil {
			t.Fatal("the CLI also refuses unknown fields and inputs that are too large")
		}
	}
}

// Go around once: everyday conversation → ingest by the agent → lesson with sources → next request,
// without page operations or a new model. The raw text and the existing wiki are not overwritten.
func TestNotebookConversationIngestQueryLintLoop(t *testing.T) {
	repo := gitRepo(t)
	agent := hookEnv(t, nil, repo)
	t.Chdir(repo)
	const sourceID = "0000000000000001"
	st := store.Store{Dir: agent}
	if err := st.AppendFeedback(feedbackScope(repo), store.Feedback{
		ID: sourceID, Request: "README のインストール手順を分かりやすく書き直して",
		Summary: "手順を直しました", Reply: "まだ終わっていない。モデルを起動する手順も必要です",
	}); err != nil {
		t.Fatal(err)
	}
	q := notebookCommand(t, "query", "")
	if len(q["pending_sources"].([]any)) != 1 {
		t.Fatalf("unsorted conversations can be taken out: %v", q)
	}
	before, err := os.ReadFile(filepath.Join(agent, "wiki", "index.md"))
	if err != nil {
		t.Fatal(err)
	}
	ingested := notebookCommand(t, "ingest", `{"sources":["`+sourceID+`"],"title":"インストール手順の確認","lesson":"インストール手順には、モデルを起動する方法まで含めて確認する。","kind":"rule","scene":"テストの時","applies_to":"README の導入手順を書く依頼"}`)
	page := ingested["page"].(map[string]any)
	if page["status"] != "hypothesis" || len(page["sources"].([]any)) != 1 {
		t.Fatalf("a lesson is a hypothesis with sources: %v", page)
	}
	q = notebookCommand(t, "query", "モデルを起動する方法を説明して")
	if len(q["lessons"].([]any)) != 1 || len(q["pending_sources"].([]any)) != 0 {
		t.Fatalf("sorted lessons can be searched and used: %v", q)
	}
	ctx, err := memoryContext(agent, feedbackScope(repo), "README の導入手順を書く依頼", &hookSession{})
	if err != nil || !strings.Contains(ctx, "モデルを起動する方法まで含めて確認する") || !strings.Contains(ctx, sourceID) {
		t.Fatalf("the lesson and its source arrive before the request: %s %v", ctx, err)
	}
	lint := notebookCommand(t, "lint", "")
	if len(lint["issues"].([]any)) != 0 {
		t.Fatalf("sources and indexes are in order: %v", lint)
	}
	indexes, _ := filepath.Glob(filepath.Join(agent, "wiki", "lessons", "*", "index.md"))
	logs, _ := filepath.Glob(filepath.Join(agent, "wiki", "lessons", "*", "log.md"))
	if len(indexes) != 1 || len(logs) != 1 {
		t.Fatalf("the index and change log are made automatically: indexes=%v logs=%v", indexes, logs)
	}
	after, _ := os.ReadFile(filepath.Join(agent, "wiki", "index.md"))
	if string(before) != string(after) {
		t.Fatal("the existing wording wiki is not overwritten")
	}
	if _, labels, err := st.Counts("done-as-asked"); err != nil || len(labels) != 0 {
		t.Fatalf("summarized lessons do not become a person's truth: %v %v", labels, err)
	}
}

func TestNotebookHookOffersBoundedAutomaticIngest(t *testing.T) {
	model := fakeAgentModel(t, func(string) float64 { return 0.1 })
	repo := gitRepo(t)
	agent := hookEnv(t, model, repo)
	t.Chdir(repo)
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "wiki", "cwd": repo, "prompt": "README のインストール手順を更新して"})
	runHook(t, "stop", map[string]any{"session_id": "wiki", "cwd": repo, "last_assistant_message": "更新しました"})
	ctx := feedbackContext(t, runHook(t, "user-prompt-submit", map[string]any{"session_id": "wiki", "cwd": repo, "prompt": "まだ終わっていない。モデルを起動する手順も書いて"}))
	if strings.Contains(ctx, "shikii_reflect") {
		t.Fatalf("with only one conversation, it waits before asking for sorting: %s", ctx)
	}
	// Notifications do not add sorting work.
	if out := runHook(t, "user-prompt-submit", map[string]any{"session_id": "wiki", "cwd": repo, "prompt": "<task-notification>done</task-notification>"}); out != "" {
		t.Fatalf("wiki work is not inserted into a notification: %s", out)
	}
	// When a follow-up request yields no lesson, the raw text is kept and it can be marked as sorted.
	view := notebookCommand(t, "query", "")
	id := view["pending_sources"].([]any)[0].(map[string]any)["id"].(string)
	notebookCommand(t, "ingest", `{"sources":["`+id+`"]}`)
	view = notebookCommand(t, "query", "")
	if len(view["pending_sources"].([]any)) != 0 || len(view["lessons"].([]any)) != 0 {
		t.Fatalf("there is no need to invent a lesson from every message: %v", view)
	}
	ctx, _, err := reflectContext(agent, feedbackScope(repo))
	if err != nil || ctx != "" {
		t.Fatalf("sorted conversations are not passed again and again: %s %v", ctx, err)
	}
	// Raw text that grows with JSON escaping still keeps the limit after quoting.
	for _, id := range []string{"0000000000000002", "0000000000000003"} {
		if err := (store.Store{Dir: agent}).AppendFeedback(feedbackScope(repo), store.Feedback{ID: id, Request: "README を更新して", Summary: strings.Repeat("\x01", 1500), Reply: strings.Repeat("\x02", 1500)}); err != nil {
			t.Fatal(err)
		}
	}
	ctx, _, err = reflectContext(agent, feedbackScope(repo))
	// The size of the text asking for reflection is not limited (0.9.1; every added ritual sentence turned into a fight over space). What is kept is passing only source IDs, without repeating the conversation's raw text
	if err != nil || ctx == "" || strings.Contains(ctx, strings.Repeat("\x01", 100)) || strings.Contains(ctx, strings.Repeat("\x02", 100)) || !utf8.ValidString(ctx) {
		t.Fatalf("the wiki quote does not repeat the conversation's raw text either: bytes=%d err=%v", len(ctx), err)
	}
	parts := strings.SplitN(ctx, "\n", 2)
	var quoted map[string]any
	if len(parts) != 2 || json.Unmarshal([]byte(parts[1]), &quoted) != nil || len(quoted["sources"].([]any)) != 2 {
		t.Fatal("even when cut, the boundary between quoted data and instructions is kept")
	}
}

// 3. wiki ingest (CLI) hides the JSON error: Claude sent applies_to as an array and a lesson object and got the same
// generic line 3 times. capture and maintain already pass the decoder's reason on.
func TestIngestErrorSaysWhatIsWrong(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("HOME", filepath.Join(dir, "home"))
	capture(t, func() error { return cmdInit(nil) })
	for input, want := range map[string]string{
		`{"sources":["x"],"title":"t","lesson":"l","applies_to":["a"]}`:            "cannot unmarshal array",  // applies_to must be a string
		`{"sources":["x"],"title":"t","lesson":{"kind":"howto"},"applies_to":"a"}`: "cannot unmarshal object", // lesson must be a string
		`{"title":"t","lesson":"l","applies_to":"a"}`:                              "sources",                 // MCP said "needs source ids"; Claude tried source_ids
	} {
		var err error
		withStdin(t, input, func() { err = cmdNotebook([]string{"ingest"}) })
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("the ingest error must say %q: %v (input %s)", want, err, input)
		}
	}
}

// Behaviour check 0.9.2 (Claude Code): a wrong wiki capture input was answered with ingest's error and no field name,
// and the agent gave up keeping the whole log. The errors now name capture and say what is wrong.
func TestCaptureErrorsSayWhatIsWrong(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("HOME", filepath.Join(dir, "home"))
	capture(t, func() error { return cmdInit(nil) })
	for input, want := range map[string][]string{
		`{"kind":"test","title":"t","content":"c","reference":"r","observed_at":"2026-10-26T00:00:00Z"}`:                 {"capture", "version"},
		`{"kind":"result","title":"t","content":"c","reference":"r","version":"v","observed_at":"2026-10-26T00:00:00Z"}`: {"capture", "kind"},
		`{"kind":"test","title":"t","body":"c"}`: {"capture", "body"},
	} {
		var err error
		withStdin(t, input, func() { err = cmdNotebook([]string{"capture"}) })
		for _, w := range want {
			if err == nil || !strings.Contains(err.Error(), w) {
				t.Fatalf("the capture error says %q: %v (input %s)", w, err, input)
			}
		}
		if strings.Contains(err.Error(), "ingest") {
			t.Fatalf("a capture error does not speak of ingest: %v", err)
		}
	}
}

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

// A source record has no size limit of its own: a long memory file is captured whole (not split by its sections),
// read back, and listed for sorting. Only the store's line limit (store.MaxLineBytes) still applies to the record.
func TestLongSourceIsCapturedWhole(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("HOME", filepath.Join(dir, "home"))
	capture(t, func() error { return cmdInit(nil) })
	content := strings.Repeat("A long memory file, kept verbatim. ", 6000) + "END-OF-MEMORY" // about 210 KiB
	in, _ := json.Marshal(notebook.Observation{Kind: "agent", Title: "long memory", Content: content, Reference: "memory/long.md", Version: "2026-10-26", ObservedAt: time.Now()})
	var out string
	withStdin(t, string(in), func() { out = capture(t, func() error { return cmdNotebook([]string{"capture"}) }) })
	var saved notebook.Observation
	if err := json.Unmarshal([]byte(out), &saved); err != nil || saved.ID == "" {
		t.Fatalf("the CLI must capture a long source whole: %v\n%.300s", err, out)
	}
	root, scope, err := currentProjectStore()
	if err != nil {
		t.Fatal(err)
	}
	book := notebook.Book{Root: root, Scope: scope}
	got, err := book.ReadSources([]string{saved.ID})
	obs, _ := got["observations"].([]notebook.Observation)
	if err != nil || len(obs) != 1 || obs[0].Content != content {
		t.Fatalf("the long source must be read back whole: %v", err)
	}
	pending, err := book.PendingObservations()
	if err != nil || len(pending) != 1 || !strings.HasSuffix(pending[0].Content, "END-OF-MEMORY") {
		t.Fatalf("the long source must be listed for sorting: %v", err)
	}
	huge := notebook.Observation{Kind: "agent", Title: "huge", Content: strings.Repeat("x", store.MaxLineBytes), Reference: "memory/huge.md", Version: "v", ObservedAt: time.Now()}
	if _, err := book.Capture(huge); err == nil || !strings.Contains(err.Error(), "MiB") {
		t.Fatalf("a record over the store's line limit is still refused, saying the limit: %v", err)
	}
}
