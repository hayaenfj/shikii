package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/store"
)

// 0.7.4: `shikii status` の中身(人の手間・届いた教訓の効果・止める検査)を画面にも出す。/v1/status に summary を足す。
func TestStatusEndpointCarriesTheSummaryForThePage(t *testing.T) {
	st, scope, h, _, _ := memoryEffectFixture(t)
	get := func() map[string]any {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/status", nil))
		var out map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("status JSON: %v %s", err, rec.Body.String())
		}
		s, _ := out["summary"].(map[string]any)
		if s == nil {
			t.Fatalf("status には summary が要る: %s", rec.Body.String())
		}
		return s
	}
	s := get()
	mem, _ := s["memory"].(map[string]any)
	gu, _ := s["guards"].(map[string]any)
	if mem["delivered"] != 0.0 || gu["total"] != 0.0 || gu["enabled"] != 0.0 {
		t.Fatalf("最初は何も無い: %v", s)
	}
	writeGuard(t, st.Dir, guard{ID: "changelog-pair", Kind: guardKindPairedFiles, Title: "変更履歴は英語と日本語を揃える", Pairs: [][]string{{"CHANGELOG.md", "CHANGELOG_ja.md"}}})
	memoryEffectPrompt(t, scope, "s1")
	s = get()
	mem, _ = s["memory"].(map[string]any)
	gu, _ = s["guards"].(map[string]any)
	items, _ := gu["items"].([]any)
	if mem["delivered"] != 1.0 || mem["pending"] != 1.0 || gu["total"] != 1.0 || gu["enabled"] != 1.0 || len(items) != 1 || items[0].(map[string]any)["id"] != "changelog-pair" {
		t.Fatalf("届けた教訓と止める検査が summary に出る: %v", s)
	}
	person, _ := s["person"].(map[string]any)
	if person == nil || person["records"] == nil {
		t.Fatalf("人の手間も出る: %v", s)
	}
}

// 0.7.6: 人が答えない判定(wiki-conflict / wiki-place)は確認待ちに数えず、結果待ちの数だけ出す。hook の判定は人の抜き取りがあるので従来どおり。
func TestStatusDoesNotCountMachineOnlySpecsAsWaitingForAPerson(t *testing.T) {
	model := fakeAgentModel(t, func(string) float64 { return .1 })
	hookEnv(t, model)
	repo := gitRepo(t)
	initAt(t, repo)
	root := filepath.Join(repo, ".shikii")
	st := store.Store{Dir: root}
	for i := 0; i < 3; i++ {
		rec := store.Record{TraceID: fmt.Sprintf("wc%d", i), SpecID: "wiki-conflict", Type: "noul", Model: "m", Inputs: map[string]string{"lesson": "a", "new_lesson": "b"}, InputsHash: fmt.Sprintf("h%d", i), Raw: map[string]float64{"yes": .2, "no": .8}, Top: "no", Band: "discard", At: time.Now()}
		if err := st.AppendRecord(rec); err != nil {
			t.Fatal(err)
		}
	}
	o := serveOpts{storeDir: root, specsDir: filepath.Join(root, "decisions"), projectScope: repo}
	h := newServer(o)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/status", nil))
	var body struct {
		Specs []map[string]any `json:"specs"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("%v: %s", err, rec.Body.String()[:200])
	}
	found := false
	for _, sp := range body.Specs {
		if sp["id"] == "wiki-conflict" {
			found = true
			if sp["person_answers"] != false || sp["unlabeled"] != float64(0) || sp["awaiting_results"] != float64(3) || sp["truth_source"] != "results" {
				t.Fatalf("人が答えない判定は確認待ち 0、結果待ち 3: %v", sp)
			}
		}
		if sp["id"] == "needs-plan" && sp["person_answers"] != true {
			t.Fatalf("hook の判定は人が答える: %v", sp)
		}
	}
	if !found {
		t.Fatal("wiki-conflict が無い")
	}
}
