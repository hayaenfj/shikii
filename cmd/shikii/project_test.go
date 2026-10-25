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
	initTestProject(t, a)
	initTestProject(t, b)
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
			t.Fatalf("decisions must be saved in each project's .shikii: got=%d want=%d err=%v", len(recs), want, err)
		}
	}
	aStore, bStore := filepath.Join(a, ".shikii"), filepath.Join(b, ".shikii")
	if loadSession(bStore, session).Previous != "" {
		t.Fatal("must not hand over another work's replies even with the same session id")
	}
	if ctx, err := memoryContext(bStore, feedbackScope(b), request, &hookSession{}); err != nil || ctx != "" {
		t.Fatalf("must not return another work's conversation: %v", err)
	}
	if ctx, err := memoryContext(aStore, feedbackScope(a), request, &hookSession{}); err != nil || !strings.Contains(ctx, "起動手順") {
		t.Fatalf("the original work can refer to its conversation: %v", err)
	}
	status, err := exec.Command("git", "-C", a, "status", "--porcelain", "--untracked-files=all").Output()
	if err != nil || strings.Contains(string(status), ".shikii/") {
		t.Fatal("conversations must not be picked up by git add -A")
	}
	before := treeSnapshot(a)
	// Internal files a person deliberately tracked are not counted in the outcome decisions either.
	if out, err := exec.Command("git", "-C", a, "add", "-f", ".shikii/decisions/needs-plan.yaml").CombinedOutput(); err != nil {
		t.Fatalf("synthetic tracking setup: %v %s", err, out)
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
		t.Fatal("a tracked .shikii must not count as a change of the work either")
	}
	sub := filepath.Join(a, "docs")
	if err := os.Mkdir(sub, 0700); err != nil {
		t.Fatal(err)
	}
	t.Chdir(sub)
	root, scope, err := currentProjectStore()
	if err != nil || root != aStore || scope != feedbackScope(a) {
		t.Fatal("the CLI in a subfolder must also use the project root's store")
	}
	if q := notebookCommand(t, "query", ""); len(q["pending_sources"].([]any)) != 1 {
		t.Fatal("the CLI's search must also reach the same work's conversations")
	}
	global, _ := agentHome()
	if recs, _ := (store.Store{Dir: global}).Records("prompt-kind", 0); len(recs) != 0 {
		t.Fatal("must not save decisions on the shared side")
	}
	// Initialization alone must not add outcome labels for file changes.
	if labels, err := (store.Store{Dir: aStore}).LabeledTraces("prompt-kind"); err != nil {
		t.Fatal(err)
	} else {
		for _, label := range labels {
			if label.Truth == "edit" {
				t.Fatal("shikii's own saves must not count as changes of the work")
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
	b := filepath.Join(t.TempDir(), filepath.Base(a)) // another project with the same name
	if err := os.Mkdir(b, 0700); err != nil {
		t.Fatal(err)
	}
	bStore, bScope := initTestProject(t, b)
	aScope := feedbackScope(a)
	id, trace := reviewSeed(t, store.Store{Dir: aStore}, aScope, 1)
	if _, err := runNotebook(aStore, aScope, notebookRequest{Operation: "ingest", Draft: notebook.Draft{Sources: []string{id}, Title: "起動手順", Lesson: "モデルを起動する手順を書く", Kind: "rule", Scene: "テストの時", AppliesTo: "README の導入手順"}}); err != nil {
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
		t.Fatal("the entry page must show only each project's name and link")
	}
	for _, path := range []string{"/v1/queue?spec=done-as-asked", "/p/unknown/v1/status"} {
		if code, _ := get(path); code != 404 {
			t.Fatalf("records must not open from the shared side or an unregistered work: %s code=%d", path, code)
		}
	}
	for _, prefix := range []string{aURL, bURL} {
		req := httptest.NewRequest("POST", prefix+"v1/review", strings.NewReader(`{"operation":"query"}`))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != 200 || strings.Contains(rec.Body.String(), trace) != (prefix == aURL) {
			t.Fatal("scoring sources must be read only from the chosen work")
		}
		req = httptest.NewRequest("POST", prefix+"v1/wiki/notebook", strings.NewReader(`{"operation":"query","sources":["`+id+`"]}`))
		req.Header.Set("Content-Type", "application/json")
		rec = httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if (rec.Code == 200) != (prefix == aURL) {
			t.Fatal("a lesson's original text must be read only from the chosen work")
		}

		code, body := get(prefix + "v1/wiki")
		if code != 200 || strings.Contains(body, "モデルを起動する手順を書く") != (prefix == aURL) {
			t.Fatal("must read only the chosen work's lessons")
		}
		code, body = get(prefix + "v1/status")
		if code != 200 || strings.Contains(body, `"records": 1`) != (prefix == aURL) {
			t.Fatal("scoring and line results must be read per project")
		}
	}
	// Even with the same trace, scores and truths cannot be written from another work.
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
			t.Fatalf("must refuse writes into another work: %s", endpoint)
		}
	}
	if _, err := runReview(aStore, aScope, reviewRequest{Operation: "submit", SourceID: id, TraceID: trace, Truth: "no", Reason: "起動手順が不足"}); err != nil {
		t.Fatal(err)
	}
	q, err := reviewQuery(store.Store{Dir: bStore}, bScope)
	if err != nil || q["quality"].(map[string]store.ReviewStats)["done-as-asked"].Scored != 0 {
		t.Fatal("another work's scores must not count toward the trust record")
	}
	// After the store is swapped for a shared location, even an existing handler does not open it.
	if err := os.RemoveAll(filepath.Join(bStore, "records")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(aStore, "records"), filepath.Join(bStore, "records")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := projectStoreAt(b); err == nil {
		t.Fatal("the CLI must not use a link to a shared location either")
	}
	if code, _ := get(bURL + "v1/status"); code != 404 {
		t.Fatal("swapping the store for a link must not allow reading another work's records")
	}
}
