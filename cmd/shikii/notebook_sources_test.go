package main

import (
	"encoding/json"
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
		t.Fatalf("確認結果を種類・版・時点付きで保存できる: %d %s", r.Code, r.Body.String())
	}
	var saved map[string]any
	if err := json.Unmarshal(r.Body.Bytes(), &saved); err != nil {
		t.Fatal(err)
	}
	id := saved["id"].(string)
	got := notebookCommand(t, "query", id)
	if got["observations"].([]any)[0].(map[string]any)["kind"] != "test" {
		t.Fatal("原資料の種類を保持する")
	}
	view, err := reflectQuery(root, feedbackScope(repo))
	data, _ := json.Marshal(view)
	if err != nil || !strings.Contains(string(data), id) {
		t.Fatal("普段の振り返りに観察を渡す", err)
	}
	notebookCommand(t, "ingest", `{"sources":["`+id+`"],"title":"Smoke checks","lesson":"Run package smoke checks","applies_to":"Package maintenance"}`)
	got = notebookCommand(t, "query", "Smoke checks")
	if len(got["lessons"].([]any)) != 1 {
		t.Fatal("確認結果を教訓の出典にできる")
	}
	all := notebookCommand(t, "query", "")
	task := all["maintenance"].([]any)[0].(map[string]any)
	review := map[string]any{"page_id": task["page_id"], "revision": task["revision"], "key": task["key"], "reason": "Checked the captured smoke result against the article."}
	data, _ = json.Marshal(review)
	notebookCommand(t, "maintain", string(data))
	after, err := reflectQuery(root, feedbackScope(repo))
	if err != nil || len(after.Maintenance) != 0 {
		t.Fatal("見直し結果をCLIで保存し振り返りから除く", err)
	}
	cli := notebookCommand(t, "capture", `{"kind":"code","title":"Source excerpt","content":"package fixture","reference":"example.go:1","version":"fixture-v1","observed_at":"2026-01-01T00:00:00Z"}`)
	if cli["kind"] != "code" {
		t.Fatal("CLIからも同じ保存操作を使える")
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
		t.Fatalf("一度のCLI呼出しで重複を除いて原資料を読める: %s %v", text, err)
	}
	seen := map[string]bool{}
	for _, source := range got.Observations {
		if (source.ID != ids[0] && source.ID != ids[1]) || seen[source.ID] || source.Content == "" || source.Version != "v1" {
			t.Fatalf("指定した原文・版をそれぞれ一度返す: %+v", source)
		}
		seen[source.ID] = true
	}
	// 検索語の区切りは従来どおり。IDだけの引数と混同しない。
	body := "送料を除いた支払額を返す。"
	if _, err := book.Ingest(notebook.Draft{Sources: ids, Title: "返品 送料", Lesson: body, AppliesTo: "返品の対応", Body: &body}); err != nil {
		t.Fatal(err)
	}
	text = capture(t, func() error { return cmdWiki([]string{"query", "返品", "送料"}) })
	if !strings.Contains(text, `"lessons":[{`) {
		t.Fatalf("複数の検索語は検索を続ける: %s", text)
	}
	for _, args := range [][]string{
		{"query", ids[0], "0000000000000000"},
		{"query", ids[0], ids[1], ids[0], ids[1], ids[0], ids[1], ids[0], ids[1], ids[0]},
	} {
		if err := cmdWiki(args); err == nil {
			t.Fatal("別プロジェクトの原資料や8件を超える読込みを検索へ逃がさない")
		}
	}
	// まとめ読込みでも引用元の改変検出を通す。
	paths, err := filepath.Glob(filepath.Join(root, "records", "wiki-sources", "*", ids[0]+".json"))
	if err != nil || len(paths) != 1 {
		t.Fatal("保存した原資料を確認できない", err)
	}
	data, err := os.ReadFile(paths[0])
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths[0], append(data, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	if err := cmdWiki([]string{"query", ids[0], ids[1]}); err == nil {
		t.Fatal("まとめ読込みで引用元のハッシュ照合を省略しない")
	}
}
