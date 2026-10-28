package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/decide"
	"github.com/hayaenfj/shikii/internal/spec"
	"github.com/hayaenfj/shikii/internal/store"
	"github.com/hayaenfj/shikii/internal/systemone"
)

// noSignalRows are 400 truths, "yes" 9 times in 10, where the probability says nothing: half of each answer at 0.8 and half at 0.75,
// so the AUC is 0.5 and the upper end of its interval (about 0.6) is below 0.7.
func noSignalRows() []map[string]any {
	rows := []map[string]any{}
	for i := 0; i < 400; i++ {
		truth, raw := "yes", 0.75
		if i%2 == 0 {
			raw = 0.8
		}
		if i%20 == 1 || i%20 == 2 {
			truth = "no"
		}
		rows = append(rows, map[string]any{"inputs": map[string]string{"a": fmt.Sprint(i)}, "truth": truth, "raw": raw})
	}
	return rows
}

// clearRows are 50 truths the probability ranks well, with one overlapping pair (a line is drawn).
func clearRows() []map[string]any {
	rows := []map[string]any{}
	for i := 0; i < 50; i++ {
		truth, raw := "no", 0.2
		if i%2 == 0 {
			truth, raw = "yes", 0.8
		}
		if i == 1 {
			raw = 0.9
		}
		rows = append(rows, map[string]any{"inputs": map[string]string{"a": fmt.Sprint(i)}, "truth": truth, "raw": raw})
	}
	return rows
}

// writeHookNoulSpec is writeNoulSpec as one of the hook's questions (needs-plan): only those are stopped.
func writeHookNoulSpec(t *testing.T, dir string) string {
	t.Helper()
	src, err := os.ReadFile(writeNoulSpec(t, dir))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "needs-plan.yaml")
	if err := os.WriteFile(path, bytes.Replace(src, []byte("id: same\n"), []byte("id: needs-plan\n"), 1), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// writeStop keeps a stop as calibrate would, under the wording it names.
func writeStop(dir, specID string, b []byte) error {
	var s stoppedLine
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	return os.WriteFile(stoppedPath(dir, specID, s.SpecHash), b, 0o600)
}

func calibrateRows(t *testing.T, path, storeDir string, rows []map[string]any, model ...string) (string, error) {
	t.Helper()
	file := filepath.Join(filepath.Dir(path), "labels.json")
	data, _ := json.Marshal(rows)
	if err := os.WriteFile(file, data, 0o600); err != nil {
		t.Fatal(err)
	}
	var report bytes.Buffer
	o := calibrateOpts{output: &report, storeDir: storeDir, labeled: file, write: true}
	if len(model) > 0 {
		o.ef.model = model[0]
	}
	err := runCalibrate(path, o)
	return report.String(), err
}

// A refused calibration with enough truths and no acceptable signal keeps the stop with the measured AUC; a line drawn later removes it.
func TestCalibrateKeepsTheStopOfAQuestionThatCannotTellTheAnswersApart(t *testing.T) {
	dir := t.TempDir()
	path := writeHookNoulSpec(t, dir)
	storeDir := filepath.Join(dir, "store")
	report, err := calibrateRows(t, path, storeDir, noSignalRows())
	if err == nil {
		t.Fatalf("no line for answers the probability cannot tell apart:\n%s", report)
	}
	stop, ok := readStopped(storeDir, mustLoad(t, path))
	if !ok || stop.AUC != 0.5 || stop.High >= stopBelowAUC || stop.Pos != 360 || stop.Neg != 40 {
		t.Fatalf("the stop is kept with the AUC it was measured on: %+v ok=%v\n%s", stop, ok, report)
	}
	if _, err := calibrateRows(t, path, storeDir, clearRows()); err != nil {
		t.Fatal(err)
	}
	if _, ok := readStopped(storeDir, mustLoad(t, path)); ok {
		t.Fatal("a line drawn later removes the stop")
	}
}

// A refusal for another reason is not a stop: too few truths, or a signal whose interval still reaches 0.7.
func TestCalibrateDoesNotStopForOtherRefusals(t *testing.T) {
	for _, tc := range []struct {
		name string
		rows []map[string]any
		minN string
	}{
		{"too few truths", noSignalRows(), "min_n: 1000"},
		{"interval still reaches 0.7", noSignalRows()[:50], "min_n: 4"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := writeHookNoulSpec(t, dir)
			src, _ := os.ReadFile(path)
			if err := os.WriteFile(path, bytes.Replace(src, []byte("min_n: 4"), []byte(tc.minN), 1), 0o644); err != nil {
				t.Fatal(err)
			}
			storeDir := filepath.Join(dir, "store")
			if _, err := calibrateRows(t, path, storeDir, tc.rows); err == nil {
				t.Fatal("no line for these truths")
			}
			if _, ok := readStopped(storeDir, mustLoad(t, path)); ok {
				t.Fatal("not a stop")
			}
		})
	}
}

// The hook leaves a stopped question unasked except on 1 request in 20, a stop measured on another wording does not apply, and an
// answer from another model removes the stop.
func TestHookAsksAStoppedQuestionOnOneRequestInTwenty(t *testing.T) {
	agent := t.TempDir()
	if err := os.MkdirAll(filepath.Join(agent, "decisions"), 0o755); err != nil {
		t.Fatal(err)
	}
	path := writeHookNoulSpec(t, filepath.Join(agent, "decisions"))
	sp := mustLoad(t, path)
	b, _ := json.Marshal(stoppedLine{Model: "m", SpecHash: sp.Hash(), AUC: 0.5, Low: 0.4, High: 0.6, Pos: 360, Neg: 40, At: time.Now()})
	if err := writeStop(agent, "needs-plan", b); err != nil {
		t.Fatal(err)
	}
	asked := 0
	for i := 0; i < 400; i++ {
		inputs := map[string]string{"a": fmt.Sprint(i)}
		if !hookSkipsStopped(agent, "needs-plan", inputs) {
			asked++
			if !stoppedAsk("needs-plan", inputs) {
				t.Fatalf("only the requests stoppedAsk picks are asked: %v", inputs)
			}
		}
	}
	if asked == 0 || asked == 400 {
		t.Fatalf("some requests are still asked, most are not: %d of 400", asked)
	}
	resumeOnNewModel(agent, "needs-plan", "m")
	if _, ok := readStopped(agent, sp); !ok {
		t.Fatal("an answer from the same model keeps the stop")
	}
	resumeOnNewModel(agent, "needs-plan", "m2")
	if _, ok := readStopped(agent, sp); ok {
		t.Fatal("an answer from another model removes the stop")
	}
	other, _ := json.Marshal(stoppedLine{Model: "m", SpecHash: "another-wording"})
	if err := writeStop(agent, "needs-plan", other); err != nil {
		t.Fatal(err)
	}
	if hookSkipsStopped(agent, "needs-plan", map[string]string{"a": "1"}) || hookSkipsStopped(agent, "needs-plan", map[string]string{"a": "2"}) {
		t.Fatal("a stop measured on another wording does not stop the current one")
	}
}

// status and the line state say the question is stopped and why, for the model it was measured on only.
func TestLineStateSaysAQuestionIsStopped(t *testing.T) {
	agent := t.TempDir()
	if err := os.MkdirAll(filepath.Join(agent, "decisions"), 0o755); err != nil {
		t.Fatal(err)
	}
	path := writeHookNoulSpec(t, filepath.Join(agent, "decisions"))
	sp := mustLoad(t, path)
	st := store.Store{Dir: agent}
	for i := 0; i < 6; i++ {
		truth := "yes"
		if i%2 == 1 {
			truth = "no"
		}
		if err := st.AppendLabel(store.Label{TraceID: fmt.Sprintf("%016x", i), SpecID: "needs-plan", SpecHash: sp.Hash(), Model: "m", InputsHash: fmt.Sprint(i), Raw: map[string]float64{"yes": 0.5}, Truth: truth, Weight: 1, Source: "ui", At: time.Now()}); err != nil {
			t.Fatal(err)
		}
	}
	b, _ := json.Marshal(stoppedLine{Model: "m", SpecHash: sp.Hash(), AUC: 0.35, Low: 0.19, High: 0.51, Pos: 3, Neg: 3})
	if err := writeStop(agent, "needs-plan", b); err != nil {
		t.Fatal(err)
	}
	_, s, ok := currentLineState(agent, sp)
	if !ok || s.Kind != lineStopped || !strings.Contains(s.statusNote(sp, sp.Eval.MinN), "stopped: the probabilities do not tell the answers apart (AUC 0.35, 95% 0.19-0.51") {
		t.Fatalf("the line state is stopped with the reason: %+v %q", s, s.statusNote(sp, sp.Eval.MinN))
	}
	other, _ := json.Marshal(stoppedLine{Model: "m2", SpecHash: sp.Hash()})
	if err := writeStop(agent, "needs-plan", other); err != nil {
		t.Fatal(err)
	}
	if _, s, _ := currentLineState(agent, sp); s.Kind == lineStopped {
		t.Fatal("a stop measured on another model does not apply to this one")
	}
}

// hookDecideAll does not ask the model for a stopped question on a request stoppedAsk does not pick, and asks it on one it picks.
func TestHookDoesNotAskTheModelForAStoppedQuestion(t *testing.T) {
	calls := 0
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req systemone.Request
		_ = json.NewDecoder(r.Body).Decode(&req)
		calls++
		resp := systemone.Response{Model: "m", Answers: map[string]systemone.Answer{}}
		for k := range req.Questions {
			resp.Answers[k] = systemone.Answer{Type: "noul", Noul: 0.6}
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(model.Close)
	agent := hookEnv(t, model)
	sp := mustLoad(t, writeHookNoulSpec(t, filepath.Join(agent, "decisions")))
	b, _ := json.Marshal(stoppedLine{Model: "m", SpecHash: sp.Hash(), AUC: 0.5, Low: 0.4, High: 0.6})
	if err := writeStop(agent, "needs-plan", b); err != nil {
		t.Fatal(err)
	}
	var skipped, picked map[string]string
	for i := 0; skipped == nil || picked == nil; i++ { // the first input of each kind (stoppedAsk is a fixed hash, so these are always the same)
		in := map[string]string{"a": fmt.Sprint(i)}
		if stoppedAsk("needs-plan", in) {
			picked = in
		} else if skipped == nil {
			skipped = in
		}
	}
	calls = 0 // only the calls below count (setting up the project may reach the model)
	ds, _, err := hookDecideAll(agent, []string{"needs-plan"}, skipped, "p", nil)
	if err != nil || ds[0] != nil || calls != 0 {
		t.Fatalf("a stopped question is not asked: d=%v calls=%d err=%v", ds[0], calls, err)
	}
	ds, _, err = hookDecideAll(agent, []string{"needs-plan"}, picked, "p", nil)
	if err != nil || ds[0] == nil || calls != 1 {
		t.Fatalf("1 request in 20 still asks it: d=%v calls=%d err=%v", ds[0], calls, err)
	}
}

// 0.10: calibrate keeps the share of real "no" below ask. 20 "yes" at 0.9, and 7 "yes" and 13 "no" at 0.1: the ranking is good (a line
// is drawn) and the 20 at 0.1 stay together below ask after any calibration (mostly "no"), but only 13 of them, 0.65, are "no": less than act (0.70).
func TestCalibrateKeepsTheShareOfNoBelowAsk(t *testing.T) {
	dir := t.TempDir()
	path := writeNoulSpec(t, dir)
	rows := []map[string]any{}
	for i := 0; i < 40; i++ {
		truth, raw := "yes", 0.9
		switch {
		case i >= 27:
			truth, raw = "no", 0.1
		case i >= 20:
			raw = 0.1
		}
		rows = append(rows, map[string]any{"inputs": map[string]string{"a": fmt.Sprint(i)}, "truth": truth, "raw": raw})
	}
	report, err := calibrateRows(t, path, filepath.Join(dir, "store"), rows)
	if err != nil {
		t.Fatalf("the ranking is good enough for a line: %v\n%s", err, report)
	}
	c := mustLoad(t, path).Calibration
	if len(c) != 1 || math.Abs(c[0].PrecisionAtDiscard-0.65) > 1e-9 {
		t.Fatalf("13 of the 20 below ask are a real no: %+v\n%s", c, report)
	}
}

// The "this looks outside what you asked for" notice on the discard side is given only when the line put real "no" answers there at
// least as often as act requires (before 0.10 any calibrated discard gave it).
func TestDiscardNoticeNeedsTheShareOfNoToReachAct(t *testing.T) {
	agent := hookEnv(t, fakeAgentModel(t, func(string) float64 { return 0.1 }))
	path := filepath.Join(agent, "decisions", "changes-in-scope.yaml")
	for _, tc := range []struct {
		share  float64
		notice bool
	}{{0.5, false}, {0.95, true}} {
		sp := mustLoad(t, path)
		sp.Calibration = []spec.Calibration{{Model: "kev-4b", SpecHash: sp.Hash(), Method: "identity", N: 100, PrecisionAtDiscard: tc.share, FittedAt: "2026-10-28"}}
		if err := sp.Save(); err != nil {
			t.Fatal(err)
		}
		d, out, err := hookDecide(agent, "changes-in-scope", map[string]string{"request": fmt.Sprint("fix ", tc.share), "files": "a.go"}, "p")
		if err != nil || d.Band != decide.Discard {
			t.Fatalf("a confident no: %+v %v", d, err)
		}
		if got := strings.Contains(out.System, "outside what you asked for"); got != tc.notice {
			t.Fatalf("share %.2f: notice %v, want %v: %q", tc.share, got, tc.notice, out.System)
		}
	}
}

// skippedOf counts the requests (inputs a=0..n-1) on which the hook leaves the question unasked.
func skippedOf(agent, specID string, n int) int {
	k := 0
	for i := 0; i < n; i++ {
		if hookSkipsStopped(agent, specID, map[string]string{"a": fmt.Sprint(i)}) {
			k++
		}
	}
	return k
}

func hookStopAgent(t *testing.T) (agent, path string) {
	t.Helper()
	agent = t.TempDir()
	if err := os.MkdirAll(filepath.Join(agent, "decisions"), 0o755); err != nil {
		t.Fatal(err)
	}
	return agent, writeHookNoulSpec(t, filepath.Join(agent, "decisions"))
}

// A question with a line for one model is never stopped by another model's refused calibration (the stop would silence the line).
func TestStopOfAnotherModelLeavesAQuestionWithALineAsked(t *testing.T) {
	agent, path := hookStopAgent(t)
	if r, err := calibrateRows(t, path, agent, clearRows(), "a"); err != nil {
		t.Fatalf("a line for model a: %v\n%s", err, r)
	}
	if _, err := calibrateRows(t, path, agent, noSignalRows(), "b"); err == nil {
		t.Fatal("model b is refused")
	}
	if n := skippedOf(agent, "needs-plan", 100); n != 0 {
		t.Fatalf("model a has a line, so the question is asked on every request: skipped %d of 100", n)
	}
}

// A later refused calibration of the same model keeps its earlier line in the spec: no stop is kept, and status keeps saying calibrated.
func TestNoStopWhileTheQuestionHasALine(t *testing.T) {
	agent, path := hookStopAgent(t)
	if r, err := calibrateRows(t, path, agent, clearRows(), "a"); err != nil {
		t.Fatalf("a line: %v\n%s", err, r)
	}
	if _, err := calibrateRows(t, path, agent, noSignalRows(), "a"); err == nil {
		t.Fatal("refused")
	}
	sp := mustLoad(t, path)
	if _, ok := readStopped(agent, sp); ok || skippedOf(agent, "needs-plan", 100) != 0 {
		t.Fatal("a question with a line is not stopped")
	}
	if _, err := os.Stat(stoppedPath(agent, "needs-plan", sp.Hash())); !os.IsNotExist(err) {
		t.Fatalf("no stop is kept for a question with a line: %v", err)
	}
}

// Trying another wording of the same question (tune calibrates <id>-v2.yaml) keeps the current wording's stop.
func TestStopOfAnotherWordingKeepsTheCurrentStop(t *testing.T) {
	agent, path := hookStopAgent(t)
	if _, err := calibrateRows(t, path, agent, noSignalRows(), "a"); err == nil {
		t.Fatal("refused")
	}
	src, _ := os.ReadFile(path)
	v2 := filepath.Join(t.TempDir(), "needs-plan-v2.yaml")
	if err := os.WriteFile(v2, bytes.Replace(src, []byte("Same case?"), []byte("Is it the same case?"), 1), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := calibrateRows(t, v2, agent, noSignalRows(), "a"); err == nil {
		t.Fatal("the other wording is refused too")
	}
	if _, ok := readStopped(agent, mustLoad(t, path)); !ok {
		t.Fatal("the current wording stays stopped")
	}
}

// A line on the context rung keeps the question asked even when the minimal rung was stopped.
func TestStopOnTheMinimalRungLeavesAContextLineUsed(t *testing.T) {
	agent, path := hookStopAgent(t)
	src, _ := os.ReadFile(path)
	if err := os.WriteFile(path, bytes.Replace(src, []byte("  required: [a]\n"), []byte("  required: [a]\n  context: \"{{ a }} {{ conversation }}\"\n"), 1), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := calibrateRows(t, path, agent, noSignalRows(), "a"); err == nil {
		t.Fatal("the minimal rung is refused")
	}
	sp := mustLoad(t, path)
	if _, ok := readStopped(agent, sp); !ok {
		t.Fatal("the minimal rung's stop is kept")
	}
	sp.PutCalibration(spec.Calibration{Model: "a", SpecHash: sp.ContextSpec().Hash(), Method: "identity", N: 100, PrecisionAt: 0.9, FittedAt: "2026-10-28"})
	if err := sp.Save(); err != nil {
		t.Fatal(err)
	}
	if n := skippedOf(agent, "needs-plan", 100); n != 0 {
		t.Fatalf("the context rung has a line, so the question is asked: skipped %d of 100", n)
	}
	if _, s, _ := currentLineState(agent, mustLoad(t, path)); s.Kind == lineStopped {
		t.Fatal("status does not call a question with a line stopped")
	}
}

// Only the hook's questions are stopped: the hook is what asks a stopped question on 1 request in 20, so a question asked elsewhere
// (the wiki's, a person's own) is never called stopped.
func TestOnlyTheHooksQuestionsAreStopped(t *testing.T) {
	dir := t.TempDir()
	path := writeNoulSpec(t, dir)
	storeDir := filepath.Join(dir, "store")
	if _, err := calibrateRows(t, path, storeDir, noSignalRows()); err == nil {
		t.Fatal("refused")
	}
	if entries, _ := os.ReadDir(storeDir); len(entries) > 0 {
		for _, e := range entries {
			if strings.HasPrefix(e.Name(), "stopped-") {
				t.Fatalf("no stop for a question the hook does not ask: %s", e.Name())
			}
		}
	}
}

// The page says stopped for a stopped hook question.
func TestStatusAPISaysAHookQuestionIsStopped(t *testing.T) {
	repo := gitRepo(t)
	hookEnv(t, nil)
	initAt(t, repo)
	root := filepath.Join(repo, ".shikii")
	sp := mustLoad(t, filepath.Join(root, "decisions", "needs-plan.yaml"))
	st := store.Store{Dir: root}
	if err := st.AppendLabels([]store.Label{{TraceID: "y1", SpecID: sp.ID, SpecHash: sp.Hash(), Type: "noul", Model: "m1", InputsHash: "y1", Truth: "yes", Weight: 1, Source: "ui", At: time.Now()}}); err != nil {
		t.Fatal(err)
	}
	stop, _ := json.Marshal(stoppedLine{Model: "m1", SpecHash: sp.Hash(), AUC: 0.5, Low: 0.3, High: 0.6})
	if err := writeStop(root, "needs-plan", stop); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	newServer(serveOpts{storeDir: root, specsDir: filepath.Join(root, "decisions"), projectScope: repo}).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/status", nil))
	var body struct {
		Specs []struct {
			ID   string           `json:"id"`
			Keys []map[string]any `json:"keys"`
		} `json:"specs"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	for _, s := range body.Specs {
		if s.ID == "needs-plan" {
			if len(s.Keys) != 1 || s.Keys[0]["state"] != "stopped" {
				t.Fatalf("the page says stopped: %+v", s.Keys)
			}
			return
		}
	}
	t.Fatal("needs-plan is on the page")
}
