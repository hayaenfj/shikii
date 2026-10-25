package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/hayaenfj/shikii/internal/spec"
	"github.com/hayaenfj/shikii/internal/store"
	"github.com/hayaenfj/shikii/internal/systemone"
)

// A context-sensitive fake model: "fix …" is unsure with the text alone (0.65). With the previous exchange (Previous turn) it is confident (0.9). "why …" is confident with the text alone.
func contextModel(t *testing.T) (*httptest.Server, *int32) {
	t.Helper()
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req systemone.Request
		_ = json.NewDecoder(r.Body).Decode(&req)
		atomic.AddInt32(&calls, 1)
		probs := map[string]float64{"change_request": 0.65, "question": 0.3, "other": 0.05}
		switch {
		case strings.Contains(req.State, "why"):
			probs = map[string]float64{"change_request": 0.03, "question": 0.95, "other": 0.02}
		case strings.Contains(req.State, "Previous turn"):
			probs = map[string]float64{"change_request": 0.9, "question": 0.08, "other": 0.02}
		}
		top, _ := argmax(probs)
		resp := systemone.Response{Model: "m", Answers: map[string]systemone.Answer{}}
		for k := range req.Questions {
			resp.Answers[k] = systemone.Answer{Type: "choice", Choice: top, Probabilities: probs, Confidence: 0.9}
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(srv.Close)
	return srv, &calls
}

// writeContextSpec puts state.context (a layout that adds the previous exchange) into the intent spec and places it in decisions/.
func writeContextSpec(t *testing.T, dir string) string {
	t.Helper()
	sub := filepath.Join(dir, "decisions")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	path := writeSpec(t, sub, "false")
	b, _ := os.ReadFile(path)
	src := strings.Replace(string(b), "  required: [message]\n", "  required: [message]\n  context: \"Previous turn: {{ previous }}\\nMessage: {{ message }}\"\n", 1)
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func readLabels(t *testing.T, storeDir, specID string) []store.Label {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(storeDir, "labels", specID+".jsonl"))
	if err != nil {
		return nil
	}
	var out []store.Label
	for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		var l store.Label
		if err := json.Unmarshal([]byte(line), &l); err != nil {
			t.Fatal(err)
		}
		out = append(out, l)
	}
	return out
}

// Only when unsure, add context and ask again; an answer that becomes confident there is kept as the truth for the minimal question (source context, weight = probability).
// When a person answers, that truth goes on both rungs. status shows counts per rung and the person's decisions per 1000 decisions.
func TestDecideWritesTheContextAnswerAsTruthForTheMinimalRung(t *testing.T) {
	dir := t.TempDir()
	srv, calls := contextModel(t)
	specPath := writeContextSpec(t, dir)
	sp := mustLoad(t, specPath)
	sp.PutCalibration(spec.Calibration{Model: "m", SpecHash: sp.Hash(), Method: "identity", N: 2, FittedAt: "2026-10-18"})
	sp.PutCalibration(spec.Calibration{Model: "m", SpecHash: sp.ContextHash(), Method: "identity", N: 2, FittedAt: "2026-10-18"})
	if err := sp.Save(); err != nil {
		t.Fatal(err)
	}
	storeDir := filepath.Join(dir, ".shikii")
	out := capture(t, func() error {
		return cmdDecide([]string{specPath, "-i", "message=fix a", "-i", "previous=I renamed the module", "--local", srv.URL, "--store", storeDir})
	})
	if !strings.Contains(out, "band=act top=change_request p=0.900") || !strings.Contains(out, "rung=context") || !strings.Contains(out, "p=0.650 band=ask") || *calls != 2 {
		t.Fatalf("the unsure minimal answer must be asked again with context (2 calls) and say so:\n%s (calls=%d)", out, *calls)
	}
	trace := traceRe.FindStringSubmatch(out)[1]
	labels := readLabels(t, storeDir, "intent")
	if len(labels) != 1 {
		t.Fatalf("the confident context answer must become one truth for the minimal rung, got %d labels", len(labels))
	}
	l := labels[0]
	if l.Source != "context" || l.Rung != "minimal" || l.SpecHash != sp.Hash() || l.Truth != "change_request" || l.Weight != 0.9 || l.Raw["change_request"] != 0.65 || l.TraceID != trace {
		t.Fatalf("the machine truth carries the minimal rung's hash and raw, the context rung's answer and probability: %+v", l)
	}
	// The person's answer goes on both rungs, and since it is the same input, the newer one (the person) beats the machine truth
	out = capture(t, func() error { return cmdLabel([]string{trace, "question", "--store", storeDir, "--source", "ask"}) })
	if strings.Count(out, "labeled ") != 2 || !strings.Contains(out, "context rung") || !strings.Contains(out, "minimal rung") {
		t.Fatalf("a human answer on a context decision must label both rungs:\n%s", out)
	}
	labels = readLabels(t, storeDir, "intent")
	if len(labels) != 3 || labels[1].SpecHash != sp.ContextHash() || labels[1].Raw["change_request"] != 0.9 || labels[2].SpecHash != sp.Hash() || labels[2].Raw["change_request"] != 0.65 || labels[1].Truth != "question" || labels[2].Truth != "question" {
		t.Fatalf("labels: %+v", labels)
	}
	// A decision without context stops at the minimal rung and goes to the person
	capture(t, func() error {
		return cmdDecide([]string{specPath, "-i", "message=fix b", "--local", srv.URL, "--store", storeDir})
	})
	out = capture(t, func() error { return cmdStatus([]string{specPath, "--store", storeDir}) })
	for _, must := range []string{"settled by the minimal question 0, by added context 1, left to a person 1 (500 per 1000 decisions)", "(context rung)  calibrated: identity"} {
		if !strings.Contains(out, must) {
			t.Fatalf("status must show where decisions settle and the context rung's line; missing %q:\n%s", must, out)
		}
	}
	if strings.Contains(out, "stale hash") {
		t.Fatalf("the context rung's hash is live, not stale:\n%s", out)
	}
}

// calibrate draws a line per rung: one from the minimal question's truths, one from the context rung's truths. check verifies both and passes.
func TestCalibrateFitsEachRungOnItsOwnTruths(t *testing.T) {
	dir := t.TempDir()
	srv, _ := contextModel(t)
	specPath := writeContextSpec(t, dir)
	storeDir := filepath.Join(dir, ".shikii")
	// While there is no line, even a confident answer is ask, so every decision goes up to the context rung and to the person. The person's answers become truths for both rungs
	for _, x := range []string{"a", "b", "c"} {
		for verb, truth := range map[string]string{"fix": "change_request", "why": "question"} {
			out := capture(t, func() error {
				return cmdDecide([]string{specPath, "-i", "message=" + verb + " " + x, "-i", "previous=context", "--local", srv.URL, "--store", storeDir})
			})
			if !strings.Contains(out, "rung=context") || !strings.Contains(out, "band=ask") {
				t.Fatalf("uncalibrated: both rungs ask, the decision still goes to a person:\n%s", out)
			}
			trace := traceRe.FindStringSubmatch(out)[1]
			capture(t, func() error { return cmdLabel([]string{trace, truth, "--store", storeDir}) })
		}
	}
	out := capture(t, func() error {
		return cmdCalibrate([]string{specPath, "--local", srv.URL, "--store", storeDir, "--write", "--labeled", "none"})
	})
	sp := mustLoad(t, specPath)
	if !strings.Contains(out, "context rung") || len(sp.Calibration) != 2 {
		t.Fatalf("calibrate must fit a line per rung:\n%s\n%+v", out, sp.Calibration)
	}
	hashes := map[string]bool{}
	for _, c := range sp.Calibration {
		hashes[c.SpecHash] = true
		if c.Model != "m" || c.N != 6 {
			t.Fatalf("each line is fitted on its own rung's 6 truths: %+v", c)
		}
	}
	if !hashes[sp.Hash()] || !hashes[sp.ContextHash()] {
		t.Fatalf("one line per rung: %+v", sp.Calibration)
	}
	out = capture(t, func() error { return cmdCheck([]string{specPath, "--store", storeDir}) })
	if !strings.Contains(out, "ok   intent") || !strings.Contains(out, "calibrations=2") {
		t.Fatalf("check must accept the context rung's line as live:\n%s", out)
	}
	// A line was drawn, so the next decision does not go to the person. Either the minimal rung, which learned from 6 truths that "change_request at 0.65 is right", gives act, or the context rung does
	out = capture(t, func() error {
		return cmdDecide([]string{specPath, "-i", "message=fix d", "-i", "previous=context", "--local", srv.URL, "--store", storeDir})
	})
	if !strings.Contains(out, "band=act top=change_request") {
		t.Fatalf("with both lines the decision settles without a person:\n%s", out)
	}
	out = capture(t, func() error { return cmdStatus([]string{specPath, "--store", storeDir}) })
	if !strings.Contains(out, "left to a person 6 (857 per 1000 decisions)") {
		t.Fatalf("status counts the 6 uncalibrated decisions that went to a person and the 1 that did not:\n%s", out)
	}
}

// tune scores wording candidates only on the minimal question rung. Even if the bank's text has context inputs, it does not go up the rungs.
func TestTuneScoresCandidatesOnTheMinimalRungOnly(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	var contextCalls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req systemone.Request
		_ = json.NewDecoder(r.Body).Decode(&req)
		if strings.Contains(req.State, "Previous turn") {
			atomic.AddInt32(&contextCalls, 1)
		}
		resp := systemone.Response{Model: "m", Answers: map[string]systemone.Answer{}}
		for k, q := range req.Questions {
			probs := map[string]float64{"change_request": 0.65, "question": 0.3, "other": 0.05} // an unsure answer: the conditions to go up a rung are met
			if strings.Contains(q.Instructions, "strict") && strings.Contains(req.State, "why") {
				probs = map[string]float64{"change_request": 0.1, "question": 0.9, "other": 0}
			}
			top, _ := argmax(probs)
			resp.Answers[k] = systemone.Answer{Type: "choice", Choice: top, Probabilities: probs, Confidence: 0.9}
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(srv.Close)
	specPath := specWithVariants(t, dir)
	b, _ := os.ReadFile(specPath)
	src := strings.Replace(string(b), "  required: [message]\n", "  required: [message]\n  context: \"Previous turn: {{ previous }}\\nMessage: {{ message }}\"\n", 1)
	if err := os.WriteFile(specPath, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	rows := regexp.MustCompile(`"message":"([^"]+)"`).ReplaceAllString(bankRows(), `"message":"$1","previous":"p"`)
	labeledPath := filepath.Join(dir, "rows.json")
	if err := os.WriteFile(labeledPath, []byte(rows), 0o644); err != nil {
		t.Fatal(err)
	}
	storeDir := filepath.Join(dir, ".shikii")
	out := capture(t, func() error {
		return cmdTune([]string{specPath, "--labeled", labeledPath, "--local", srv.URL, "--store", storeDir})
	})
	if contextCalls != 0 || !strings.Contains(out, "winner: strict") {
		t.Fatalf("tune must never climb to the context rung while scoring (%d context calls):\n%s", contextCalls, out)
	}
	// The adopted version keeps context. calibrate at adoption asks again and draws the context rung's line too, since the text has context (this measures that rung rather than going up a rung)
	out = capture(t, func() error {
		return cmdTune([]string{specPath, "--labeled", labeledPath, "--local", srv.URL, "--store", storeDir, "--write"})
	})
	v2 := mustLoad(t, filepath.Join(dir, "decisions", "intent-v2.yaml"))
	if !v2.HasContext() || !strings.Contains(out, "context rung") || contextCalls == 0 {
		t.Fatalf("the adopted version must keep state.context and get a line for its context rung from the replayed truths:\n%s\n%+v", out, v2.State)
	}
}

// serve: a decision with added context looks so on the page and is not old wording (stale). The person's answer goes on both rungs, and status shows counts per rung.
func TestServeShowsTheRungAndLabelsBothRungs(t *testing.T) {
	dir := t.TempDir()
	srv, _ := contextModel(t)
	writeContextSpec(t, dir)
	storeDir := filepath.Join(dir, ".shikii")
	var ef endpointFlags
	ef.model, ef.local = "m", srv.URL
	h := newServer(serveOpts{specsDir: filepath.Join(dir, "decisions"), storeDir: storeDir, ef: ef})
	s := httptest.NewServer(h)
	t.Cleanup(s.Close)

	code, d := postJSON(t, s.URL+"/v1/decide", map[string]any{"spec": "intent", "inputs": map[string]string{"message": "fix a", "previous": "I renamed the module"}})
	if code != 200 || d["rung"] != "context" || d["minimal"] == nil {
		t.Fatalf("decide: %d %v", code, d)
	}
	var q struct {
		Items []map[string]any `json:"items"`
	}
	getJSON(t, s.URL+"/v1/queue?spec=intent", &q)
	if len(q.Items) != 1 || q.Items[0]["rung"] != "context" || q.Items[0]["stale"] != false {
		t.Fatalf("the queue must say the decision was asked again with context and is not stale: %+v", q.Items)
	}
	code, l := postJSON(t, s.URL+"/v1/label", map[string]any{"trace_id": d["trace_id"], "spec": "intent", "truth": "question", "source": "ui"})
	if code != 200 || l["rungs"] != 2.0 {
		t.Fatalf("label must reach both rungs: %d %v", code, l)
	}
	if labels := readLabels(t, storeDir, "intent"); len(labels) != 2 || labels[0].Rung != "context" || labels[1].Rung != "minimal" {
		t.Fatalf("labels: %+v", labels)
	}
	var st struct {
		Specs []struct {
			Keys   []map[string]any `json:"keys"`
			Funnel map[string]any   `json:"funnel"`
		} `json:"specs"`
	}
	getJSON(t, s.URL+"/v1/status", &st)
	if len(st.Specs) != 1 || len(st.Specs[0].Keys) != 2 || st.Specs[0].Keys[0]["rung"] != "minimal" || st.Specs[0].Keys[1]["rung"] != "context" || st.Specs[0].Keys[1]["state"] != "collecting" {
		t.Fatalf("status must list a key per rung, both live: %+v", st.Specs)
	}
	if f := st.Specs[0].Funnel; f["human"] != 1.0 || f["human_per_thousand"] != 1000.0 {
		t.Fatalf("funnel: %+v", f)
	}
}
