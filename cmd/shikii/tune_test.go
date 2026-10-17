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

// bankRows は tune のテストで使う正解 12 件: fix a〜f は change_request、why a〜f は question。extra は足す行の JSON。
// 12 件なのは、片側の符号検定(勝ち 6 負け 0 で p = 1/64)が通る最小に近い数だから。
func bankRows(extra ...string) string {
	var rows []string
	for _, x := range []string{"a", "b", "c", "d", "e", "f"} {
		rows = append(rows, `{"inputs":{"message":"fix `+x+`"},"truth":"change_request"}`, `{"inputs":{"message":"why `+x+`"},"truth":"question"}`)
	}
	rows = append(rows, extra...)
	return "[" + strings.Join(rows, ",") + "]"
}

func specWithVariants(t *testing.T, dir string) string {
	t.Helper()
	sub := filepath.Join(dir, "decisions") // 標準の配置: decisions/ の隣に wiki/ ができる
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	path := writeSpec(t, sub, "false") // id intent, min_n 2, question "What is asked?"
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
	t.Chdir(dir)
	storeDir := filepath.Join(dir, ".shikii")
	srv := wordingSensitiveModel(t)
	specPath := specWithVariants(t, dir)
	rows := bankRows()
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
	if _, err := os.Stat(filepath.Join(dir, "decisions", "intent-v2.yaml")); err == nil {
		t.Fatal("without --write nothing may be written")
	}
	capture(t, func() error {
		return cmdTune([]string{specPath, "--labeled", labeledPath, "--local", srv.URL, "--store", storeDir, "--write"})
	})
	v2Path := filepath.Join(dir, "decisions", "intent-v2.yaml")
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
	if strings.Contains(v2.Question, "Anything goes") {
		t.Fatal("a losing variant must not become the question")
	}
}

// 今の文言が一番良ければ、何も書かない(「変えない」も結論)。
func TestTuneKeepsCurrentWhenNothingBeatsIt(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
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
	if _, err := os.Stat(filepath.Join(dir, "decisions", "intent-v2.yaml")); err == nil {
		t.Fatal("keeping the current wording must not write a new version")
	}
}

// 貯金が min_n 未満なら測らない(少ない正解で文言を選ぶのは当てずっぽう)。
func TestTuneRefusesWithoutEnoughTruths(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
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
	t.Chdir(dir)
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
	if _, err := os.Stat(filepath.Join(dir, "decisions", "intent-v2.yaml")); err == nil {
		t.Fatal("nothing may be written")
	}
}

// 提案役(チャット用 LLM)の偽物: 受け取ったプロンプトを控え、候補 2 つを JSON で返す。
func fakeProposer(t *testing.T) (*httptest.Server, *[]string) {
	t.Helper()
	var prompts []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Messages []struct {
				Role, Content string
			} `json:"messages"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		var all strings.Builder
		for _, m := range req.Messages {
			all.WriteString(m.Content + "\n")
		}
		prompts = append(prompts, all.String())
		body := map[string]any{"choices": []map[string]any{{"message": map[string]any{"role": "assistant", "content": "```json\n" +
			`[{"name":"proposed-strict","question":"What is asked? Be strict about questions.","rationale":"questions were mistaken for requests"},` +
			`{"name":"proposed-loose","question":"What is asked? Anything goes.","rationale":"control"}]` + "\n```"}}}}
		_ = json.NewEncoder(w).Encode(body)
	}))
	t.Cleanup(srv.Close)
	return srv, &prompts
}

// --propose N: 提案役に今の聞き方・混同表・外した実例・これまでの学びを渡して候補を作らせ、spec の variants に残し、採点し、学びを markdown に追記する。
func TestTuneProposeStoresVariantsScoresThemAndWritesTheNotebook(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	storeDir := filepath.Join(dir, ".shikii")
	model := wordingSensitiveModel(t)
	proposer, prompts := fakeProposer(t)
	if err := os.MkdirAll(filepath.Join(dir, "decisions"), 0o755); err != nil {
		t.Fatal(err)
	}
	specPath := writeSpec(t, filepath.Join(dir, "decisions"), "false") // 候補は書かない。提案役に作らせる。wiki は decisions/ の隣
	rows := bankRows()
	labeledPath := filepath.Join(dir, "rows.json")
	if err := os.WriteFile(labeledPath, []byte(rows), 0o644); err != nil {
		t.Fatal(err)
	}
	// 人が書いた決まり(wiki の spec ページの Rules)は提案役に毎回渡る
	wiki := filepath.Join(dir, "wiki")
	notebook := filepath.Join(wiki, "specs", "intent.md")
	if err := os.MkdirAll(filepath.Dir(notebook), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(notebook, []byte("# intent\n\n## Rules\n- Never mention the word banana.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := capture(t, func() error {
		return cmdTune([]string{specPath, "--labeled", labeledPath, "--local", model.URL, "--store", storeDir,
			"--propose", "2", "--proposer", proposer.URL, "--write"})
	})
	if len(*prompts) < 1 {
		t.Fatal("the proposer must be asked")
	}
	p := (*prompts)[0]
	for _, must := range []string{"What is asked?", "confusion", "why a", "Never mention the word banana"} {
		if !strings.Contains(p, must) {
			t.Fatalf("proposer prompt must carry the current wording, the confusion matrix, a missed example and the human rules; missing %q:\n%s", must, p)
		}
	}
	if !strings.Contains(out, "proposed-strict") || !strings.Contains(out, "winner: proposed-strict") {
		t.Fatalf("proposed candidates must be scored and the good one must win:\n%s", out)
	}
	// 機械が作った文言は --write でも人が読む前には採用されない。読んだ人が tune --write で採用する
	if _, err := os.Stat(filepath.Join(dir, "decisions", "intent-v2.yaml")); err == nil {
		t.Fatal("a proposed wording must not be adopted unseen")
	}
	capture(t, func() error {
		return cmdTune([]string{specPath, "--labeled", labeledPath, "--local", model.URL, "--store", storeDir, "--write"})
	})
	if _, err := os.Stat(filepath.Join(dir, "decisions", "intent-v2.yaml")); err != nil {
		t.Fatal("after reading the variants, tune --write adopts the winner")
	}
	// 提案は spec の variants に残る(次回は提案無しでも採点できる)
	sp := mustLoad(t, specPath)
	if len(sp.Variants) != 2 || sp.Variants[0].Name != "proposed-strict" {
		t.Fatalf("proposals must be stored in the spec: %+v", sp.Variants)
	}
	// wiki: run ページに数字と採否と一行の学び、spec ページに run への案内。人の決まりは残る
	nb, _ := os.ReadFile(notebook)
	for _, must := range []string{"## Rules", "Never mention the word banana", "proposed-strict", "adopted", "Lesson:", "runs/"} {
		if !strings.Contains(string(nb), must) {
			t.Fatalf("spec page must keep the rules and point at the run; missing %q:\n%s", must, nb)
		}
	}
	runs, _ := filepath.Glob(filepath.Join(wiki, "runs", "*.md"))
	if len(runs) != 2 {
		t.Fatalf("two run pages expected (propose, then adopt), got %v", runs)
	}
	if rp, _ := os.ReadFile(runs[0]); !strings.Contains(string(rp), "holdout") || !strings.Contains(string(rp), "proposed-strict") {
		t.Fatalf("run page must carry the table and the holdout:\n%s", rp)
	}
	// 2 回目の提案は、前回の学びを読んで出す
	capture(t, func() error {
		return cmdTune([]string{filepath.Join(dir, "decisions", "intent-v2.yaml"), "--labeled", labeledPath, "--local", model.URL, "--store", storeDir,
			"--propose", "2", "--proposer", proposer.URL})
	})
	if len(*prompts) < 2 || !strings.Contains((*prompts)[1], "Lesson:") {
		t.Fatalf("the second proposal must be fed the earlier lessons:\n%s", (*prompts)[len(*prompts)-1])
	}
}

// pii: true の本文は、手元(loopback)以外の提案役には渡さない。
func TestTuneProposeRefusesRemoteProposerForPIISpecs(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	model := wordingSensitiveModel(t)
	specPath := writeSpec(t, dir, "true")
	labeledPath := filepath.Join(dir, "rows.json")
	if err := os.WriteFile(labeledPath, []byte(`[{"inputs":{"message":"fix a"},"truth":"change_request"},{"inputs":{"message":"why a"},"truth":"question"}]`), 0o644); err != nil {
		t.Fatal(err)
	}
	err := cmdTune([]string{specPath, "--labeled", labeledPath, "--local", model.URL, "--store", filepath.Join(dir, ".shikii"),
		"--propose", "2", "--proposer", "https://api.example.invalid/v1"})
	if err == nil || !strings.Contains(err.Error(), "pii") {
		t.Fatalf("a pii:true spec must not send example texts to a remote proposer, got %v", err)
	}
}
