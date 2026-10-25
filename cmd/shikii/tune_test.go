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

// A fake model whose answer changes with the wording: if the question contains "strict" it looks at the text and answers correctly; otherwise it says change_request for anything.
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

// bankRows are the 12 truths used in tune tests: fix a to f are change_request, why a to f are question. extra is JSON for additional rows.
// 12 because it is close to the minimum at which the one-sided sign test passes (6 wins, 0 losses gives p = 1/64).
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
	sub := filepath.Join(dir, "decisions") // the standard layout: wiki/ is created next to decisions/
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

// Score every wording candidate on the bank of truths and write only the winner as a new version. Losers are not written.
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
	// A line is drawn at the same time as adopting (asking the bank again with the new wording)
	if c := v2.CalibrationFor("m"); c == nil || c.SpecHash != v2.Hash() {
		t.Fatalf("the adopted wording must be calibrated right away: %+v", v2.Calibration)
	}
	// The losing candidate "worse" is not written anywhere
	if strings.Contains(v2.Question, "Anything goes") {
		t.Fatal("a losing variant must not become the question")
	}
}

// If the current wording is best, write nothing ("no change" is a conclusion too).
func TestTuneKeepsCurrentWhenNothingBeatsIt(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	storeDir := filepath.Join(dir, ".shikii")
	srv := wordingSensitiveModel(t)
	specPath := specWithVariants(t, dir)
	// If every truth is change_request, the current one (always change_request) is perfect and ties with strict. On a tie, keep the current one
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

// If the bank is below min_n, do not measure (choosing wording with few truths is a guess).
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

// Even a candidate that gets one more right over the whole bank is not adopted unless holdout (choose on k-1, measure on 1) beats the current one (do not change wording on a lucky win).
func TestTuneDoesNotAdoptAWinByNoise(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	storeDir := filepath.Join(dir, ".shikii")
	// Current: gets m1 to m3 right, m4 and m5 wrong (3/5). Candidate "v2": gets m1 to m3 and m5 right, m4 wrong (4/5).
	// 5-fold holdout: on the 4 without m4, the candidate wins → measured on m4, wrong. On the 4 without m5, a tie → current → measured on m5, wrong.
	// holdout 3/5 = the current 3/5 → it does not beat it, so it is not adopted.
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

// A fake proposer (chat LLM): keeps the prompt it received and returns 2 candidates as JSON.
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

// --propose N: give the proposer the current wording, the confusion table, examples it got wrong and the learnings so far; have it make candidates, keep them in the spec's variants, score them, and append the learnings to markdown.
func TestTuneProposeStoresVariantsScoresThemAndWritesTheNotebook(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	storeDir := filepath.Join(dir, ".shikii")
	model := wordingSensitiveModel(t)
	proposer, prompts := fakeProposer(t)
	if err := os.MkdirAll(filepath.Join(dir, "decisions"), 0o755); err != nil {
		t.Fatal(err)
	}
	specPath := writeSpec(t, filepath.Join(dir, "decisions"), "false") // write no candidates; the proposer makes them. The wiki is next to decisions/
	rows := bankRows()
	labeledPath := filepath.Join(dir, "rows.json")
	if err := os.WriteFile(labeledPath, []byte(rows), 0o644); err != nil {
		t.Fatal(err)
	}
	// Rules people wrote (Rules on the wiki's spec page) are given to the proposer every time
	wiki := filepath.Join(dir, ".shikii", "wiki")
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
	// Wording the machine made is not adopted before a person reads it, even with --write. Someone who read it adopts with tune --write
	if _, err := os.Stat(filepath.Join(dir, "decisions", "intent-v2.yaml")); err == nil {
		t.Fatal("a proposed wording must not be adopted unseen")
	}
	capture(t, func() error {
		return cmdTune([]string{specPath, "--labeled", labeledPath, "--local", model.URL, "--store", storeDir, "--write"})
	})
	if _, err := os.Stat(filepath.Join(dir, "decisions", "intent-v2.yaml")); err != nil {
		t.Fatal("after reading the variants, tune --write adopts the winner")
	}
	// Proposals stay in the spec's variants (the next run can score them without proposing)
	sp := mustLoad(t, specPath)
	if len(sp.Variants) != 2 || sp.Variants[0].Name != "proposed-strict" {
		t.Fatalf("proposals must be stored in the spec: %+v", sp.Variants)
	}
	// wiki: the run page has numbers, the verdict and a one-line learning; the spec page has a pointer to the run. The person's rules remain
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
	// The second proposal is made after reading the previous learnings
	capture(t, func() error {
		return cmdTune([]string{filepath.Join(dir, "decisions", "intent-v2.yaml"), "--labeled", labeledPath, "--local", model.URL, "--store", storeDir,
			"--propose", "2", "--proposer", proposer.URL})
	})
	if len(*prompts) < 2 || !strings.Contains((*prompts)[1], "Lesson:") {
		t.Fatalf("the second proposal must be fed the earlier lessons:\n%s", (*prompts)[len(*prompts)-1])
	}
}

// Text from a pii: true spec is never given to a proposer other than a local (loopback) one.
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
