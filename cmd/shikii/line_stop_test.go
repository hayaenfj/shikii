package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

func calibrateRows(t *testing.T, path, storeDir string, rows []map[string]any) (string, error) {
	t.Helper()
	file := filepath.Join(filepath.Dir(path), "labels.json")
	data, _ := json.Marshal(rows)
	if err := os.WriteFile(file, data, 0o600); err != nil {
		t.Fatal(err)
	}
	var report bytes.Buffer
	err := runCalibrate(path, calibrateOpts{output: &report, storeDir: storeDir, labeled: file, write: true})
	return report.String(), err
}

// A refused calibration with enough truths and no acceptable signal keeps the stop with the measured AUC; a line drawn later removes it.
func TestCalibrateKeepsTheStopOfAQuestionThatCannotTellTheAnswersApart(t *testing.T) {
	dir := t.TempDir()
	path := writeNoulSpec(t, dir)
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
	if _, err := os.Stat(stoppedPath(storeDir, "same")); !os.IsNotExist(err) {
		t.Fatalf("a line drawn later removes the stop: %v", err)
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
			path := writeNoulSpec(t, dir)
			src, _ := os.ReadFile(path)
			if err := os.WriteFile(path, bytes.Replace(src, []byte("min_n: 4"), []byte(tc.minN), 1), 0o644); err != nil {
				t.Fatal(err)
			}
			storeDir := filepath.Join(dir, "store")
			if _, err := calibrateRows(t, path, storeDir, tc.rows); err == nil {
				t.Fatal("no line for these truths")
			}
			if _, err := os.Stat(stoppedPath(storeDir, "same")); !os.IsNotExist(err) {
				t.Fatalf("not a stop: %v", err)
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
	path := writeNoulSpec(t, filepath.Join(agent, "decisions"))
	sp := mustLoad(t, path)
	b, _ := json.Marshal(stoppedLine{Model: "m", SpecHash: sp.Hash(), AUC: 0.5, Low: 0.4, High: 0.6, Pos: 360, Neg: 40, At: time.Now()})
	if err := os.WriteFile(stoppedPath(agent, "same"), b, 0o600); err != nil {
		t.Fatal(err)
	}
	asked := 0
	for i := 0; i < 400; i++ {
		inputs := map[string]string{"a": fmt.Sprint(i)}
		if !hookSkipsStopped(agent, "same", inputs) {
			asked++
			if !stoppedAsk("same", inputs) {
				t.Fatalf("only the requests stoppedAsk picks are asked: %v", inputs)
			}
		}
	}
	if asked == 0 || asked == 400 {
		t.Fatalf("some requests are still asked, most are not: %d of 400", asked)
	}
	resumeOnNewModel(agent, "same", "m")
	if _, ok := readStopped(agent, sp); !ok {
		t.Fatal("an answer from the same model keeps the stop")
	}
	resumeOnNewModel(agent, "same", "m2")
	if _, ok := readStopped(agent, sp); ok {
		t.Fatal("an answer from another model removes the stop")
	}
	other, _ := json.Marshal(stoppedLine{Model: "m", SpecHash: "another-wording"})
	if err := os.WriteFile(stoppedPath(agent, "same"), other, 0o600); err != nil {
		t.Fatal(err)
	}
	if hookSkipsStopped(agent, "same", map[string]string{"a": "1"}) || hookSkipsStopped(agent, "same", map[string]string{"a": "2"}) {
		t.Fatal("a stop measured on another wording does not stop the current one")
	}
}

// status and the line state say the question is stopped and why, for the model it was measured on only.
func TestLineStateSaysAQuestionIsStopped(t *testing.T) {
	agent := t.TempDir()
	if err := os.MkdirAll(filepath.Join(agent, "decisions"), 0o755); err != nil {
		t.Fatal(err)
	}
	path := writeNoulSpec(t, filepath.Join(agent, "decisions"))
	sp := mustLoad(t, path)
	st := store.Store{Dir: agent}
	for i := 0; i < 6; i++ {
		truth := "yes"
		if i%2 == 1 {
			truth = "no"
		}
		if err := st.AppendLabel(store.Label{TraceID: fmt.Sprintf("%016x", i), SpecID: "same", SpecHash: sp.Hash(), Model: "m", InputsHash: fmt.Sprint(i), Raw: map[string]float64{"yes": 0.5}, Truth: truth, Weight: 1, Source: "ui", At: time.Now()}); err != nil {
			t.Fatal(err)
		}
	}
	b, _ := json.Marshal(stoppedLine{Model: "m", SpecHash: sp.Hash(), AUC: 0.35, Low: 0.19, High: 0.51, Pos: 3, Neg: 3})
	if err := os.WriteFile(stoppedPath(agent, "same"), b, 0o600); err != nil {
		t.Fatal(err)
	}
	_, s, ok := currentLineState(agent, sp)
	if !ok || s.Kind != lineStopped || !strings.Contains(s.statusNote(sp, sp.Eval.MinN), "stopped: the probabilities do not tell the answers apart (AUC 0.35, 95% 0.19-0.51") {
		t.Fatalf("the line state is stopped with the reason: %+v %q", s, s.statusNote(sp, sp.Eval.MinN))
	}
	other, _ := json.Marshal(stoppedLine{Model: "m2", SpecHash: sp.Hash()})
	if err := os.WriteFile(stoppedPath(agent, "same"), other, 0o600); err != nil {
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
	sp := mustLoad(t, writeNoulSpec(t, filepath.Join(agent, "decisions")))
	b, _ := json.Marshal(stoppedLine{Model: "m", SpecHash: sp.Hash(), AUC: 0.5, Low: 0.4, High: 0.6})
	if err := os.WriteFile(stoppedPath(agent, "same"), b, 0o600); err != nil {
		t.Fatal(err)
	}
	var skipped, picked map[string]string
	for i := 0; skipped == nil || picked == nil; i++ { // the first input of each kind (stoppedAsk is a fixed hash, so these are always the same)
		in := map[string]string{"a": fmt.Sprint(i)}
		if stoppedAsk("same", in) {
			picked = in
		} else if skipped == nil {
			skipped = in
		}
	}
	calls = 0 // only the calls below count (setting up the project may reach the model)
	ds, _, err := hookDecideAll(agent, []string{"same"}, skipped, "p", nil)
	if err != nil || ds[0] != nil || calls != 0 {
		t.Fatalf("a stopped question is not asked: d=%v calls=%d err=%v", ds[0], calls, err)
	}
	ds, _, err = hookDecideAll(agent, []string{"same"}, picked, "p", nil)
	if err != nil || ds[0] == nil || calls != 1 {
		t.Fatalf("1 request in 20 still asks it: d=%v calls=%d err=%v", ds[0], calls, err)
	}
}
