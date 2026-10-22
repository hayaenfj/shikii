package main

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
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
