package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/decide"
	"github.com/hayaenfj/shikii/internal/spec"
	"github.com/hayaenfj/shikii/internal/store"
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

// Audit 0.9.2 (re-audit, relative tune): a run tuned from the project folder with a relative spec path kept spec_path relative,
// so serve or the hook, started from another folder, could not find the spec and could not adopt the run. spec_path is saved absolute.
func TestTuneSavesAnAbsoluteSpecPath(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	storeDir := filepath.Join(dir, ".shikii")
	srv := wordingSensitiveModel(t)
	specPath := specWithVariants(t, storeDir) // the standard layout: .shikii/decisions, wiki in .shikii/wiki
	labeledPath := filepath.Join(dir, "rows.json")
	if err := os.WriteFile(labeledPath, []byte(bankRows()), 0o644); err != nil {
		t.Fatal(err)
	}
	rel, err := filepath.Rel(dir, specPath)
	if err != nil || filepath.IsAbs(rel) {
		t.Fatal(rel, err)
	}
	capture(t, func() error {
		return cmdTune([]string{rel, "--labeled", labeledPath, "--local", srv.URL, "--store", storeDir})
	})
	runs, _ := filepath.Glob(filepath.Join(storeDir, "wiki", "runs", "*.json"))
	if len(runs) != 1 {
		t.Fatalf("one run is saved: %v", runs)
	}
	t.Chdir(t.TempDir()) // serve and the hook may run from another folder
	res, err := loadTuneResult(filepath.Join(storeDir, "wiki"), filepath.Base(runs[0]))
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(res.SpecPath) {
		t.Fatalf("spec_path is saved as an absolute path: %q", res.SpecPath)
	}
	got, err1 := os.Stat(res.SpecPath)
	want, err2 := os.Stat(specPath)
	if err1 != nil || err2 != nil || !os.SameFile(got, want) {
		t.Fatalf("spec_path names the tuned spec from any folder: %q (%v %v)", res.SpecPath, err1, err2)
	}
	if _, err := spec.Load(res.SpecPath); err != nil {
		t.Fatalf("the run can be adopted from another folder: %v", err)
	}
}

// MCP can run tune and read results, but has no adopt tool (checking meaning is the person's job).
func TestMCPCanTuneButCannotAdopt(t *testing.T) {
	_, specsDir, _, _ := newTuneFixture(t)
	model := wordingSensitiveModel(t)
	var ef endpointFlags
	ef.model, ef.local = "m", model.URL
	h := newServer(serveOpts{specsDir: specsDir, storeDir: filepath.Join(filepath.Dir(specsDir), ".shikii"), ef: ef})
	var in, out bytes.Buffer
	send := func(req map[string]any) map[string]any {
		in.Reset()
		out.Reset()
		b, _ := json.Marshal(req)
		in.Write(append(b, '\n'))
		if err := runMCP(bufioReader(&in), &out, h); err != nil {
			t.Fatal(err)
		}
		var resp map[string]any
		_ = json.Unmarshal(out.Bytes(), &resp)
		return resp
	}
	r := result(t, send(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/list"}))
	names := map[string]bool{}
	for _, tl := range r["tools"].([]any) {
		names[tl.(map[string]any)["name"].(string)] = true
	}
	if !names["shikii_tune"] {
		t.Fatalf("shikii_tune must exist: %v", names)
	}
	for n := range names {
		if strings.Contains(n, "adopt") {
			t.Fatalf("no adopt tool may be exposed to agents: %v", names)
		}
	}
	r = result(t, send(map[string]any{"jsonrpc": "2.0", "id": 2, "method": "tools/call", "params": map[string]any{"name": "shikii_tune", "arguments": map[string]any{"spec": "intent"}}}))
	sc, _ := r["structuredContent"].(map[string]any)
	if r["isError"] == true || sc == nil || sc["winner"] != "strict" || sc["adoptable"] != true {
		t.Fatalf("shikii_tune must run synchronously and return the result: %v", r)
	}
}

func bufioReader(b *bytes.Buffer) *bufio.Reader { return bufio.NewReader(b) }

// Fake model: if the question has strict, it gets half right (some wrong with confidence 0.9). Otherwise it gets all wrong. Creates a case where calibration cannot meet precision.
func confidentlyWrongModel(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req systemone.Request
		_ = json.NewDecoder(r.Body).Decode(&req)
		state := req.State
		resp := systemone.Response{Model: "m", Answers: map[string]systemone.Answer{}}
		for k, q := range req.Questions {
			strict := strings.Contains(q.Instructions, "strict")
			why := strings.Contains(state, "why")
			st := strings.TrimSpace(state)
			odd := strings.HasSuffix(st, "b") || strings.HasSuffix(st, "d") || strings.HasSuffix(st, "f")
			right := map[string]float64{"change_request": 0.9, "question": 0.1, "other": 0}
			if why {
				right = map[string]float64{"change_request": 0.1, "question": 0.9, "other": 0}
			}
			wrong := map[string]float64{"change_request": right["question"], "question": right["change_request"], "other": 0}
			probs := wrong // without strict, all wrong
			if strict && !odd {
				probs = right // with strict, the 6 of a c e are right and the 6 of b d f are confidently wrong (precision 0.5, so calibration refuses)
			}
			top, _ := argmax(probs)
			resp.Answers[k] = systemone.Answer{Type: "choice", Choice: top, Probabilities: probs, Confidence: 0.9}
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// Adopting and calibrating go together: if calibration cannot write for lack of precision, no new version is left, the run is not "adopted", and it can be redone.
func TestAdoptionIsUndoneWhenCalibrationRefuses(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	specsDir := filepath.Join(dir, "decisions")
	specWithVariants(t, dir)
	labeledPath := filepath.Join(dir, "rows.json")
	rows := bankRows()
	if err := os.WriteFile(labeledPath, []byte(rows), 0o644); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(specsDir, "intent.yaml")
	b, _ := os.ReadFile(p)
	if err := os.WriteFile(p, []byte(strings.Replace(string(b), "eval:\n  min_n: 2\n", "eval:\n  min_n: 2\n  labeled: "+labeledPath+"\n", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	model := confidentlyWrongModel(t)
	var ef endpointFlags
	ef.model, ef.local = "m", model.URL
	h := newServer(serveOpts{specsDir: specsDir, storeDir: filepath.Join(dir, ".shikii"), ef: ef})
	s := httptest.NewServer(h)
	defer s.Close()
	code, res := postJSON(t, s.URL+"/v1/tune/sync", map[string]any{"spec": "intent"})
	if code != 200 || res["adoptable"] != true || res["winner"] != "strict" {
		t.Fatalf("strict must win on top-1 and be adoptable: %d %v", code, res)
	}
	code, out := postJSON(t, s.URL+"/v1/tune/adopt", map[string]any{"spec": "intent", "run": res["run"], "meaning_same": true})
	if code == 200 || out["adopted"] == true {
		t.Fatalf("adoption must fail when calibration refuses: %d %v", code, out)
	}
	if _, err := os.Stat(filepath.Join(specsDir, "intent-v2.yaml")); err == nil {
		t.Fatal("an uncalibrated v2 must not be left behind")
	}
	var st map[string]any
	getJSON(t, s.URL+"/v1/tune?spec=intent", &st)
	if st["latest"].(map[string]any)["adopted"] == true {
		t.Fatalf("the run must not say adopted: %v", st)
	}
	// serve stays on v1
	sps, err := loadSpecs(specsDir)
	if err != nil || len(sps) != 1 || sps["intent"] == nil || sps["intent"].Version != 1 {
		t.Fatalf("serve must keep using v1: %v %+v", err, sps)
	}
}

// Null hypothesis: if all candidates are equally good, adoption stays rare however many candidates are added (one-sided sign test p < 0.05).
// A fake model that gets each candidate right with probability 0.7 using independent random numbers. Measures the adoption rate over 20 candidates × 30 runs.
func TestManyEqualCandidatesAreRarelyAdopted(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	var seed int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req systemone.Request
		_ = json.NewDecoder(r.Body).Decode(&req)
		resp := systemone.Response{Model: "m", Answers: map[string]systemone.Answer{}}
		for k, q := range req.Questions {
			h := fnvHash(fmt.Sprintf("%d|%s|%s", seed, q.Instructions, req.State))
			truthIsQ := strings.Contains(req.State, "why")
			right := h%10 < 7
			probs := map[string]float64{"change_request": 0.9, "question": 0.1, "other": 0}
			if truthIsQ == right {
				probs = map[string]float64{"change_request": 0.1, "question": 0.9, "other": 0}
			}
			top, _ := argmax(probs)
			resp.Answers[k] = systemone.Answer{Type: "choice", Choice: top, Probabilities: probs}
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(srv.Close)
	if err := os.MkdirAll("decisions", 0o755); err != nil {
		t.Fatal(err)
	}
	specPath := writeSpec(t, filepath.Join(dir, "decisions"), "false")
	b, _ := os.ReadFile(specPath)
	variants := "variants:\n"
	for i := 0; i < 20; i++ {
		variants += fmt.Sprintf("  - name: c%d\n    question: What is asked? variant %d\n", i, i)
	}
	if err := os.WriteFile(specPath, []byte(string(b)+variants), 0o644); err != nil {
		t.Fatal(err)
	}
	var rows []string
	for i := 0; i < 20; i++ {
		rows = append(rows, fmt.Sprintf(`{"inputs":{"message":"fix %d"},"truth":"change_request"}`, i), fmt.Sprintf(`{"inputs":{"message":"why %d"},"truth":"question"}`, i))
	}
	labeledPath := filepath.Join(dir, "rows.json")
	if err := os.WriteFile(labeledPath, []byte("["+strings.Join(rows, ",")+"]"), 0o644); err != nil {
		t.Fatal(err)
	}
	adopted := 0
	const runs = 30
	for seed = 1; seed <= runs; seed++ {
		out := capture(t, func() error {
			return cmdTune([]string{specPath, "--labeled", labeledPath, "--local", srv.URL, "--store", filepath.Join(dir, ".shikii")})
		})
		if strings.Contains(out, "dry run: pass --write") {
			adopted++
		}
	}
	if adopted > runs*15/100 {
		t.Fatalf("with 20 candidates of equal skill, %d/%d runs would adopt a wording: the holdout gate is not holding (want ≤ 5%% nominal)", adopted, runs)
	}
}

func fnvHash(s string) uint32 {
	h := fnv.New32a()
	_, _ = h.Write([]byte(s))
	return h.Sum32()
}

// Basic layout: everything shikii makes is under the store (.shikii/). The wiki is always <store>/wiki and is never guessed from where specs are (never written to the parent folder).
func TestWikiLivesAtTheProjectRootNotNextToTheSpec(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "myproject")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(project)
	srv := wordingSensitiveModel(t)
	specPath := specWithVariants(t, project) // myproject/decisions/intent.yaml
	flat := filepath.Join(project, "flat.yaml")
	b, _ := os.ReadFile(specPath)
	if err := os.WriteFile(flat, b, 0o644); err != nil {
		t.Fatal(err)
	}
	rows := bankRows()
	labeledPath := filepath.Join(project, "rows.json")
	if err := os.WriteFile(labeledPath, []byte(rows), 0o644); err != nil {
		t.Fatal(err)
	}
	capture(t, func() error {
		return cmdTune([]string{flat, "--labeled", labeledPath, "--local", srv.URL, "--store", filepath.Join(project, ".shikii")})
	})
	if _, err := os.Stat(filepath.Join(root, "wiki")); err == nil {
		t.Fatal("tune must never write a wiki above the project")
	}
	if runs, _ := filepath.Glob(filepath.Join(project, ".shikii", "wiki", "runs", "*.md")); len(runs) != 1 {
		t.Fatalf("the run must land in ./wiki of the project: %v", runs)
	}
}

// Deduplicating the bank uses the same "latest truth per input" as calibrate. A later correction beats the file row.
// Old truths (choices not in the current spec) and rows with weight > 1 are counted and dropped.
func TestTruthBankUsesLatestTruthAndSkipsUnknownAnswers(t *testing.T) {
	dir := t.TempDir()
	sp := mustLoad(t, writeSpec(t, dir, "false"))
	labeledPath := filepath.Join(dir, "rows.json")
	rows := `[{"inputs":{"message":"fix a"},"truth":"change_request"},{"inputs":{"message":"old"},"truth":"removed_option"},{"inputs":{"message":"heavy"},"truth":"question","weight":5}]`
	if err := os.WriteFile(labeledPath, []byte(rows), 0o644); err != nil {
		t.Fatal(err)
	}
	st := store.Store{Dir: filepath.Join(dir, ".shikii")}
	later := store.Label{TraceID: "t1", SpecID: sp.ID, SpecHash: sp.Hash(), Type: string(sp.Type), Model: "m",
		InputsHash: store.HashInputs(map[string]string{"message": "fix a"}), Inputs: map[string]string{"message": "fix a"},
		Raw: map[string]float64{"question": 0.5, "change_request": 0.5}, Truth: "question", Weight: 1, Source: "ui", At: time.Now().Add(time.Hour)}
	if err := st.AppendLabel(later); err != nil {
		t.Fatal(err)
	}
	bank, skipped, err := truthBank(sp, st, labeledPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(bank) != 2 || skipped != 1 {
		t.Fatalf("want 2 rows (fix a, heavy) and 1 skipped (removed_option), got %d rows, %d skipped: %+v", len(bank), skipped, bank)
	}
	for _, r := range bank {
		if r.inputs["message"] == "fix a" && r.truth != "question" {
			t.Fatalf("the later correction must win: %+v", r)
		}
		if r.weight > 1 {
			t.Fatalf("file weights are capped at 1 like the store: %+v", r)
		}
	}
}

// Rows that cannot be scored (missing required input, nowhere to send) do not stop tune; they are counted and dropped.
func TestTuneSkipsRowsItCannotScore(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	srv := wordingSensitiveModel(t)
	specPath := specWithVariants(t, dir)
	rows := bankRows(`{"inputs":{"other":"no message here"},"truth":"question"}`)
	labeledPath := filepath.Join(dir, "rows.json")
	if err := os.WriteFile(labeledPath, []byte(rows), 0o644); err != nil {
		t.Fatal(err)
	}
	out := capture(t, func() error {
		return cmdTune([]string{specPath, "--labeled", labeledPath, "--local", srv.URL, "--store", filepath.Join(dir, ".shikii")})
	})
	if !strings.Contains(out, "winner: strict") || !strings.Contains(out, "1 truth(s) could not be scored") {
		t.Fatalf("one unscorable row must be skipped and counted, not abort the run:\n%s", out)
	}
}

// The person's "different meaning" also stays on the spec's page, and the next proposer reads it.
func TestRejectionReachesTheNextProposer(t *testing.T) {
	url, _, wiki, _ := newTuneFixture(t)
	code, res := postJSON(t, url+"/v1/tune/sync", map[string]any{"spec": "intent"})
	if code != 200 {
		t.Fatalf("%d %v", code, res)
	}
	if code, _ := postJSON(t, url+"/v1/tune/adopt", map[string]any{"spec": "intent", "run": res["run"], "meaning_same": false, "note": "it now asks about tone"}); code != 200 {
		t.Fatalf("reject: %d", code)
	}
	sp := mustLoad(t, filepath.Join(filepath.Dir(filepath.Dir(wiki)), "decisions", "intent.yaml"))
	if ctx := wikiContext(wiki, sp, "m"); !strings.Contains(ctx, "it now asks about tone") || !strings.Contains(ctx, "strict") {
		t.Fatalf("the proposer must be told which wording a person rejected and why:\n%s", ctx)
	}
}

// tune can score candidates that differ only in layout; if one wins, the adopted version gets the layout, and calibration is drawn with that layout's model name.
func TestTuneScoresAndAdoptsALayout(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	srv := layoutSensitiveModel(t)
	if err := os.MkdirAll(filepath.Join(dir, "decisions"), 0o755); err != nil {
		t.Fatal(err)
	}
	specPath := writeSpec(t, filepath.Join(dir, "decisions"), "false")
	b, _ := os.ReadFile(specPath)
	src := string(b) + "variants:\n  - name: plain\n    layout: plain\n"
	if err := os.WriteFile(specPath, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	labeledPath := filepath.Join(dir, "rows.json")
	if err := os.WriteFile(labeledPath, []byte(bankRows()), 0o644); err != nil {
		t.Fatal(err)
	}
	out := capture(t, func() error {
		return cmdTune([]string{specPath, "--labeled", labeledPath, "--local", srv.URL, "--store", filepath.Join(dir, ".shikii"), "--write"})
	})
	if !strings.Contains(out, "winner: plain") || !strings.Contains(out, "layout: plain") {
		t.Fatalf("the layout candidate must be scored, described and win:\n%s", out)
	}
	v2 := mustLoad(t, filepath.Join(dir, "decisions", "intent-v2.yaml"))
	if v2.Layout != "plain" || v2.CalibrationFor("m+plain") == nil {
		t.Fatalf("the adopted version carries the layout and is calibrated under the layout's model name: %+v", v2.Calibration)
	}
}

func TestProjectTuningKeepsProcessOutput(t *testing.T) {
	dir := t.TempDir()
	paths := []string{filepath.Join(dir, "a.yaml"), filepath.Join(dir, "b.yaml")}
	for _, path := range paths {
		if err := syscall.Mkfifo(path, 0600); err != nil {
			t.Fatal(err)
		}
	}
	openWriter := func(path string) *os.File {
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			f, err := os.OpenFile(path, os.O_WRONLY|syscall.O_NONBLOCK, 0600)
			if err == nil {
				return f
			}
			if !errors.Is(err, syscall.ENXIO) {
				t.Fatalf("cannot open the synthetic pipe: %v", err)
			}
			time.Sleep(5 * time.Millisecond)
		}
		t.Fatal("the other project's tune did not start in time")
		return nil
	}
	original := stdout
	defer func() { stdout = original }()
	a, b := &server{}, &server{}
	doneA, doneB := make(chan struct{}), make(chan struct{})
	go func() { defer close(doneA); a.runTuneCaptured(tuneOpts{specPath: paths[0]}) }()
	writerA := openWriter(paths[0])
	defer writerA.Close()
	go func() { defer close(doneB); b.runTuneCaptured(tuneOpts{specPath: paths[1]}) }()
	writerB := openWriter(paths[1])
	defer writerB.Close()
	writerA.Close()
	<-doneA
	writerB.Close()
	<-doneB
	if stdout != original {
		t.Fatal("overlapping tune runs of other works must not change the process's output")
	}
}

// tune can score a split wording as a candidate. With a model that is bad at many choices and good at yes/no, the split wins and the adopted version gets steps.
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
	// Asking with the adopted version answers through the split
	eps := []decide.Endpoint{{Kind: "local", Client: systemone.New(srv.URL, "", "m")}}
	d, err := decide.Decide(context.Background(), v2, map[string]string{"message": "why c"}, decide.Session{}, eps)
	if err != nil || d.Top != "question" {
		t.Fatalf("v2 must decide through the steps: %v %+v", err, d)
	}
}
