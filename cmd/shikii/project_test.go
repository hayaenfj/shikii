package main

import (
	"github.com/hayaenfj/shikii/internal/notebook"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hayaenfj/shikii/internal/store"
)

func TestHookSeparatesProjectStores(t *testing.T) {
	model := fakeAgentModel(t, func(string) float64 { return 0.1 })
	hookEnv(t, model)
	a, b := gitRepo(t), gitRepo(t)
	const session = "same-session"
	const request = "README のインストール手順を詳しく書き直して"
	runHook(t, "user-prompt-submit", map[string]any{"session_id": session, "cwd": a, "prompt": request})
	runHook(t, "stop", map[string]any{"session_id": session, "cwd": a, "last_assistant_message": "手順を直しました"})
	runHook(t, "user-prompt-submit", map[string]any{"session_id": session, "cwd": a, "prompt": "モデルの起動手順が足りないので追記して"})
	runHook(t, "user-prompt-submit", map[string]any{"session_id": session, "cwd": b, "prompt": request})
	for _, root := range []string{a, b} {
		recs, err := (store.Store{Dir: filepath.Join(root, ".shikii")}).Records("prompt-kind", 0)
		want := 1
		if root == a {
			want = 2
		}
		if err != nil || len(recs) != want {
			t.Fatalf("判定は各プロジェクトの .shikii に保存する: got=%d want=%d err=%v", len(recs), want, err)
		}
	}
	aStore, bStore := filepath.Join(a, ".shikii"), filepath.Join(b, ".shikii")
	if loadSession(bStore, session).Previous != "" {
		t.Fatal("同じ session id でも他の仕事の返答を渡さない")
	}
	if ctx, err := recallFeedback(bStore, feedbackScope(b), request); err != nil || ctx != "" {
		t.Fatalf("他の仕事の会話を返さない: %v", err)
	}
	if ctx, err := recallFeedback(aStore, feedbackScope(a), request); err != nil || !strings.Contains(ctx, "起動手順") {
		t.Fatalf("元の仕事では会話を参照できる: %v", err)
	}
	status, err := exec.Command("git", "-C", a, "status", "--porcelain", "--untracked-files=all").Output()
	if err != nil || strings.Contains(string(status), ".shikii/") {
		t.Fatal("会話を git add -A の対象にしない")
	}
	before := treeSnapshot(a)
	// 人が意図して追跡した内部ファイルも、結果の判定には含めない。
	if out, err := exec.Command("git", "-C", a, "add", "-f", ".shikii/decisions/needs-plan.yaml").CombinedOutput(); err != nil {
		t.Fatalf("合成の追跡設定: %v %s", err, out)
	}
	path := filepath.Join(aStore, "decisions", "needs-plan.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(data, []byte("\n# 内部の控え\n")...), 0600); err != nil {
		t.Fatal(err)
	}
	if after := treeSnapshot(a); before == "" || after != before {
		t.Fatal("追跡済みの .shikii も作業の変更に数えない")
	}
	sub := filepath.Join(a, "docs")
	if err := os.Mkdir(sub, 0700); err != nil {
		t.Fatal(err)
	}
	t.Chdir(sub)
	root, scope, err := currentProjectStore()
	if err != nil || root != aStore || scope != feedbackScope(a) {
		t.Fatal("サブフォルダの CLI もプロジェクトルートの保存先を使う")
	}
	if q := notebookCommand(t, "query", ""); len(q["pending_sources"].([]any)) != 1 {
		t.Fatal("CLI の検索も同じ仕事の会話へ届く")
	}
	global, _ := agentHome()
	if recs, _ := (store.Store{Dir: global}).Records("prompt-kind", 0); len(recs) != 0 {
		t.Fatal("共通側へ判定を保存しない")
	}
	// 初期化だけでファイル変更の結果ラベルを増やさない。
	if labels, err := (store.Store{Dir: aStore}).LabeledTraces("prompt-kind"); err != nil {
		t.Fatal(err)
	} else {
		for _, label := range labels {
			if label.Truth == "edit" {
				t.Fatal("shikii 自身の保存を作業の変更に数えない")
			}
		}
	}

	if _, err := os.Stat(filepath.Join(aStore, "decisions", "needs-plan.yaml")); err != nil {
		t.Fatal(err)
	}
}

func TestProjectHubKeepsReadsAndWritesWithinTheSelectedProject(t *testing.T) {
	model := fakeAgentModel(t, func(string) float64 { return .1 })
	a := gitRepo(t)
	aStore := hookEnv(t, model, a)
	b := filepath.Join(t.TempDir(), filepath.Base(a)) // 同名の別プロジェクト
	if err := os.Mkdir(b, 0700); err != nil {
		t.Fatal(err)
	}
	bStore, bScope, err := prepareHookProject(b)
	if err != nil {
		t.Fatal(err)
	}
	aScope := feedbackScope(a)
	id, trace := reviewSeed(t, store.Store{Dir: aStore}, aScope, 1)
	if _, err := runNotebook(aStore, aScope, notebookRequest{Operation: "ingest", Draft: notebook.Draft{Sources: []string{id}, Title: "起動手順", Lesson: "モデルを起動する手順を書く", AppliesTo: "README の導入手順"}}); err != nil {
		t.Fatal(err)
	}
	common, _ := agentHome()
	h := newProjectHub(common, serveOpts{})
	get := func(path string) (int, string) {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
		return rec.Code, rec.Body.String()
	}
	aURL, bURL := "/p/"+projectID(aScope)+"/", "/p/"+projectID(bScope)+"/"
	if code, body := get("/v1/status"); code != 200 || !strings.Contains(body, aURL) || !strings.Contains(body, bURL) || strings.Contains(body, "起動手順") {
		t.Fatal("入口には各プロジェクトの名前とリンクだけを出す")
	}
	for _, path := range []string{"/v1/queue?spec=done-as-asked", "/p/unknown/v1/status"} {
		if code, _ := get(path); code != 404 {
			t.Fatalf("共通側と未登録の仕事から記録を開けない: %s code=%d", path, code)
		}
	}
	for _, prefix := range []string{aURL, bURL} {
		req := httptest.NewRequest("POST", prefix+"v1/review", strings.NewReader(`{"operation":"query"}`))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != 200 || strings.Contains(rec.Body.String(), trace) != (prefix == aURL) {
			t.Fatal("採点の原資料も選んだ仕事だけで読む")
		}
		req = httptest.NewRequest("POST", prefix+"v1/wiki/notebook", strings.NewReader(`{"operation":"query","sources":["`+id+`"]}`))
		req.Header.Set("Content-Type", "application/json")
		rec = httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if (rec.Code == 200) != (prefix == aURL) {
			t.Fatal("教訓の原文も選んだ仕事だけで読む")
		}

		code, body := get(prefix + "v1/wiki")
		if code != 200 || strings.Contains(body, "モデルを起動する手順を書く") != (prefix == aURL) {
			t.Fatal("選んだ仕事だけの教訓を読む")
		}
		code, body = get(prefix + "v1/status")
		if code != 200 || strings.Contains(body, `"records": 1`) != (prefix == aURL) {
			t.Fatal("採点や線の成績もプロジェクトごとに読む")
		}
	}
	// 同じ trace を指定しても、他の仕事から採点や正解を書けない。
	for _, endpoint := range []string{"review", "label"} {
		body := `{"operation":"submit","source_id":"` + id + `","trace_id":"` + trace + `","truth":"no","reason":"起動手順が不足"}`
		if endpoint == "label" {
			body = `{"trace_id":"` + trace + `","truth":"no","source":"ui"}`
		}
		req := httptest.NewRequest("POST", bURL+"v1/"+endpoint, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code < 400 {
			t.Fatalf("他の仕事への書き込みを拒む: %s", endpoint)
		}
	}
	if _, err := runReview(aStore, aScope, reviewRequest{Operation: "submit", SourceID: id, TraceID: trace, Truth: "no", Reason: "起動手順が不足"}); err != nil {
		t.Fatal(err)
	}
	q, err := reviewQuery(store.Store{Dir: bStore}, bScope)
	if err != nil || q["quality"].(map[string]store.ReviewStats)["done-as-asked"].Scored != 0 {
		t.Fatal("別の仕事の採点を信頼の実績に数えない")
	}
	// 保存先を共有先へ差し替えたら、既存の handler でも開かない。
	if err := os.RemoveAll(filepath.Join(bStore, "records")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(aStore, "records"), filepath.Join(bStore, "records")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := projectStoreAt(b); err == nil {
		t.Fatal("CLI も共有先へのリンクを使わない")
	}
	if code, _ := get(bURL + "v1/status"); code != 404 {
		t.Fatal("保存先のリンク差し替えで別の仕事の記録を読めない")
	}
}
