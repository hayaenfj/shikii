package main

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/hayaenfj/shikii/internal/decide"
	"github.com/hayaenfj/shikii/internal/spec"
	"github.com/hayaenfj/shikii/internal/systemone"
)

// 偽モデル: 多択で聞かれると全部 change_request と答える(下手)。はい/いいえ の段で聞かれると本文を読んで当てる(上手)。
// 1 回の呼び出しに載った質問の数と、呼び出しの回数を数える。
func stepwiseModel(t *testing.T) (*httptest.Server, *int32, *int32) {
	t.Helper()
	var calls, questions int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req systemone.Request
		_ = json.NewDecoder(r.Body).Decode(&req)
		atomic.AddInt32(&calls, 1)
		atomic.AddInt32(&questions, int32(len(req.Questions)))
		resp := systemone.Response{Model: "m", Answers: map[string]systemone.Answer{}}
		for k, q := range req.Questions {
			switch q.Type {
			case "noul":
				p := 0.1
				switch {
				case strings.Contains(q.Instructions, "work") && (strings.Contains(req.State, "fix") || strings.Contains(req.State, "why")):
					p = 0.9
				case strings.Contains(q.Instructions, "change") && strings.Contains(req.State, "fix"):
					p = 0.8
				}
				resp.Answers[k] = systemone.Answer{Type: "noul", Noul: p}
			default:
				probs := map[string]float64{"change_request": 0.8, "question": 0.1, "other": 0.1}
				resp.Answers[k] = systemone.Answer{Type: "choice", Choice: "change_request", Probabilities: probs, Confidence: 0.8}
			}
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(srv.Close)
	return srv, &calls, &questions
}

// 分解した spec は、全部の段を 1 回の呼び出しに載せ、枝の確率の積で元の選択肢の分布を作る。
func TestDecideRunsStepsInOneCallAndMultipliesBranches(t *testing.T) {
	srv, calls, questions := stepwiseModel(t)
	dir := t.TempDir()
	sp := mustLoad(t, writeSpec(t, dir, "false"))
	sp.Steps = []spec.Step{{Ask: "Does it ask for work?", No: "other"}, {Ask: "Does it ask to change files?", Yes: "change_request", No: "question"}}
	if err := sp.Validate(); err != nil {
		t.Fatal(err)
	}
	eps := []decide.Endpoint{{Kind: "local", Client: systemone.New(srv.URL, "", "m")}}
	d, err := decide.Decide(context.Background(), sp, map[string]string{"message": "fix a"}, decide.Session{}, eps)
	if err != nil {
		t.Fatal(err)
	}
	// fix a: work 0.9 → change 0.8: other 0.1, change_request 0.72, question 0.18
	want := map[string]float64{"other": 0.1, "change_request": 0.72, "question": 0.18}
	for k, v := range want {
		if math.Abs(d.Raw[k]-v) > 1e-9 {
			t.Fatalf("raw[%s]=%v want %v (all: %v)", k, d.Raw[k], v, d.Raw)
		}
	}
	if d.Top != "change_request" || *calls != 1 || *questions != 2 {
		t.Fatalf("steps must travel in one request: top=%s calls=%d questions=%d", d.Top, *calls, *questions)
	}
	if d.SpecHash != sp.Hash() || d.Type != "choice" {
		t.Fatalf("a decomposed decision is still a choice decision under the spec's key: %+v", d)
	}
	// yes で確定する途中の段: change 0.8 → 残り 0.2 × (work 0.9 → question 0.18, other 0.02)
	sp.Steps = []spec.Step{{Ask: "Does it ask to change files?", Yes: "change_request"}, {Ask: "Does it ask for work?", Yes: "question", No: "other"}}
	d, err = decide.Decide(context.Background(), sp, map[string]string{"message": "fix a"}, decide.Session{}, eps)
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range map[string]float64{"change_request": 0.8, "question": 0.18, "other": 0.02} {
		if math.Abs(d.Raw[k]-v) > 1e-9 {
			t.Fatalf("yes-settling middle step: raw[%s]=%v want %v (all: %v)", k, d.Raw[k], v, d.Raw)
		}
	}
}

// 提案役は分解(steps)も提案できる。形の悪い分解は却下され、理由が残る。
func TestProposalsMayCarrySteps(t *testing.T) {
	sp := mustLoad(t, writeSpec(t, t.TempDir(), "false"))
	props := []proposal{
		{Name: "two-step", Steps: []spec.Step{{Ask: "Any work?", No: "other"}, {Ask: "Change files?", Yes: "change_request", No: "question"}}},
		{Name: "broken", Steps: []spec.Step{{Ask: "Any work?", No: "other"}}},
	}
	added, skipped, notes := adoptProposals(sp, props)
	if added != 1 || skipped != 1 || len(sp.Variants) != 1 || len(sp.Variants[0].Steps) != 2 {
		t.Fatalf("one valid steps proposal must land in variants, the broken one must be skipped: added=%d skipped=%d %+v\n%v", added, skipped, sp.Variants, notes)
	}
	system, _ := buildProposerPrompt(sp, candScore{confusion: map[string]map[string]float64{}}, nil, "", 2, true)
	if !strings.Contains(system, "steps") {
		t.Fatalf("the proposer must be told it may decompose a choice into yes/no steps:\n%s", system)
	}
}

// tune は分解した聞き方を候補として採点できる。多択が下手で はい/いいえ が上手なモデルでは分解が勝ち、採用した版には steps が入る。
func TestTuneScoresAndAdoptsADecomposedWording(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	srv, _, _ := stepwiseModel(t)
	if err := os.MkdirAll(filepath.Join(dir, "decisions"), 0o755); err != nil {
		t.Fatal(err)
	}
	specPath := writeSpec(t, filepath.Join(dir, "decisions"), "false")
	b, _ := os.ReadFile(specPath)
	src := string(b) + `variants:
  - name: two-step
    steps:
      - ask: Does this message ask for any work to be done?
        no: other
      - ask: Does it ask to change files or code?
        yes: change_request
        no: question
`
	if err := os.WriteFile(specPath, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	labeledPath := filepath.Join(dir, "rows.json")
	if err := os.WriteFile(labeledPath, []byte(bankRows()), 0o644); err != nil {
		t.Fatal(err)
	}
	storeDir := filepath.Join(dir, ".shikii")
	out := capture(t, func() error {
		return cmdTune([]string{specPath, "--labeled", labeledPath, "--local", srv.URL, "--store", storeDir, "--write"})
	})
	for _, must := range []string{"two-step", "winner: two-step", "2 yes/no steps"} {
		if !strings.Contains(out, must) {
			t.Fatalf("the decomposed candidate must be scored, described and win; missing %q:\n%s", must, out)
		}
	}
	v2 := mustLoad(t, filepath.Join(dir, "decisions", "intent-v2.yaml"))
	if len(v2.Steps) != 2 || v2.Steps[1].Yes != "change_request" || v2.CalibrationFor("m") == nil {
		t.Fatalf("the adopted version must carry the steps and be calibrated: %+v", v2)
	}
	// 採用した版で聞くと、分解で答える
	eps := []decide.Endpoint{{Kind: "local", Client: systemone.New(srv.URL, "", "m")}}
	d, err := decide.Decide(context.Background(), v2, map[string]string{"message": "why c"}, decide.Session{}, eps)
	if err != nil || d.Top != "question" {
		t.Fatalf("v2 must decide through the steps: %v %+v", err, d)
	}
}
