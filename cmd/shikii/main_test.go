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
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/calib"
	"github.com/hayaenfj/shikii/internal/spec"
	"github.com/hayaenfj/shikii/internal/store"
	"github.com/hayaenfj/shikii/internal/systemone"
)

// fakeSystemOne returns a fixed choice answer.
func fakeSystemOne(t *testing.T, model string, probs map[string]float64) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req systemone.Request
		_ = json.NewDecoder(r.Body).Decode(&req)
		top, _ := argmax(probs)
		resp := systemone.Response{Model: model, Answers: map[string]systemone.Answer{}}
		for k := range req.Questions {
			resp.Answers[k] = systemone.Answer{Type: "choice", Choice: top, Probabilities: probs, Confidence: 0.9}
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func writeSpec(t *testing.T, dir, pii string) string {
	path := filepath.Join(dir, "intent.yaml")
	src := `id: intent
version: 1
type: choice
question: What is asked?
options:
  - id: change_request
    description: wants a change
  - id: question
    description: wants an answer
  - id: other
    description: none
state:
  template: "{{ message }}"
  required: [message]
policy:
  send_to: [local]
  pii: ` + pii + `
thresholds:
  act: 0.75
  ask: 0.60
eval:
  min_n: 2
`
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func capture(t *testing.T, fn func() error) string {
	var buf bytes.Buffer
	old := stdout
	stdout = &buf
	defer func() { stdout = old }()
	if err := fn(); err != nil {
		t.Fatalf("%v\n%s", err, buf.String())
	}
	return buf.String()
}

var traceRe = regexp.MustCompile(`trace=([0-9a-f]{16})`)

// The decide → record → label → calibrate (verify) loop closes. The text is kept or not according to pii.
func TestDecideRecordLabelCalibrateLoop(t *testing.T) {
	dir := t.TempDir()
	storeDir := filepath.Join(dir, ".shikii")
	srv := fakeSystemOne(t, "kev-4b", map[string]float64{"change_request": 0.9, "question": 0.1, "other": 0})

	// Even with pii: true, the text is kept in the record (what is sent outside is a separate matter)
	specPossible := writeSpec(t, dir, "true")
	out := capture(t, func() error {
		return cmdDecide([]string{specPossible, "--local", srv.URL, "--store", storeDir, "-i", "message=fix the typo"})
	})
	if !strings.Contains(out, "band=ask") || !strings.Contains(out, "top=change_request") {
		t.Fatalf("decide output: %s", out)
	}
	m := traceRe.FindStringSubmatch(out)
	if m == nil {
		t.Fatalf("no trace id in output: %s", out)
	}
	trace1 := m[1]
	rec, err := (store.Store{Dir: storeDir}).FindRecord("intent", trace1)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Inputs["message"] != "fix the typo in README.md" && rec.Inputs["message"] == "" {
		t.Fatalf("text must be kept in the record regardless of pii, got %v", rec.Inputs)
	}
	if rec.InputsHash == "" || rec.Model != "kev-4b" || rec.Raw["change_request"] != 0.9 {
		t.Fatalf("record: %+v", rec)
	}

	// With pii: false the text is kept as well
	_ = os.MkdirAll(filepath.Join(dir, "never"), 0o755)
	specNever := writeSpec(t, filepath.Join(dir, "never"), "false")
	out = capture(t, func() error {
		return cmdDecide([]string{specNever, "--local", srv.URL, "--store", storeDir, "-i", "message=why is the sky blue"})
	})
	trace2 := traceRe.FindStringSubmatch(out)[1]
	rec2, err := (store.Store{Dir: storeDir}).FindRecord("intent", trace2)
	if err != nil {
		t.Fatal(err)
	}
	if rec2.Inputs["message"] != "why is the sky blue" {
		t.Fatalf("pii=false must store inputs too, got %v", rec2.Inputs)
	}

	// label: the first one is right, the second wrong (the model said change_request but question is the truth)
	capture(t, func() error {
		return cmdLabel([]string{"--store", storeDir, "--source", "ask", trace1, "change_request"})
	})
	capture(t, func() error { return cmdLabel([]string{"--store", storeDir, "--source", "ask", trace2, "question"}) })
	if err := cmdLabel([]string{"--store", storeDir, trace2, "operation"}); err == nil {
		t.Fatal("label with an option the model was not asked about must be rejected")
	}
	if err := cmdLabel([]string{"--store", storeDir, "deadbeefdeadbeef", "question"}); err == nil {
		t.Fatal("label for unknown trace must be rejected")
	}

	// calibrate (verify): both have p_top=0.9 ≥ act, 1/2 correct → precision 0.5 < 0.75 → identity is not written
	out = capture(t, func() error {
		return cmdCalibrate([]string{specPossible, "--store", storeDir, "--model", "kev-4b"})
	})
	if !strings.Contains(out, "n=2 (file=0 store=2 replay=0)") || !strings.Contains(out, "top-1 accuracy=0.500") {
		t.Fatalf("calibrate output: %s", out)
	}
	if err := cmdCalibrate([]string{specPossible, "--store", storeDir, "--model", "kev-4b", "--write"}); err == nil {
		t.Fatal("precision below act must refuse to write identity calibration")
	}

	// Labels from another model name or another wording are not counted
	out = capture(t, func() error {
		// min_n=2, so we want to see n before it stops for too few → an error is acceptable
		var buf bytes.Buffer
		old := stdout
		stdout = &buf
		defer func() { stdout = old; _, _ = os.Stdout.Write(nil) }()
		err := cmdCalibrate([]string{specPossible, "--store", storeDir, "--model", "jev-1.13.0"})
		stdout = old
		if err == nil || !strings.Contains(err.Error(), "no labels") {
			t.Fatalf("labels for another model must not be used, got err=%v out=%s", err, buf.String())
		}
		return nil
	})
	_ = out
}

// If verification gives precision at or above act, identity is written and the next decide gives act.
func TestVerifiedModelGetsIdentityAndActs(t *testing.T) {
	dir := t.TempDir()
	storeDir := filepath.Join(dir, ".shikii")
	srv := fakeSystemOne(t, "jev-1.13.0", map[string]float64{"change_request": 1, "question": 0, "other": 0})
	specPath := writeSpec(t, dir, "false")

	var traces []string
	for i := 0; i < 3; i++ {
		out := capture(t, func() error {
			return cmdDecide([]string{specPath, "--local", srv.URL, "--store", storeDir, "-i", fmt.Sprintf("message=fix it %d", i)})
		})
		if !strings.Contains(out, "band=ask") {
			t.Fatalf("before verification must be ask: %s", out)
		}
		traces = append(traces, traceRe.FindStringSubmatch(out)[1])
	}
	for _, tr := range traces {
		capture(t, func() error { return cmdLabel([]string{"--store", storeDir, "--source", "ask", tr, "change_request"}) })
	}
	out := capture(t, func() error {
		return cmdCalibrate([]string{specPath, "--store", storeDir, "--model", "jev-1.13.0", "--write"})
	})
	if !strings.Contains(out, "wrote calibration") {
		t.Fatalf("calibrate: %s", out)
	}
	b, _ := os.ReadFile(specPath)
	if !strings.Contains(string(b), "method: identity") || !strings.Contains(string(b), "model: jev-1.13.0") {
		t.Fatalf("spec must carry identity calibration for jev-1.13.0:\n%s", b)
	}
	out = capture(t, func() error {
		return cmdDecide([]string{specPath, "--local", srv.URL, "--store", storeDir, "-i", "message=fix it"})
	})
	if !strings.Contains(out, "band=act") || !strings.Contains(out, "calibrated=true") {
		t.Fatalf("verified model must act: %s", out)
	}
}

func TestParseTruth(t *testing.T) {
	if v, err := parseTruth("noul", json.RawMessage(`true`)); err != nil || v != "yes" {
		t.Fatalf("bool true → yes: %v %v", v, err)
	}
	if v, err := parseTruth("noul", json.RawMessage(`"no"`)); err != nil || v != "no" {
		t.Fatalf("string no: %v %v", v, err)
	}
	if _, err := parseTruth("choice", json.RawMessage(`true`)); err == nil {
		t.Fatal("bool truth for choice must be rejected")
	}
	if v, err := parseTruth("choice", json.RawMessage(`"question"`)); err != nil || v != "question" {
		t.Fatalf("choice string: %v %v", v, err)
	}
}

// check is the contract check: changing the wording makes it stale, a drop in precision makes it FAIL. drift warns about degradation in recent labels.
func TestCheckAndDrift(t *testing.T) {
	dir := t.TempDir()
	storeDir := filepath.Join(dir, ".shikii")
	srv := fakeSystemOne(t, "m1", map[string]float64{"change_request": 0.95, "question": 0.03, "other": 0.02})
	specPath := writeSpec(t, dir, "false")

	var traces []string
	for i := 0; i < 4; i++ {
		out := capture(t, func() error {
			return cmdDecide([]string{specPath, "--local", srv.URL, "--store", storeDir, "-i", fmt.Sprintf("message=fix %d", i)})
		})
		traces = append(traces, traceRe.FindStringSubmatch(out)[1])
	}
	for _, tr := range traces {
		capture(t, func() error { return cmdLabel([]string{"--store", storeDir, "--source", "ask", tr, "change_request"}) })
	}
	capture(t, func() error { return cmdCalibrate([]string{specPath, "--store", storeDir, "--model", "m1", "--write"}) })

	// The contract holds
	out := capture(t, func() error { return cmdCheck([]string{"--store", storeDir, specPath}) })
	if !strings.Contains(out, "ok   intent") {
		t.Fatalf("check must pass: %s", out)
	}

	// Changing the wording fails it as stale
	b, _ := os.ReadFile(specPath)
	edited := strings.Replace(string(b), "question: What is asked?", "question: What is asked? Be strict.", 1)
	if err := os.WriteFile(specPath, []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	old := stdout
	stdout = &buf
	err := cmdCheck([]string{"--store", storeDir, specPath})
	stdout = old
	if err == nil || !strings.Contains(buf.String(), "stale spec_hash") {
		t.Fatalf("editing the question must fail check with stale hash: err=%v out=%s", err, buf.String())
	}
	if err := os.WriteFile(specPath, b, 0o644); err != nil {
		t.Fatal(err)
	}

	// drift: ok if all recent ones are right
	out = capture(t, func() error {
		return cmdDrift([]string{specPath, "--store", storeDir, "--model", "m1", "--window", "10"})
	})
	if !strings.Contains(out, "ok") || !strings.Contains(out, "ask agreement:    1.000") {
		t.Fatalf("drift must be ok: %s", out)
	}

	// More misses (the model keeps saying change_request but the truth is question) → precision drops and it warns
	for i := 0; i < 6; i++ {
		o := capture(t, func() error {
			return cmdDecide([]string{specPath, "--local", srv.URL, "--store", storeDir, "-i", fmt.Sprintf("message=why %d", i)})
		})
		tr := traceRe.FindStringSubmatch(o)[1]
		capture(t, func() error { return cmdLabel([]string{"--store", storeDir, "--source", "ask", tr, "question"}) })
	}
	buf.Reset()
	stdout = &buf
	err = cmdDrift([]string{specPath, "--store", storeDir, "--model", "m1", "--window", "10"})
	stdout = old
	if err == nil || !strings.Contains(buf.String(), "WARNING") || !strings.Contains(buf.String(), "precision at act 0.400") {
		t.Fatalf("drift must warn when recent precision collapses: err=%v out=%s", err, buf.String())
	}
	// check fails on the same labels too (the promise made at calibration does not hold with the current labels)
	buf.Reset()
	stdout = &buf
	err = cmdCheck([]string{"--store", storeDir, specPath})
	stdout = old
	if err == nil || !strings.Contains(buf.String(), "precision at act is 0.400") {
		t.Fatalf("check must fail when labels no longer support act: err=%v out=%s", err, buf.String())
	}
}

// A choice that lacks confidence raw reaches act with vector scaling. Identifier keys follow the same rule.
func TestCalibrateWritesVectorWhenIdentityIsNotEnough(t *testing.T) {
	dir := t.TempDir()
	storeDir := filepath.Join(dir, ".shikii")
	// The truth is always change_request, but the model gives only 0.55 (reproducing Kev-4B's "lack of confidence")
	srv := fakeSystemOne(t, "kev", map[string]float64{"change_request": 0.55, "other": 0.35, "question": 0.10})
	specPath := writeSpec(t, dir, "false")
	for i := 0; i < 6; i++ {
		out := capture(t, func() error {
			return cmdDecide([]string{specPath, "--local", srv.URL, "--store", storeDir, "-i", fmt.Sprintf("message=fix %d", i)})
		})
		if !strings.Contains(out, "band=discard") {
			t.Fatalf("raw 0.55 is below ask: %s", out)
		}
		tr := traceRe.FindStringSubmatch(out)[1]
		capture(t, func() error { return cmdLabel([]string{"--store", storeDir, "--source", "ask", tr, "change_request"}) })
	}
	out := capture(t, func() error {
		return cmdCalibrate([]string{specPath, "--store", storeDir, "--model", "kev", "--write"})
	})
	if !strings.Contains(out, "vector:") || !strings.Contains(out, "wrote calibration") {
		t.Fatalf("calibrate must fit and write vector scaling: %s", out)
	}
	b, _ := os.ReadFile(specPath)
	if !strings.Contains(string(b), "method: vector") || !strings.Contains(string(b), "biases:") {
		t.Fatalf("spec must carry vector calibration:\n%s", b)
	}
	out = capture(t, func() error {
		return cmdDecide([]string{specPath, "--local", srv.URL, "--store", storeDir, "-i", "message=fix"})
	})
	if !strings.Contains(out, "band=act") || !strings.Contains(out, "calibrated=true") {
		t.Fatalf("after vector calibration the same input must act: %s", out)
	}
	out = capture(t, func() error { return cmdCheck([]string{"--store", storeDir, specPath}) })
	if !strings.Contains(out, "ok   intent") {
		t.Fatalf("check must pass for the written vector calibration: %s", out)
	}
}

// fakeByMessage returns a different distribution per state (message).
func fakeByMessage(t *testing.T, model string, table map[string]map[string]float64) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req systemone.Request
		_ = json.NewDecoder(r.Body).Decode(&req)
		probs, ok := table[strings.TrimSpace(req.State)]
		if !ok {
			http.Error(w, "unknown state "+req.State, 400)
			return
		}
		top, _ := argmax(probs)
		resp := systemone.Response{Model: model, Answers: map[string]systemone.Answer{}}
		for k := range req.Questions {
			resp.Answers[k] = systemone.Answer{Type: "choice", Choice: top, Probabilities: probs, Confidence: 0.9}
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// identity also meets precision at act but has low recall. If vector raises recall while keeping precision, choose vector.
func TestCalibratePrefersVectorWhenItRaisesRecall(t *testing.T) {
	dir := t.TempDir()
	storeDir := filepath.Join(dir, ".shikii")
	table := map[string]map[string]float64{}
	// 10 cases: half confident (0.95), half not (0.55, with 0.35 leaking to other). All truths are change_request.
	for i := 0; i < 10; i++ {
		msg := "fix " + string(rune('a'+i))
		if i%2 == 0 {
			table[msg] = map[string]float64{"change_request": 0.95, "other": 0.03, "question": 0.02}
		} else {
			table[msg] = map[string]float64{"change_request": 0.55, "other": 0.35, "question": 0.10}
		}
	}
	srv := fakeByMessage(t, "kev", table)
	specPath := writeSpec(t, dir, "false")
	for msg := range table {
		out := capture(t, func() error {
			return cmdDecide([]string{specPath, "--local", srv.URL, "--store", storeDir, "-i", "message=" + msg})
		})
		tr := traceRe.FindStringSubmatch(out)[1]
		capture(t, func() error { return cmdLabel([]string{"--store", storeDir, "--source", "ask", tr, "change_request"}) })
	}
	out := capture(t, func() error {
		return cmdCalibrate([]string{specPath, "--store", storeDir, "--model", "kev", "--write"})
	})
	if !strings.Contains(out, "at act>=0.75: precision=1.000 recall=0.500") {
		t.Fatalf("raw must show precision 1.0 / recall 0.5: %s", out)
	}
	if !strings.Contains(out, "choose vector: both satisfy act") {
		t.Fatalf("vector must be chosen for higher recall: %s", out)
	}
	b, _ := os.ReadFile(specPath)
	if !strings.Contains(string(b), "method: vector") {
		t.Fatalf("spec must carry vector:\n%s", b)
	}
	// The inputs that lacked confidence reach act
	out = capture(t, func() error {
		return cmdDecide([]string{specPath, "--local", srv.URL, "--store", storeDir, "-i", "message=fix b"})
	})
	if !strings.Contains(out, "band=act") {
		t.Fatalf("after vector the 0.55 input must act: %s", out)
	}
}

func TestRecallAtAndCrossValidation(t *testing.T) {
	probs := []float64{0.9, 0.9, 0.5, 0.5}
	truths := []bool{true, false, true, true}
	w := []float64{1, 1, 1, 1}
	if r := recallAt(0.75, probs, truths, w); math.Abs(r-1.0/3) > 1e-9 {
		t.Fatalf("recall: got %f want 1/3", r)
	}
	if p := precisionAt(0.75, probs, truths, w); p != 0.5 {
		t.Fatalf("precision: got %f want 0.5", p)
	}
	// CV returns 0 and stays quiet with fewer data than folds (it does not print overfitted numbers)
	sp := &spec.Spec{Thresholds: spec.Thresholds{Act: 0.75, Ask: 0.6}}
	if p, r := crossValidateVector(sp, []calib.MultiSample{{Raw: map[string]float64{"a": 0.6, "b": 0.4}, Truth: "a"}}, 5); p != 0 || r != 0 {
		t.Fatalf("too few samples must yield 0,0 got %f %f", p, r)
	}
}

// After calibrating by asking the model about a file's labels, the labels are kept in the store, and check then passes without the model.
// Rerunning with the same file does not count twice.
func TestCalibrateFromFilePersistsLabelsForOfflineCheck(t *testing.T) {
	dir := t.TempDir()
	storeDir := filepath.Join(dir, ".shikii")
	table := map[string]map[string]float64{}
	var rows []map[string]any
	for i := 0; i < 8; i++ {
		msg := "fix " + string(rune('a'+i))
		table[msg] = map[string]float64{"change_request": 0.9, "other": 0.07, "question": 0.03}
		rows = append(rows, map[string]any{"inputs": map[string]string{"message": msg}, "truth": "change_request"})
	}
	labeledPath := filepath.Join(dir, "labeled.json")
	b, _ := json.Marshal(rows)
	if err := os.WriteFile(labeledPath, b, 0o644); err != nil {
		t.Fatal(err)
	}
	srv := fakeByMessage(t, "kev", table)
	specPath := writeSpec(t, dir, "false")

	out := capture(t, func() error {
		return cmdCalibrate([]string{specPath, "--labeled", labeledPath, "--local", srv.URL, "--store", storeDir, "--write"})
	})
	if !strings.Contains(out, "n=8 (file=8 store=0 replay=0)") || !strings.Contains(out, "persisted 8 queried labels") || !strings.Contains(out, "wrote calibration") {
		t.Fatalf("first calibrate: %s", out)
	}
	stored, err := (store.Store{Dir: storeDir}).Labels("intent", mustHash(t, specPath), "kev")
	if err != nil || len(stored) != 8 {
		t.Fatalf("8 labels must be persisted, got %d (%v)", len(stored), err)
	}
	for _, l := range stored {
		if l.At.IsZero() {
			t.Fatalf("persisted labels must carry a timestamp (drift sorts by it): %+v", l)
		}
	}

	// Second run: even with the model stopped, it reuses the store's labels and calibrates with the same count. No double counting, nothing new kept
	srv.Close()
	out = capture(t, func() error {
		return cmdCalibrate([]string{specPath, "--labeled", labeledPath, "--local", srv.URL, "--store", storeDir, "--model", "kev"})
	})
	if !strings.Contains(out, "reused 8 stored labels") || !strings.Contains(out, "n=8 (file=8 store=8 replay=0)") || strings.Contains(out, "persisted") {
		t.Fatalf("re-run must reuse stored labels offline without double counting: %s", out)
	}

	// check also passes on the store's labels (the model is stopped)
	out = capture(t, func() error { return cmdCheck([]string{"--store", storeDir, specPath}) })
	if !strings.Contains(out, "ok   intent") || strings.Contains(out, "FAIL") {
		t.Fatalf("check must pass offline from persisted labels: %s", out)
	}
	// drift also works on the same labels (no labels come from ask, so agreement is 0)
	out = capture(t, func() error { return cmdDrift([]string{specPath, "--store", storeDir, "--model", "kev"}) })
	if !strings.Contains(out, "ok") {
		t.Fatalf("drift must run offline: %s", out)
	}
}

func mustHash(t *testing.T, specPath string) string {
	sp, err := spec.Load(specPath)
	if err != nil {
		t.Fatal(err)
	}
	return sp.Hash()
}

// fakeNoulByMessage returns a fixed p(yes) per state.
func fakeNoulByMessage(t *testing.T, model string, table map[string]float64) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req systemone.Request
		_ = json.NewDecoder(r.Body).Decode(&req)
		p, ok := table[strings.TrimSpace(req.State)]
		if !ok {
			http.Error(w, "unknown state "+req.State, 400)
			return
		}
		resp := systemone.Response{Model: model, Answers: map[string]systemone.Answer{}}
		for k := range req.Questions {
			resp.Answers[k] = systemone.Answer{Type: "noul", Noul: p}
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func writeNoulSpec(t *testing.T, dir string) string {
	path := filepath.Join(dir, "same.yaml")
	src := `id: same
version: 1
type: noul
question: Same case?
state:
  template: "{{ a }}"
  required: [a]
policy:
  send_to: [local]
  pii: false
thresholds:
  act: 0.70
  ask: 0.50
eval:
  min_n: 4
`
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// noul also compares identity and platt: identity for an already calibrated model, platt for an overconfident one.
func TestNoulChoosesIdentityOrPlattByCV(t *testing.T) {
	run := func(t *testing.T, table map[string]float64, truth func(msg string) bool, wantMethod string) string {
		dir := t.TempDir()
		storeDir := filepath.Join(dir, ".shikii")
		srv := fakeNoulByMessage(t, "m", table)
		specPath := writeNoulSpec(t, dir)
		// Map iteration order changes every time. Fix the order of labels (= how CV splits folds)
		msgs := make([]string, 0, len(table))
		for msg := range table {
			msgs = append(msgs, msg)
		}
		sort.Strings(msgs)
		for _, msg := range msgs {
			out := capture(t, func() error {
				return cmdDecide([]string{specPath, "--local", srv.URL, "--store", storeDir, "-i", "a=" + msg})
			})
			tr := traceRe.FindStringSubmatch(out)[1]
			lab := "no"
			if truth(msg) {
				lab = "yes"
			}
			capture(t, func() error { return cmdLabel([]string{"--store", storeDir, "--source", "ask", tr, lab}) })
		}
		out := capture(t, func() error { return cmdCalibrate([]string{specPath, "--store", storeDir, "--model", "m", "--write"}) })
		b, _ := os.ReadFile(specPath)
		if !strings.Contains(string(b), "method: "+wantMethod) {
			t.Fatalf("want %s, spec:\n%s\nout:\n%s", wantMethod, b, out)
		}
		return out
	}

	// Already calibrated: positives around 0.9, negatives around 0.1 → platt does not improve, identity
	calibrated := map[string]float64{}
	for i := 0; i < 10; i++ {
		calibrated[fmt.Sprintf("pos%d", i)] = 0.86 + 0.01*float64(i)
		calibrated[fmt.Sprintf("neg%d", i)] = 0.05 + 0.01*float64(i)
	}
	out := run(t, calibrated, func(m string) bool { return strings.HasPrefix(m, "pos") }, "identity")
	if !strings.Contains(out, "choose identity") {
		t.Fatalf("expected identity: %s", out)
	}

	// Overconfident: both positives and negatives return 0.9 or more, negatives slightly lower → platt rebuilds the threshold and recovers precision
	over := map[string]float64{}
	for i := 0; i < 10; i++ {
		over[fmt.Sprintf("pos%d", i)] = 0.985 + 0.001*float64(i)
		over[fmt.Sprintf("neg%d", i)] = 0.90 + 0.003*float64(i)
	}
	out = run(t, over, func(m string) bool { return strings.HasPrefix(m, "pos") }, "platt")
	if !strings.Contains(out, "choose platt: CV precision") {
		t.Fatalf("expected platt chosen for overconfident model: %s", out)
	}
}

// A partial label "!edit" (the outcome only told us "it was not a change") counts in evaluating precision and is left out of fitting.
func TestPartialLabelsCountAgainstPrecision(t *testing.T) {
	dir := t.TempDir()
	storeDir := filepath.Join(dir, ".shikii")
	srv := fakeSystemOne(t, "m", map[string]float64{"change_request": 0.9, "question": 0.07, "other": 0.03})
	specPath := writeSpec(t, dir, "false")
	var traces []string
	for i := 0; i < 5; i++ {
		out := capture(t, func() error {
			return cmdDecide([]string{specPath, "--local", srv.URL, "--store", storeDir, "-i", fmt.Sprintf("message=do it %d", i)})
		})
		traces = append(traces, traceRe.FindStringSubmatch(out)[1])
	}
	// The model said change_request 0.9 for all. 2 were really changes, 2 were "not a change" (misses),
	// 1 was "not a question" (consistent with change_request, so counted as right)
	capture(t, func() error {
		return cmdLabel([]string{"--store", storeDir, "--source", "outcome", traces[0], "change_request"})
	})
	capture(t, func() error {
		return cmdLabel([]string{"--store", storeDir, "--source", "outcome", traces[1], "change_request"})
	})
	capture(t, func() error {
		return cmdLabel([]string{"--store", storeDir, "--source", "outcome", "--weight", "0.7", traces[2], "!change_request"})
	})
	capture(t, func() error {
		return cmdLabel([]string{"--store", storeDir, "--source", "outcome", "--weight", "0.7", traces[3], "!change_request"})
	})
	capture(t, func() error {
		return cmdLabel([]string{"--store", storeDir, "--source", "outcome", "--weight", "0.7", traces[4], "!question"})
	})
	out := capture(t, func() error { return cmdCalibrate([]string{specPath, "--store", storeDir, "--model", "m"}) })
	if !strings.Contains(out, "labels: 2 full, 3 partial") {
		t.Fatalf("partial labels must be counted separately: %s", out)
	}
	// precision@act = (2 + 0.7) / (2 + 0.7 + 0.7·2) = 0.659 — "!change_request" is a miss, "!question" is a hit
	if !strings.Contains(out, "at act>=0.75: precision=0.659") {
		t.Fatalf("partial labels must be scored as 'anything but x': %s", out)
	}
	if !strings.Contains(out, "WARNING") {
		t.Fatalf("precision below act must warn: %s", out)
	}
}

// score + thresholds.level: the report's precision is also measured with P(level ≥ k) and "truth level ≥ k".
func TestScoreLevelReports(t *testing.T) {
	dir := t.TempDir()
	storeDir := filepath.Join(dir, ".shikii")
	// Distribution 2:0.10 / 3:0.45 / 4:0.45 → top 0.45 (ties go to the smaller key "3"), P(≥3)=0.90
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req systemone.Request
		_ = json.NewDecoder(r.Body).Decode(&req)
		resp := systemone.Response{Model: "m", Answers: map[string]systemone.Answer{}}
		for k := range req.Questions {
			resp.Answers[k] = systemone.Answer{Type: "score", Score: 3.35, Confidence: 0.4,
				Probabilities: map[string]float64{"0": 0, "1": 0, "2": 0.10, "3": 0.45, "4": 0.45}}
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(srv.Close)
	specPath := filepath.Join(dir, "lvl.yaml")
	src := `id: lvl
version: 1
type: score
question: how related?
levels: [unrelated, same domain, circumstantial, directly connected, same case]
state:
  template: "{{ m }}"
  required: [m]
policy:
  send_to: [local]
  pii: false
thresholds:
  act: 0.75
  ask: 0.50
  level: 3
eval:
  min_n: 2
`
	if err := os.WriteFile(specPath, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	var traces []string
	for i := 0; i < 4; i++ {
		out := capture(t, func() error {
			return cmdDecide([]string{specPath, "--local", srv.URL, "--store", storeDir, "-i", fmt.Sprintf("m=x %d", i)})
		})
		if !strings.Contains(out, "band=ask") || !strings.Contains(out, "p=0.900") {
			t.Fatalf("decide must band on P(level>=3)=0.90 (ask, since uncalibrated): %s", out)
		}
		traces = append(traces, traceRe.FindStringSubmatch(out)[1])
	}
	// Truths: 3 are level 4 (event happened), 1 is level 1 (no event)
	for _, tr := range traces[:3] {
		capture(t, func() error { return cmdLabel([]string{"--store", storeDir, "--source", "ask", tr, "4"}) })
	}
	capture(t, func() error { return cmdLabel([]string{"--store", storeDir, "--source", "ask", traces[3], "1"}) })
	out := capture(t, func() error { return cmdCalibrate([]string{specPath, "--store", storeDir, "--model", "m"}) })
	// P(≥3)=0.90 ≥ act puts all 4 on the act side, 3/4 correct → precision 0.750
	if !strings.Contains(out, "at act>=0.75: precision=0.750 recall=1.000") {
		t.Fatalf("score-level precision must be measured on P(level>=k) vs truth>=k: %s", out)
	}
}

// Measuring order bias: a biased model that adds probability to the first option shows flips and position bias; an unbiased model does not.
func TestBiasDetectsOrderSensitivity(t *testing.T) {
	mkServer := func(firstBonus float64) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var req struct {
				Questions map[string]struct {
					Criteria systemone.OrderedCriteria `json:"criteria"`
				} `json:"questions"`
			}
			_ = json.NewDecoder(r.Body).Decode(&req)
			q := req.Questions["q"]
			// Base probabilities: change_request 0.45 / question 0.40 / other 0.15. Add firstBonus to the first one and normalize
			base := map[string]float64{"change_request": 0.45, "question": 0.40, "other": 0.15}
			probs := map[string]float64{}
			var sum float64
			for pos, c := range q.Criteria {
				p := base[c.ID]
				if pos == 0 {
					p += firstBonus
				}
				probs[c.ID] = p
				sum += p
			}
			for k := range probs {
				probs[k] /= sum
			}
			top, _ := argmax(probs)
			resp := systemone.Response{Model: "m", Answers: map[string]systemone.Answer{
				"q": {Type: "choice", Choice: top, Probabilities: probs, Confidence: 0.5},
			}}
			_ = json.NewEncoder(w).Encode(resp)
		}))
	}
	dir := t.TempDir()
	specPath := writeSpec(t, dir, "false")
	var rows []map[string]any
	for i := 0; i < 6; i++ {
		rows = append(rows, map[string]any{"inputs": map[string]string{"message": fmt.Sprintf("m%d", i)}, "truth": "change_request"})
	}
	b, _ := json.Marshal(rows)
	labeledPath := filepath.Join(dir, "rows.json")
	if err := os.WriteFile(labeledPath, b, 0o644); err != nil {
		t.Fatal(err)
	}

	biased := mkServer(0.30)
	defer biased.Close()
	var buf bytes.Buffer
	old := stdout
	stdout = &buf
	err := cmdBias([]string{specPath, "--labeled", labeledPath, "--local", biased.URL, "--permutations", "6", "--rows", "6"})
	stdout = old
	if err == nil || !strings.Contains(buf.String(), "WARNING") {
		t.Fatalf("biased model must be flagged: err=%v\n%s", err, buf.String())
	}
	if !strings.Contains(buf.String(), "queries=42") {
		t.Fatalf("6 rows × (1 + 6 permutations) = 42 queries: %s", buf.String())
	}
	if strings.Contains(buf.String(), "flip rate (top changes with order): 0.000") {
		t.Fatalf("biased model must flip the top for some orders:\n%s", buf.String())
	}
	if !strings.Contains(buf.String(), "pos 1: +0.") {
		t.Fatalf("the first slot must show a positive shift for the first-option-biased model:\n%s", buf.String())
	}

	fair := mkServer(0)
	defer fair.Close()
	out := capture(t, func() error {
		return cmdBias([]string{specPath, "--labeled", labeledPath, "--local", fair.URL, "--permutations", "6", "--rows", "6"})
	})
	if !strings.Contains(out, "flip rate (top changes with order): 0.000") || !strings.Contains(out, "position shift spread (max−min): 0.000") || !strings.Contains(out, "\nok") {
		t.Fatalf("order-insensitive model must pass cleanly:\n%s", out)
	}
}

// status takes stock of records, labels and calibration. Labels under old wording are stale, too few is collecting, enough is ready.
func TestStatusInventory(t *testing.T) {
	dir := t.TempDir()
	storeDir := filepath.Join(dir, ".shikii")
	srv := fakeSystemOne(t, "m", map[string]float64{"change_request": 0.9, "question": 0.1, "other": 0})
	specPath := writeSpec(t, dir, "false") // min_n: 2
	var traces []string
	for i := 0; i < 3; i++ {
		out := capture(t, func() error {
			return cmdDecide([]string{specPath, "--local", srv.URL, "--store", storeDir, "-i", fmt.Sprintf("message=fix %d", i)})
		})
		traces = append(traces, traceRe.FindStringSubmatch(out)[1])
	}
	capture(t, func() error {
		return cmdLabel([]string{"--store", storeDir, "--source", "ask", traces[0], "change_request"})
	})
	out := capture(t, func() error { return cmdStatus([]string{"--store", storeDir, specPath}) })
	if !strings.Contains(out, "intent: records=3 labels=1") || !strings.Contains(out, "collecting (1/2)") || !strings.Contains(out, "by source: ask=1") {
		t.Fatalf("status before enough labels:\n%s", out)
	}
	capture(t, func() error {
		return cmdLabel([]string{"--store", storeDir, "--source", "outcome", traces[1], "!change_request"})
	})
	out = capture(t, func() error { return cmdStatus([]string{"--store", storeDir, specPath}) })
	if !strings.Contains(out, "enough truths: the line is drawn automatically") || !strings.Contains(out, "by truth:  !change_request=1 change_request=1") {
		t.Fatalf("status when enough labels:\n%s", out)
	}
	// After changing the wording, the collected labels show as stale
	b, _ := os.ReadFile(specPath)
	edited := strings.Replace(string(b), "question: What is asked?", "question: What is asked? Be strict.", 1)
	if err := os.WriteFile(specPath, []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	out = capture(t, func() error { return cmdStatus([]string{"--store", storeDir, specPath}) })
	if !strings.Contains(out, "stale hash") {
		t.Fatalf("status must flag labels from the old wording:\n%s", out)
	}
	// Counts show even without passing a spec
	out = capture(t, func() error { return cmdStatus([]string{"--store", storeDir}) })
	if !strings.Contains(out, "intent: records=3 labels=2") || !strings.Contains(out, "spec not given") {
		t.Fatalf("status without spec:\n%s", out)
	}
}

// gc only shows by default. With --apply it deletes only "old records with no label"; labeled ones stay.
func TestGCKeepsLabeledAndIsDryRunByDefault(t *testing.T) {
	dir := t.TempDir()
	storeDir := filepath.Join(dir, ".shikii")
	srv := fakeSystemOne(t, "m", map[string]float64{"change_request": 0.9, "question": 0.1, "other": 0})
	specPath := writeSpec(t, dir, "false")
	var traces []string
	for i := 0; i < 3; i++ {
		out := capture(t, func() error {
			return cmdDecide([]string{specPath, "--local", srv.URL, "--store", storeDir, "-i", fmt.Sprintf("message=fix %d", i)})
		})
		traces = append(traces, traceRe.FindStringSubmatch(out)[1])
	}
	capture(t, func() error { return cmdLabel([]string{"--store", storeDir, traces[0], "change_request"}) })

	// With the default 30d, nothing from today is a target
	out := capture(t, func() error { return cmdGC([]string{"--store", storeDir, specPath}) })
	if !strings.Contains(out, "intent: would remove 0") {
		t.Fatalf("fresh records must not be candidates under the default age:\n%s", out)
	}
	// With age 0, the 2 unlabeled records are candidates. Only shown, not deleted
	out = capture(t, func() error { return cmdGC([]string{"--store", storeDir, "--older-than", "0", specPath}) })
	if !strings.Contains(out, "intent: would remove 2") || !strings.Contains(out, "keep 1 labeled") || !strings.Contains(out, "--apply") {
		t.Fatalf("dry run output:\n%s", out)
	}
	st := capture(t, func() error { return cmdStatus([]string{"--store", storeDir}) })
	if !strings.Contains(st, "intent: records=3 labels=1") {
		t.Fatalf("dry run must not delete anything:\n%s", st)
	}
	// With --apply they are deleted. Labels stay
	out = capture(t, func() error { return cmdGC([]string{"--store", storeDir, "--older-than", "0", "--apply", specPath}) })
	if !strings.Contains(out, "intent: removed 2") {
		t.Fatalf("apply output:\n%s", out)
	}
	st = capture(t, func() error { return cmdStatus([]string{"--store", storeDir}) })
	if !strings.Contains(st, "intent: records=1 labels=1") {
		t.Fatalf("after apply:\n%s", st)
	}
	// Without a spec, every spec in the store is a target
	out = capture(t, func() error { return cmdGC([]string{"--store", storeDir, "--older-than", "0"}) })
	if !strings.Contains(out, "intent: would remove 0") {
		t.Fatalf("gc without spec must still cover the store:\n%s", out)
	}
	// Reject a strange age
	if err := cmdGC([]string{"--store", storeDir, "--older-than", "3 weeks", specPath}); err == nil {
		t.Fatal("unparsable --older-than must be an error")
	}
}

func TestParseAge(t *testing.T) {
	cases := map[string]time.Duration{"30d": 30 * 24 * time.Hour, "12h": 12 * time.Hour, "0": 0, "1w": 7 * 24 * time.Hour}
	for in, want := range cases {
		got, err := parseAge(in)
		if err != nil || got != want {
			t.Fatalf("parseAge(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "-1d", "soon", "3 weeks"} {
		if _, err := parseAge(bad); err == nil {
			t.Fatalf("parseAge(%q) must fail", bad)
		}
	}
}

// label --last attaches to the spec's latest record (no need to note the trace).
func TestLabelLastRecord(t *testing.T) {
	dir := t.TempDir()
	storeDir := filepath.Join(dir, ".shikii")
	srv := fakeSystemOne(t, "m", map[string]float64{"change_request": 0.9, "question": 0.1, "other": 0})
	specPath := writeSpec(t, dir, "false")
	var traces []string
	for _, msg := range []string{"first", "second"} {
		out := capture(t, func() error {
			return cmdDecide([]string{specPath, "--local", srv.URL, "--store", storeDir, "-i", "message=" + msg})
		})
		traces = append(traces, traceRe.FindStringSubmatch(out)[1])
	}
	out := capture(t, func() error {
		return cmdLabel([]string{"--store", storeDir, "--spec-id", "intent", "--last", "question"})
	})
	if !strings.Contains(out, "labeled "+traces[1]) {
		t.Fatalf("--last must label the most recent record (%s), got: %s", traces[1], out)
	}
	if err := cmdLabel([]string{"--store", storeDir, "--last", "question"}); err == nil {
		t.Fatal("--last without --spec-id must be rejected")
	}
	if err := cmdLabel([]string{"--store", storeDir, "--spec-id", "nothing", "--last", "question"}); err == nil {
		t.Fatal("--last on a spec with no records must be rejected")
	}
}

// Changing the wording removes the calibration but not the truths a person gave. With pii: true and the text kept,
// calibrate asks the model again about the same inputs with the new wording (new hash) and redraws the line without a person's help.
func TestCalibrateReplaysTruthsAfterWordingChange(t *testing.T) {
	dir := t.TempDir()
	storeDir := filepath.Join(dir, ".shikii")
	srv := fakeSystemOne(t, "m", map[string]float64{"change_request": 0.9, "question": 0.1, "other": 0})
	specPath := writeSpec(t, dir, "true") // min_n: 2
	var traces []string
	for i := 0; i < 3; i++ {
		out := capture(t, func() error {
			return cmdDecide([]string{specPath, "--local", srv.URL, "--store", storeDir, "-i", fmt.Sprintf("message=fix %d", i)})
		})
		traces = append(traces, traceRe.FindStringSubmatch(out)[1])
	}
	for _, tr := range traces {
		capture(t, func() error { return cmdLabel([]string{"--store", storeDir, "--source", "ask", tr, "change_request"}) })
	}
	oldHash := mustLoad(t, specPath).Hash()

	// Change the wording → the hash changes → there are no labels for the current (hash, model)
	b, _ := os.ReadFile(specPath)
	edited := strings.Replace(string(b), "question: What is asked?", "question: What is asked? Be strict.", 1)
	if err := os.WriteFile(specPath, []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	newHash := mustLoad(t, specPath).Hash()
	if newHash == oldHash {
		t.Fatal("test setup: wording change must change the hash")
	}

	out := capture(t, func() error {
		return cmdCalibrate([]string{specPath, "--local", srv.URL, "--store", storeDir, "--write"})
	})
	if !strings.Contains(out, "replayed 3") || !strings.Contains(out, "wrote calibration") {
		t.Fatalf("calibrate must replay the 3 stored truths against the new wording and write:\n%s", out)
	}
	st := store.Store{Dir: storeDir}
	got, err := st.Labels("intent", newHash, "m")
	if err != nil || len(got) != 3 {
		t.Fatalf("replayed labels must be persisted under the new hash: %d %v", len(got), err)
	}
	for _, l := range got {
		if l.Source != "replay" || l.Truth != "change_request" {
			t.Fatalf("replayed label must keep the human truth and say where it came from: %+v", l)
		}
	}
	// The second run does not re-ask (the new hash already has labels)
	out = capture(t, func() error {
		return cmdCalibrate([]string{specPath, "--local", srv.URL, "--store", storeDir})
	})
	if strings.Contains(out, "replayed") || !strings.Contains(out, "store=3") {
		t.Fatalf("second run must use the persisted labels, not replay again:\n%s", out)
	}
	// check passes without the model (the store has labels for the new hash)
	capture(t, func() error { return cmdCheck([]string{"--store", storeDir, specPath}) })
}

// Truths of records without their text (old ones from before text was kept) cannot be re-asked. Do not silently report 0; give the reason.
func TestCalibrateCannotReplayWithoutStoredText(t *testing.T) {
	dir := t.TempDir()
	storeDir := filepath.Join(dir, ".shikii")
	srv := fakeSystemOne(t, "m", map[string]float64{"change_request": 0.9, "question": 0.1, "other": 0})
	specPath := writeSpec(t, dir, "true")
	sp := mustLoad(t, specPath)
	st := store.Store{Dir: storeDir}
	old := store.Record{TraceID: store.NewTraceID(), SpecID: sp.ID, SpecHash: "old-hash", Type: "choice", Model: "m",
		InputsHash: store.HashInputs(map[string]string{"message": "fix"}), Raw: map[string]float64{"change_request": 0.9, "question": 0.1, "other": 0},
		Top: "change_request", P: 0.9, Band: "ask", At: time.Now()} // no Inputs
	if err := st.AppendRecord(old); err != nil {
		t.Fatal(err)
	}
	l, err := store.LabelFor(&old, "change_request", 1, "manual")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AppendLabel(l); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	o := stdout
	stdout = &buf
	err = cmdCalibrate([]string{specPath, "--local", srv.URL, "--store", storeDir})
	stdout = o
	if err == nil {
		t.Fatal("no labels for the current hash and nothing to replay: calibrate must fail")
	}
	if !strings.Contains(buf.String(), "1 earlier truth") || !strings.Contains(buf.String(), "no text stored") {
		t.Fatalf("calibrate must say how many truths exist but cannot be replayed, and why:\n%s", buf.String())
	}
}

func mustLoad(t *testing.T, path string) *spec.Spec {
	t.Helper()
	sp, err := spec.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	return sp
}

// Flags may also come after positional arguments (the README's list is written in that order). The 5 commands new / status / check / gc / label.
func TestFlagsMayFollowPositionals(t *testing.T) {
	dir := t.TempDir()
	specPath := writeSpec(t, dir, "false")
	storeDir := filepath.Join(dir, ".shikii")
	newDir := filepath.Join(dir, "new")
	if err := os.MkdirAll(newDir, 0o755); err != nil {
		t.Fatal(err)
	}
	capture(t, func() error {
		return cmdNew([]string{"my-decision", "--type", "noul", "--dir", newDir, "--pii", "false", "--send-to", "local", "--question", "Is it?", "--input", "text"})
	})
	if _, err := os.Stat(filepath.Join(newDir, "my-decision.yaml")); err != nil {
		t.Fatalf("new <id> --type ... must work with the id first: %v", err)
	}
	for _, c := range []struct {
		name string
		run  func() error
	}{
		{"status", func() error { return cmdStatus([]string{specPath, "--store", storeDir}) }},
		{"check", func() error { return cmdCheck([]string{specPath, "--store", storeDir}) }},
		{"gc", func() error { return cmdGC([]string{specPath, "--store", storeDir}) }},
	} {
		out := capture(t, c.run)
		if strings.Contains(out, "--store") || strings.Contains(out, "no such file") {
			t.Fatalf("%s <spec> --store DIR must treat --store as a flag, got:\n%s", c.name, out)
		}
	}
	err := cmdLabel([]string{"no-such-trace", "question", "--store", storeDir})
	if err == nil || strings.Contains(err.Error(), "usage") {
		t.Fatalf("label <trace> <truth> --store DIR must parse the flag, got %v", err)
	}
}
