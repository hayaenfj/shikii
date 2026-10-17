package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hayaenfj/shikii/internal/spec"
	"github.com/hayaenfj/shikii/internal/systemone"
)

// 聞き方で答えが変わる偽モデル: 質問文に "strict" が入っていれば本文を見て正しく答え、無ければ何でも change_request。
func wordingSensitiveModel(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req systemone.Request
		_ = json.NewDecoder(r.Body).Decode(&req)
		state := req.State
		resp := systemone.Response{Model: "m", Answers: map[string]systemone.Answer{}}
		for k, q := range req.Questions {
			probs := map[string]float64{"change_request": 0.9, "question": 0.1, "other": 0}
			if strings.Contains(q.Instructions, "strict") && strings.Contains(state, "why") {
				probs = map[string]float64{"change_request": 0.1, "question": 0.9, "other": 0}
			}
			top, _ := argmax(probs)
			resp.Answers[k] = systemone.Answer{Type: "choice", Choice: top, Probabilities: probs, Confidence: 0.9}
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func specWithVariants(t *testing.T, dir string) string {
	t.Helper()
	path := writeSpec(t, dir, "false") // id intent, min_n 2, question "What is asked?"
	b, _ := os.ReadFile(path)
	src := string(b) + `variants:
  - name: strict
    question: What is asked? Be strict about questions.
  - name: worse
    question: What is asked? Anything goes.
`
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// 正解の貯金で聞き方の候補を全部採点し、勝った候補だけを新しい版として書く。負けた候補は書かない。
func TestTuneRanksVariantsOnTheTruthBankAndWritesTheWinner(t *testing.T) {
	dir := t.TempDir()
	storeDir := filepath.Join(dir, ".shikii")
	srv := wordingSensitiveModel(t)
	specPath := specWithVariants(t, dir)
	rows := `[{"inputs":{"message":"fix a"},"truth":"change_request"},{"inputs":{"message":"fix b"},"truth":"change_request"},
	          {"inputs":{"message":"why a"},"truth":"question"},{"inputs":{"message":"why b"},"truth":"question"}]`
	labeledPath := filepath.Join(dir, "rows.json")
	if err := os.WriteFile(labeledPath, []byte(rows), 0o644); err != nil {
		t.Fatal(err)
	}
	out := capture(t, func() error {
		return cmdTune([]string{specPath, "--labeled", labeledPath, "--local", srv.URL, "--store", storeDir})
	})
	for _, must := range []string{"current", "strict", "worse", "top-1", "holdout", "winner: strict"} {
		if !strings.Contains(out, must) {
			t.Fatalf("tune report must mention %q:\n%s", must, out)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "intent-v2.yaml")); err == nil {
		t.Fatal("without --write nothing may be written")
	}
	capture(t, func() error {
		return cmdTune([]string{specPath, "--labeled", labeledPath, "--local", srv.URL, "--store", storeDir, "--write"})
	})
	v2Path := filepath.Join(dir, "intent-v2.yaml")
	v2, err := spec.Load(v2Path)
	if err != nil {
		t.Fatalf("winner must be written as a new version: %v", err)
	}
	orig := mustLoad(t, specPath)
	if v2.ID != orig.ID || v2.Version != orig.Version+1 || !strings.Contains(v2.Question, "strict") || v2.Hash() == orig.Hash() {
		t.Fatalf("new version: %+v", v2)
	}
	if orig.Question != "What is asked?" {
		t.Fatal("the original file must stay as it was")
	}
	// 採用と同時に線が引かれている(貯金を新しい文言で聞き直して)
	if c := v2.CalibrationFor("m"); c == nil || c.SpecHash != v2.Hash() {
		t.Fatalf("the adopted wording must be calibrated right away: %+v", v2.Calibration)
	}
	// 負けた候補 "worse" はどこにも書かれていない
	if b, _ := os.ReadFile(v2Path); strings.Contains(string(b), "Anything goes") && !strings.Contains(string(b), "variants:") {
		t.Fatal("a losing variant must not become the question")
	}
}

// 今の文言が一番良ければ、何も書かない(「変えない」も結論)。
func TestTuneKeepsCurrentWhenNothingBeatsIt(t *testing.T) {
	dir := t.TempDir()
	storeDir := filepath.Join(dir, ".shikii")
	srv := wordingSensitiveModel(t)
	specPath := specWithVariants(t, dir)
	// 正解が全部 change_request なら、現状(常に change_request)が満点で、strict と同点。同点なら現状を残す
	rows := `[{"inputs":{"message":"fix a"},"truth":"change_request"},{"inputs":{"message":"fix b"},"truth":"change_request"},{"inputs":{"message":"fix c"},"truth":"change_request"}]`
	labeledPath := filepath.Join(dir, "rows.json")
	if err := os.WriteFile(labeledPath, []byte(rows), 0o644); err != nil {
		t.Fatal(err)
	}
	out := capture(t, func() error {
		return cmdTune([]string{specPath, "--labeled", labeledPath, "--local", srv.URL, "--store", storeDir, "--write"})
	})
	if !strings.Contains(out, "winner: current") {
		t.Fatalf("ties must keep the current wording:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(dir, "intent-v2.yaml")); err == nil {
		t.Fatal("keeping the current wording must not write a new version")
	}
}

// 貯金が min_n 未満なら測らない(少ない正解で文言を選ぶのは当てずっぽう)。
func TestTuneRefusesWithoutEnoughTruths(t *testing.T) {
	dir := t.TempDir()
	srv := wordingSensitiveModel(t)
	specPath := specWithVariants(t, dir)
	labeledPath := filepath.Join(dir, "rows.json")
	if err := os.WriteFile(labeledPath, []byte(`[{"inputs":{"message":"fix a"},"truth":"change_request"}]`), 0o644); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	old := stdout
	stdout = &buf
	err := cmdTune([]string{specPath, "--labeled", labeledPath, "--local", srv.URL, "--store", filepath.Join(dir, ".shikii")})
	stdout = old
	if err == nil || !strings.Contains(err.Error(), "min_n") {
		t.Fatalf("tune must refuse below min_n, got %v", err)
	}
}

// 貯金全体では 1 件多く当てる候補でも、holdout(k-1 で選んで 1 で測る)が現状を上回らなければ採らない(まぐれの勝ちで文言を変えない)。
func TestTuneDoesNotAdoptAWinByNoise(t *testing.T) {
	dir := t.TempDir()
	storeDir := filepath.Join(dir, ".shikii")
	// 現状: m1〜m3 を当て、m4 m5 を外す(3/5)。候補 "v2": m1〜m3 と m5 を当て、m4 を外す(4/5)。
	// 5 分割の holdout: m4 を外した 4 件では候補が勝つ → m4 で外す。m5 を外した 4 件は同点 → 現状 → m5 で外す。
	// holdout 3/5 = 現状の 3/5 と同じ → 上回らないので採らない。
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req systemone.Request
		_ = json.NewDecoder(r.Body).Decode(&req)
		resp := systemone.Response{Model: "m", Answers: map[string]systemone.Answer{}}
		for k, q := range req.Questions {
			ok := strings.Contains(req.State, "m1") || strings.Contains(req.State, "m2") || strings.Contains(req.State, "m3")
			if strings.Contains(q.Instructions, "v2") && strings.Contains(req.State, "m5") {
				ok = true
			}
			probs := map[string]float64{"change_request": 0.1, "question": 0.9, "other": 0}
			if ok {
				probs = map[string]float64{"change_request": 0.9, "question": 0.1, "other": 0}
			}
			top, _ := argmax(probs)
			resp.Answers[k] = systemone.Answer{Type: "choice", Choice: top, Probabilities: probs}
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(srv.Close)
	specPath := writeSpec(t, dir, "false")
	b, _ := os.ReadFile(specPath)
	if err := os.WriteFile(specPath, []byte(string(b)+"variants:\n  - name: v2\n    question: What is asked? v2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rows := `[{"inputs":{"message":"m1"},"truth":"change_request"},{"inputs":{"message":"m2"},"truth":"change_request"},{"inputs":{"message":"m3"},"truth":"change_request"},{"inputs":{"message":"m4"},"truth":"change_request"},{"inputs":{"message":"m5"},"truth":"change_request"}]`
	labeledPath := filepath.Join(dir, "rows.json")
	if err := os.WriteFile(labeledPath, []byte(rows), 0o644); err != nil {
		t.Fatal(err)
	}
	out := capture(t, func() error {
		return cmdTune([]string{specPath, "--labeled", labeledPath, "--local", srv.URL, "--store", storeDir, "--write"})
	})
	if !strings.Contains(out, "winner: current") || !strings.Contains(out, "holdout") {
		t.Fatalf("a win that the holdout does not support must not be adopted:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(dir, "intent-v2.yaml")); err == nil {
		t.Fatal("nothing may be written")
	}
}
