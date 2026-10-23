package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	shikii "github.com/hayaenfj/shikii"
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
		t.Fatalf("wiki の結果は JSON: %v %s", err, out)
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
	if err := json.Unmarshal(manifest, &plugin); err != nil || plugin.Version != shikii.Version || shikii.Version != "0.7.5" {
		t.Fatalf("CLI・MCP・plugin の版を揃える: %s %+v %v", shikii.Version, plugin, err)
	}
	old := os.Args
	os.Args = []string{"shikii", "--version"}
	out := capture(t, func() error { main(); return nil })
	os.Args = old
	if strings.TrimSpace(out) != "shikii 0.7.5" {
		t.Fatalf("版を確認できる: %s", out)
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
		t.Fatal("MCP の起動時にも同じ版を返す")
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
			t.Fatal("wikiとreflectの道具から本文・関連を渡せる", tool["name"])
		}
	}
	for _, args := range []map[string]any{
		{"operation": "query"},
		{"operation": "ingest", "sources": []string{id}, "title": "モデルの起動", "lesson": "モデルの起動手順まで確認する", "applies_to": "README の導入手順", "body": "## 背景\n\nARTICLE_ONLY_MARKER", "related": []string{}},
		{"operation": "query", "sources": []string{id}},
		{"operation": "lint"},
	} {
		got := result(t, send(map[string]any{"jsonrpc": "2.0", "id": 2, "method": "tools/call", "params": map[string]any{"name": "shikii_wiki", "arguments": args}}))
		if got["isError"] == true || got["structuredContent"] == nil {
			t.Fatalf("wiki の一周を MCP から呼べる: %v", got)
		}
	}
	material := notebookCommand(t, "query", id)
	if len(material["source_material"].([]any)) != 1 {
		t.Fatal("CLI からも出典の原文を確認できる")
	}
	h := newServer(serveOpts{storeDir: agent, projectScope: feedbackScope(repo)})
	recorder := httptest.NewRecorder()
	h.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/v1/wiki", nil))
	var wiki map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &wiki); err != nil || len(wiki["lessons"].([]any)) != 1 || wiki["notebook_project"] != filepath.Base(repo) {
		t.Fatalf("画面にも同じ仕事の教訓が届く: err=%v", err)
	}
	if wiki["lessons"].([]any)[0].(map[string]any)["body"] != "## 背景\n\nARTICLE_ONLY_MARKER" {
		t.Fatal("MCPで登録した本文が画面にも届く")
	}
	quote, err := memoryContext(agent, feedbackScope(repo), "README の導入手順", &hookSession{})
	if err != nil || !strings.Contains(quote, "モデルの起動手順") || strings.Contains(quote, "ARTICLE_ONLY_MARKER") {
		t.Fatal("自動配送では本文でなく要約を引用する", err)
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
		`{"operation":"ingest","sources":["../../outside"],"title":"bad","lesson":"bad","applies_to":"bad"}`,
		`{"operation":"ingest","sources":["0000000000000001"],"status":"confirmed"}`,
		`{"operation":"query","project":"/other/project"}`,
		`{"operation":"query"} {"operation":"ingest"}`,
	} {
		code, _, err := callTool(h, "shikii_wiki", json.RawMessage(body))
		if err != nil || code < 400 {
			t.Fatalf("仕事や信頼の状態を入力で偽装できない: code=%d err=%v", code, err)
		}
	}
	for _, origin := range []string{"https://other.example", "http://localhost.other.example"} {
		req := httptest.NewRequest(http.MethodPost, "/v1/wiki/notebook", strings.NewReader(`{"operation":"query"}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", origin)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != 403 {
			t.Fatal("別サイトから wiki を操作させない")
		}
	}
	old := stdin
	defer func() { stdin = old }()
	for _, body := range []string{`{"sources":[],"status":"confirmed"}`, `{"sources":[]}` + strings.Repeat(" ", 64<<10)} {
		stdin = strings.NewReader(body)
		if err := cmdWiki([]string{"ingest"}); err == nil {
			t.Fatal("CLI でも未知の項目と大きすぎる入力は受け取らない")
		}
	}
}

// 普段の会話 → エージェントによる取り込み → 出典付きの教訓 → 次の依頼、を
// 画面操作や新しいモデルなしで一周する。原文と既存の wiki は上書きしない。
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
		t.Fatalf("未整理の会話を取り出せる: %v", q)
	}
	before, err := os.ReadFile(filepath.Join(agent, "wiki", "index.md"))
	if err != nil {
		t.Fatal(err)
	}
	ingested := notebookCommand(t, "ingest", `{"sources":["`+sourceID+`"],"title":"インストール手順の確認","lesson":"インストール手順には、モデルを起動する方法まで含めて確認する。","applies_to":"README の導入手順を書く依頼"}`)
	page := ingested["page"].(map[string]any)
	if page["status"] != "hypothesis" || len(page["sources"].([]any)) != 1 {
		t.Fatalf("教訓は出典付きの仮説: %v", page)
	}
	q = notebookCommand(t, "query", "モデルを起動する方法を説明して")
	if len(q["lessons"].([]any)) != 1 || len(q["pending_sources"].([]any)) != 0 {
		t.Fatalf("整理した教訓を検索して使える: %v", q)
	}
	ctx, err := memoryContext(agent, feedbackScope(repo), "README の導入手順を書く依頼", &hookSession{})
	if err != nil || !strings.Contains(ctx, "モデルを起動する方法まで含めて確認する") || !strings.Contains(ctx, sourceID) {
		t.Fatalf("依頼の前に教訓と出典が届く: %s %v", ctx, err)
	}
	lint := notebookCommand(t, "lint", "")
	if len(lint["issues"].([]any)) != 0 {
		t.Fatalf("出典と索引が整っている: %v", lint)
	}
	indexes, _ := filepath.Glob(filepath.Join(agent, "wiki", "lessons", "*", "index.md"))
	logs, _ := filepath.Glob(filepath.Join(agent, "wiki", "lessons", "*", "log.md"))
	if len(indexes) != 1 || len(logs) != 1 {
		t.Fatalf("索引と変更履歴を自動で作る: indexes=%v logs=%v", indexes, logs)
	}
	after, _ := os.ReadFile(filepath.Join(agent, "wiki", "index.md"))
	if string(before) != string(after) {
		t.Fatal("既存の聞き方の wiki を上書きしない")
	}
	if _, labels, err := st.Counts("done-as-asked"); err != nil || len(labels) != 0 {
		t.Fatalf("まとめた教訓を人の正解に変えない: %v %v", labels, err)
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
		t.Fatalf("一会話だけなら整理をまとめるまで待つ: %s", ctx)
	}
	// 通知では整理の仕事を増やさない。
	if out := runHook(t, "user-prompt-submit", map[string]any{"session_id": "wiki", "cwd": repo, "prompt": "<task-notification>done</task-notification>"}); out != "" {
		t.Fatalf("通知には wiki の作業を差し込まない: %s", out)
	}
	// 追加依頼で教訓が無ければ、原文を残して整理済みにできる。
	view := notebookCommand(t, "query", "")
	id := view["pending_sources"].([]any)[0].(map[string]any)["id"].(string)
	notebookCommand(t, "ingest", `{"sources":["`+id+`"]}`)
	view = notebookCommand(t, "query", "")
	if len(view["pending_sources"].([]any)) != 0 || len(view["lessons"].([]any)) != 0 {
		t.Fatalf("全ての発言から教訓を捏造する必要はない: %v", view)
	}
	ctx, _, err := reflectContext(agent, feedbackScope(repo))
	if err != nil || ctx != "" {
		t.Fatalf("整理済みの会話を繰り返し渡さない: %s %v", ctx, err)
	}
	// JSON のエスケープで長くなる原文も、引用後の上限を守る。
	for _, id := range []string{"0000000000000002", "0000000000000003"} {
		if err := (store.Store{Dir: agent}).AppendFeedback(feedbackScope(repo), store.Feedback{ID: id, Request: "README を更新して", Summary: strings.Repeat("\x01", 1500), Reply: strings.Repeat("\x02", 1500)}); err != nil {
			t.Fatal(err)
		}
	}
	ctx, _, err = reflectContext(agent, feedbackScope(repo))
	if err != nil || ctx == "" || len(ctx) > 1800 || !utf8.ValidString(ctx) {
		t.Fatalf("wiki の引用も原文を重ねず1800バイト以内: bytes=%d err=%v", len(ctx), err)
	}
	parts := strings.SplitN(ctx, "\n", 2)
	var quoted map[string]any
	if len(parts) != 2 || json.Unmarshal([]byte(parts[1]), &quoted) != nil || len(quoted["sources"].([]any)) != 2 {
		t.Fatal("切り詰めても引用データと指示の境界を保つ")
	}
}
