package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/calib"
	"github.com/hayaenfj/shikii/internal/decide"
	"github.com/hayaenfj/shikii/internal/spec"
	"github.com/hayaenfj/shikii/internal/store"
	"github.com/hayaenfj/shikii/internal/systemone"
	"gopkg.in/yaml.v3"
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
	if !strings.Contains(out, "intent: records=3 labels=1") || !regexp.MustCompile(`@ \S+\s+1  collecting \(min_n 2\)`).MatchString(out) || !strings.Contains(out, "by source: ask=1") {
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

// No arguments / hash --help print usage and do not fail. The usage lists the current tools and flags.
func TestUsageMentionsCurrentToolsAndFlags(t *testing.T) {
	u := usageText()
	for _, must := range []string{"shikii_tune", "--proposer", "--propose", "shikii tune", "shikii up", "shikii doctor", "shikii uninstall [--yes]"} {
		if !strings.Contains(u, must) {
			t.Fatalf("usage must mention %q", must)
		}
	}
	if err := cmdHash([]string{"--help"}); err == nil || !strings.Contains(err.Error(), "usage") {
		t.Fatalf("hash --help must print usage, got %v", err)
	}
}

// Reproduction tests for the third audit (2026-10-18).

// Audit 1: a machine truth (source=context) must not turn into source=replay when calibrate asks again (it would be counted as a human truth).
func TestReplayNeverLaundersMachineTruths(t *testing.T) {
	dir := t.TempDir()
	srv, _ := contextModel(t)
	specPath := writeContextSpec(t, dir)
	sp := mustLoad(t, specPath)
	storeDir := filepath.Join(dir, ".shikii")
	st := store.Store{Dir: storeDir}
	in := map[string]string{"message": "fix a", "previous": "ctx"}
	rec := store.Record{TraceID: store.NewTraceID(), SpecID: "intent", SpecHash: sp.ContextHash(), Type: "choice", Model: "m", Inputs: in,
		InputsHash: store.HashInputs(in), Raw: map[string]float64{"change_request": 0.9, "question": 0.08, "other": 0.02}, Top: "change_request", P: 0.9, Band: "act", Rung: "context",
		Minimal: &store.Pass{SpecHash: sp.Hash(), InputsHash: store.HashInputs(map[string]string{"message": "fix a"}), Raw: map[string]float64{"change_request": 0.65, "question": 0.3, "other": 0.05}, Top: "change_request", P: 0.65, Band: "ask"}, At: time.Now()}
	if err := st.AppendRecord(rec); err != nil {
		t.Fatal(err)
	}
	machine := store.Label{TraceID: rec.TraceID, SpecID: "intent", SpecHash: sp.Hash(), Type: "choice", Model: "m", InputsHash: rec.Minimal.InputsHash, Inputs: in,
		Raw: rec.Minimal.Raw, Truth: "change_request", Weight: 0.9, Source: "context", Rung: "minimal", At: time.Now()}
	if err := st.AppendLabel(machine); err != nil {
		t.Fatal(err)
	}
	var buf strings.Builder
	old := stdout
	stdout = &buf
	_ = cmdCalibrate([]string{specPath, "--local", srv.URL, "--store", storeDir, "--labeled", "none"})
	stdout = old
	for _, l := range readLabels(t, storeDir, "intent") {
		if l.Source == "replay" {
			t.Fatalf("a machine truth must not be replayed into a human-looking label: %+v\n%s", l, buf.String())
		}
	}
}

// Audit 2: a line is not written unless there are at least min_n human truths. Machine truths alone only give a report.
func TestCalibrateWriteNeedsHumanTruths(t *testing.T) {
	dir := t.TempDir()
	srv, _ := contextModel(t)
	specPath := writeSpec(t, dir, "false") // min_n 2
	sp := mustLoad(t, specPath)
	storeDir := filepath.Join(dir, ".shikii")
	st := store.Store{Dir: storeDir}
	add := func(i int, source string) {
		in := map[string]string{"message": "fix " + string(rune('a'+i))}
		l := store.Label{TraceID: store.NewTraceID(), SpecID: "intent", SpecHash: sp.Hash(), Type: "choice", Model: "m", InputsHash: store.HashInputs(in), Inputs: in,
			Raw: map[string]float64{"change_request": 0.9, "question": 0.1, "other": 0}, Truth: "change_request", Weight: 1, Source: source, At: time.Now()}
		if err := st.AppendLabel(l); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 3; i++ {
		add(i, "context")
	}
	var buf strings.Builder
	old := stdout
	stdout = &buf
	err := cmdCalibrate([]string{specPath, "--local", srv.URL, "--store", storeDir, "--labeled", "none", "--write", "--model", "m"})
	stdout = old
	if err == nil || !strings.Contains(buf.String(), "human") {
		t.Fatalf("3 machine truths and 0 human truths must not write a line (err=%v):\n%s", err, buf.String())
	}
	if len(mustLoad(t, specPath).Calibration) != 0 {
		t.Fatalf("no line may be written from machine truths alone")
	}
	add(3, "ui")
	add(4, "audit")
	out := capture(t, func() error {
		return cmdCalibrate([]string{specPath, "--local", srv.URL, "--store", storeDir, "--labeled", "none", "--write", "--model", "m"})
	})
	cal := mustLoad(t, specPath).Calibration
	if len(cal) != 1 || cal[0].N != 5 || cal[0].NHuman != 2 || !strings.Contains(out, "wrote calibration") {
		t.Fatalf("with 2 human truths (min_n 2) the line is written and records both counts: %+v\n%s", cal, out)
	}
}

// Audit 3: serve's automatic calibration draws a line only for the rung whose truths are complete. Other rungs' lines are not touched.
func TestAutoCalibrateWritesOnlyTheTriggeringRung(t *testing.T) {
	dir := t.TempDir()
	srv, _ := contextModel(t)
	specPath := writeContextSpec(t, dir)
	sp := mustLoad(t, specPath)
	sp.PutCalibration(spec.Calibration{Model: "m", SpecHash: sp.Hash(), Method: "identity", N: 2, NHuman: 2, FittedAt: "2020-01-01"})
	if err := sp.Save(); err != nil {
		t.Fatal(err)
	}
	storeDir := filepath.Join(dir, ".shikii")
	var ef endpointFlags
	ef.model, ef.local = "m", srv.URL
	s := httptest.NewServer(newServer(serveOpts{specsDir: filepath.Join(dir, "decisions"), storeDir: storeDir, ef: ef, autoCalibrate: true}))
	t.Cleanup(s.Close)
	var last map[string]any
	for _, x := range []string{"a", "b"} {
		_, d := postJSON(t, s.URL+"/v1/decide", map[string]any{"spec": "intent", "inputs": map[string]string{"message": "fix " + x, "previous": "ctx"}})
		if d["rung"] != "context" {
			t.Fatalf("decide: %v", d)
		}
		_, last = postJSON(t, s.URL+"/v1/label", map[string]any{"trace_id": d["trace_id"], "spec": "intent", "truth": "change_request", "source": "ui"})
	}
	if last["calibrated"] != true {
		t.Fatalf("2 human truths on the context rung (min_n 2) must trigger its calibration: %v", last)
	}
	after := mustLoad(t, specPath)
	if after.ContextSpec().CalibrationFor("m") == nil {
		t.Fatalf("the context rung's line must be written: %+v", after.Calibration)
	}
	if c := after.CalibrationFor("m"); c == nil || c.FittedAt != "2020-01-01" {
		t.Fatalf("the minimal rung's existing line must be left alone: %+v", after.Calibration)
	}
}

// Audit 5: the input hash is per rung (only the inputs that rung's template uses). Even if the context input differs, it is the same input for the minimal question.
func TestInputsHashIsPerRung(t *testing.T) {
	dir := t.TempDir()
	srv, _ := contextModel(t)
	specPath := writeContextSpec(t, dir)
	storeDir := filepath.Join(dir, ".shikii")
	out := capture(t, func() error {
		return cmdDecide([]string{specPath, "-i", "message=fix a", "-i", "previous=ctx one", "--local", srv.URL, "--store", storeDir, "--json"})
	})
	var d decide.Decision
	if err := json.Unmarshal([]byte(out), &d); err != nil {
		t.Fatal(err)
	}
	if d.InputsHash != store.HashInputs(map[string]string{"message": "fix a", "previous": "ctx one"}) || d.Minimal == nil || d.Minimal.InputsHash != store.HashInputs(map[string]string{"message": "fix a"}) {
		t.Fatalf("each rung hashes the inputs its template uses: %s / %+v", d.InputsHash, d.Minimal)
	}
	capture(t, func() error { return cmdLabel([]string{d.TraceID, "question", "--store", storeDir}) })
	labels := readLabels(t, storeDir, "intent")
	if len(labels) != 2 || labels[1].InputsHash != d.Minimal.InputsHash {
		t.Fatalf("the minimal rung's label carries the minimal inputs hash: %+v", labels)
	}
	// file rows with the same text fold into one input
	rows := `[{"inputs":{"message":"fix a"},"truth":"question"},{"inputs":{"message":"fix b"},"truth":"change_request"}]`
	labeledPath := filepath.Join(dir, "rows.json")
	if err := os.WriteFile(labeledPath, []byte(rows), 0o644); err != nil {
		t.Fatal(err)
	}
	out = capture(t, func() error {
		return cmdCalibrate([]string{specPath, "--local", srv.URL, "--store", storeDir, "--labeled", labeledPath})
	})
	if !strings.Contains(out, "deduplicated: 3 labels on 2 distinct inputs") {
		t.Fatalf("the store label for {message, previous} and the file row for {message} are the same minimal input:\n%s", out)
	}
	// Material for asking again: the text is found from the minimal rung's hash too
	st := store.Store{Dir: storeDir}
	texts, err := st.InputsByHash("intent")
	if err != nil || texts[d.Minimal.InputsHash].Inputs["message"] != "fix a" {
		t.Fatalf("texts must be reachable by the minimal inputs hash too: %v %+v", err, texts)
	}
}

// Audits 7 and 8: status counts the person's confirmations by decision (trace). For score with levels, no machine truth is written (Top and P are different events).
func TestHumanCheckedCountsTracesAndLevelScoresWriteNoMachineTruth(t *testing.T) {
	dir := t.TempDir()
	srv, _ := contextModel(t)
	specPath := writeContextSpec(t, dir)
	storeDir := filepath.Join(dir, ".shikii")
	out := capture(t, func() error {
		return cmdDecide([]string{specPath, "-i", "message=fix a", "-i", "previous=ctx", "--local", srv.URL, "--store", storeDir})
	})
	trace := traceRe.FindStringSubmatch(out)[1]
	capture(t, func() error {
		return cmdLabel([]string{trace, "change_request", "--store", storeDir, "--source", "audit"})
	})
	out = capture(t, func() error { return cmdStatus([]string{specPath, "--store", storeDir}) })
	if !strings.Contains(out, "audit answers so far: 1") {
		t.Fatalf("one audited decision is one human check, not two labels:\n%s", out)
	}

	sc := &spec.Spec{ID: "sev", Type: spec.Score, Question: "q", Levels: []string{"low", "mid", "high"}, State: spec.State{Template: "{{ m }}", Required: []string{"m"}, Context: "{{ c }} {{ m }}"},
		Policy: spec.Policy{SendTo: []string{"local"}, PII: boolPtrMain(false)}, Thresholds: spec.Thresholds{Act: 0.75, Ask: 0.6, Level: 1}, Eval: spec.Eval{MinN: 2}}
	d := &decide.Decision{TraceID: "t1", SpecID: "sev", SpecHash: sc.ContextHash(), Type: "score", Rung: "context", Band: decide.Act, Top: "mid", P: 0.85,
		Minimal: &store.Pass{SpecHash: sc.Hash(), Raw: map[string]float64{"low": 0.3, "mid": 0.4, "high": 0.3}}, At: time.Now()}
	st := store.Store{Dir: filepath.Join(dir, ".sev")}
	if err := recordDecision(st, sc, d, map[string]string{"m": "x", "c": "y"}); err != nil {
		t.Fatal(err)
	}
	if ls := readLabels(t, st.Dir, "sev"); len(ls) != 0 {
		t.Fatalf("P is P(level >= 1) but Top is the argmax; they are different events, so no machine truth: %+v", ls)
	}
}

func boolPtrMain(b bool) *bool { return &b }

// 9: rows with raw are accepted only when the keys are complete, values are in 0..1, and the sum is close to 1. noul numbers are also 0..1.
func TestRawRowsMustBeCompleteDistributions(t *testing.T) {
	dir := t.TempDir()
	specPath := writeSpec(t, dir, "false")
	sp := mustLoad(t, specPath)
	bad := []string{`{"change_request":0.9}`, `{"change_request":1.5,"question":-0.5,"other":0}`, `{"change_request":0.5,"question":0.5,"other":0.5}`}
	for _, raw := range bad {
		if _, err := rawFromRow(sp, json.RawMessage(raw)); err == nil {
			t.Fatalf("raw %s must be rejected", raw)
		}
	}
	if d, err := rawFromRow(sp, json.RawMessage(`{"change_request":0.9,"question":0.08,"other":0.02}`)); err != nil || d["other"] != 0.02 {
		t.Fatalf("a complete distribution is accepted: %v %v", err, d)
	}
	noul := sp
	noul.Type = "noul"
	noul.Options = nil
	for _, raw := range []string{`1.5`, `-0.1`, `{"no":0.3}`} {
		if _, err := rawFromRow(noul, json.RawMessage(raw)); err == nil {
			t.Fatalf("noul raw %s must be rejected", raw)
		}
	}
	if d, err := rawFromRow(noul, json.RawMessage(`{"yes":0.7,"no":0.3}`)); err != nil || d["yes"] != 0.7 {
		t.Fatalf("noul object with yes is accepted: %v %v", err, d)
	}
}

// D: status counts machine truths (context) and re-labels of the same input and says "partway through automatic drawing", but automatic calibration counts only human truths (one per input) and does not run
func TestLineStateCountsLikeTheAutoCalibration(t *testing.T) {
	repo := gitRepo(t)
	agent := hookEnv(t, nil, repo)
	path := agent + "/decisions/changes-in-scope.yaml"
	sp := mustLoad(t, path)
	st := store.Store{Dir: agent}
	var labels []store.Label
	for i := 0; i < sp.Eval.MinN; i++ {
		truth := "no"
		if i%2 == 0 {
			truth = "yes"
		}
		// Only 5 kinds of input (the same input labeled many times). Half are machine truths (context)
		src := "outcome"
		inp := fmt.Sprintf("in%d", i%5)
		if i%3 == 0 {
			src = "context"
		}
		labels = append(labels, store.Label{TraceID: fmt.Sprintf("t%03d", i), SpecID: sp.ID, SpecHash: sp.Hash(), Type: "noul", Model: "m1", InputsHash: inp, Truth: truth, Weight: 1, Source: src, At: time.Now()})
	}
	if err := st.AppendLabels(labels); err != nil {
		t.Fatal(err)
	}
	if err := st.AppendRecord(store.Record{TraceID: "r1", SpecID: sp.ID, SpecHash: sp.Hash(), Model: "m1", InputsHash: "in0", At: time.Now()}); err != nil {
		t.Fatal(err)
	}
	out := capture(t, func() error { return cmdStatus([]string{"--store", agent, path}) })
	called := false
	old := hookCalibrate
	hookCalibrate = func(agent, path, storeDir, model string) { called = true }
	defer func() { hookCalibrate = old }()
	hookMaybeCalibrate(agent, sp.ID)
	if strings.Contains(out, "the line is drawn automatically") && !called {
		t.Fatalf("status says it will draw automatically, but automatic calibration does not run (human truths per input are below min_n)")
	}
}

// E: automatic calibration was tried on m1, yet even m2's key, never tried, shows as "tried but could not separate"
func TestLineStateStuckOnlyForTheModelTried(t *testing.T) {
	repo := gitRepo(t)
	agent := hookEnv(t, nil, repo)
	path := agent + "/decisions/changes-in-scope.yaml"
	sp := mustLoad(t, path)
	st := store.Store{Dir: agent}
	var labels []store.Label
	for i := 0; i < sp.Eval.MinN+2; i++ {
		truth := []string{"yes", "no"}[i%2]
		labels = append(labels, store.Label{TraceID: fmt.Sprintf("t%03d", i), SpecID: sp.ID, SpecHash: sp.Hash(), Type: "noul", Model: "m2", InputsHash: fmt.Sprintf("in%d", i), Truth: truth, Weight: 1, Source: "outcome", At: time.Now()})
	}
	st.AppendLabels(labels)
	guardModel := "m1"
	guard := agent + "/calibrating-changes-in-scope"
	os.WriteFile(guard, []byte(fmt.Sprintf("2026-10-24T00:00:00+09:00 %s %d %s\n", guardModel, sp.Eval.MinN, sp.Hash())), 0o600)
	old := time.Now().Add(-2 * hookCalibrateGap)
	os.Chtimes(guard, old, old)
	out := capture(t, func() error { return cmdStatus([]string{"--store", agent, path}) })
	if strings.Contains(out, "tried: the answers could not be separated") {
		t.Fatalf("a note from trying on m1 makes m2's key show as \"tried but could not separate\":\n%s", out)
	}
}

// 0.9.4 audit: when no line fits (every truth on one side, or a higher probability goes with "no"), --force must not write an empty
// calibration block (method "") over the spec; it is refused like any other refusal and the spec stays as it was.
func TestForceDoesNotWriteAnEmptyLineWhenNoneFits(t *testing.T) {
	for _, name := range []string{"one-sided", "reversed"} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			path := writeNoulSpec(t, dir) // min_n 4
			before, _ := os.ReadFile(path)
			rows := []map[string]any{}
			for i := 0; i < 8; i++ {
				truth, raw := "no", 0.9
				if i%2 == 0 {
					truth, raw = "yes", 0.1
				}
				if name == "one-sided" {
					truth, raw = "yes", 0.2+0.05*float64(i)
				}
				rows = append(rows, map[string]any{"inputs": map[string]string{"a": fmt.Sprint(i)}, "truth": truth, "raw": raw})
			}
			file := filepath.Join(dir, "labels.json")
			data, _ := json.Marshal(rows)
			if err := os.WriteFile(file, data, 0o600); err != nil {
				t.Fatal(err)
			}
			var report bytes.Buffer
			err := runCalibrate(path, calibrateOpts{output: &report, storeDir: filepath.Join(dir, "store"), labeled: file, write: true, force: true})
			var refused *calibrationRefusal
			if !errors.As(err, &refused) {
				t.Fatalf("--force with no fitted line must be refused, got %v\n%s", err, report.String())
			}
			if after, _ := os.ReadFile(path); !bytes.Equal(before, after) {
				t.Fatalf("--force wrote a block although no line fits:\n%s", after)
			}
		})
	}
}

// 0.9.4 audit: a noul label file whose truth is a string other than yes / no (e.g. "true", "Yes") is a broken file, not "the answers cannot be told apart".
// Before, such rows counted as "no", so a file of "true" strings became a one-sided refusal.
func TestNoulLabelFileRejectsTruthOtherThanYesNo(t *testing.T) {
	dir := t.TempDir()
	path := writeNoulSpec(t, dir)
	rows := []map[string]any{}
	for i := 0; i < 6; i++ {
		rows = append(rows, map[string]any{"inputs": map[string]string{"a": fmt.Sprint(i)}, "truth": "true", "raw": 0.8})
	}
	file := filepath.Join(dir, "labels.json")
	data, _ := json.Marshal(rows)
	if err := os.WriteFile(file, data, 0o600); err != nil {
		t.Fatal(err)
	}
	var report bytes.Buffer
	err := runCalibrate(path, calibrateOpts{output: &report, storeDir: filepath.Join(dir, "store"), labeled: file, write: true})
	var refused *calibrationRefusal
	if err == nil || errors.As(err, &refused) || !strings.Contains(err.Error(), "yes or no") {
		t.Fatalf("a noul truth of \"true\" must be an error that names yes / no, got %T %v\n%s", err, err, report.String())
	}
}

// check measures act's precision on human truths alone too. Even if machine truths (context / replay / file) make it look high, when there are
// at least min_n act decisions a person checked and their precision does not reach act, it fails. status shows that count.
func TestCheckMeasuresActPrecisionOnHumanTruthsToo(t *testing.T) {
	dir := t.TempDir()
	specPath := writeSpec(t, dir, "false")
	sp := mustLoad(t, specPath)
	sp.PutCalibration(spec.Calibration{Model: "m", SpecHash: sp.Hash(), Method: "identity", N: 2, FittedAt: "2026-10-18", PrecisionAt: 1})
	if err := sp.Save(); err != nil {
		t.Fatal(err)
	}
	storeDir := filepath.Join(dir, ".shikii")
	st := store.Store{Dir: storeDir}
	add := func(i int, truth, source string) {
		in := map[string]string{"message": "m" + string(rune('a'+i))}
		top := 0.95 // the probability tells right from wrong (0.10: check fails a line whose probabilities say nothing), and both are above act
		if truth != "change_request" {
			top = 0.9
		}
		l := store.Label{TraceID: store.NewTraceID(), SpecID: "intent", SpecHash: sp.Hash(), Type: "choice", Model: "m", InputsHash: store.HashInputs(in), Inputs: in,
			Raw: map[string]float64{"change_request": top, "question": 1 - top, "other": 0}, Truth: truth, Weight: 1, Source: source, At: time.Now()}
		if err := st.AppendLabel(l); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 10; i++ {
		add(i, "change_request", "context") // machine truths: all right
	}
	add(10, "question", "audit") // act decisions a person checked: both wrong
	add(11, "question", "audit")
	var buf strings.Builder
	old := stdout
	stdout = &buf
	err := cmdCheck([]string{specPath, "--store", storeDir})
	stdout = old
	out := buf.String()
	if err == nil || !strings.Contains(out, "human-checked") || !strings.Contains(out, "0.000") {
		t.Fatalf("overall precision is 10/12 but the 2 human-checked act decisions were both wrong; check must say so:\nerr=%v\n%s", err, out)
	}
	// While human truths are below min_n, do not fail; just state the count
	if err := os.Remove(filepath.Join(storeDir, "labels", "intent.jsonl")); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 10; i++ {
		add(i, "change_request", "context")
	}
	add(10, "question", "audit")
	out = capture(t, func() error { return cmdCheck([]string{specPath, "--store", storeDir}) })
	if !strings.Contains(out, "human-checked act decisions: 1") {
		t.Fatalf("below min_n check reports the count without failing:\n%s", out)
	}
	out = capture(t, func() error { return cmdStatus([]string{specPath, "--store", storeDir}) })
	if !strings.Contains(out, "audit: 1 in 20 act decisions is shown to a person; audit answers so far: 1") {
		t.Fatalf("status must show the audit sampling state:\n%s", out)
	}
}

// Tests that close the holes found in the audit of 2026-10-17. Each was confirmed to fail before being fixed.

// Audit C1: identity and vector were scored on different label sets (with or without partial labels), and check failed right after calibrate wrote.
// If a partial label "!x" contains a miss, vector's CV must count the same miss.
func TestVectorCVScoresPartialLabelsLikeCheck(t *testing.T) {
	dir := t.TempDir()
	storeDir := filepath.Join(dir, ".shikii")
	srv := fakeSystemOne(t, "m", map[string]float64{"change_request": 0.9, "question": 0.1, "other": 0})
	specPath := writeSpec(t, dir, "false") // act 0.75, min_n 2
	for i := 0; i < 10; i++ {
		out := capture(t, func() error {
			return cmdDecide([]string{specPath, "--local", srv.URL, "--store", storeDir, "-i", fmt.Sprintf("message=full %d", i)})
		})
		capture(t, func() error {
			return cmdLabel([]string{"--store", storeDir, traceRe.FindStringSubmatch(out)[1], "change_request"})
		})
	}
	for i := 0; i < 5; i++ {
		out := capture(t, func() error {
			return cmdDecide([]string{specPath, "--local", srv.URL, "--store", storeDir, "-i", fmt.Sprintf("message=partial %d", i)})
		})
		capture(t, func() error {
			return cmdLabel([]string{"--store", storeDir, traceRe.FindStringSubmatch(out)[1], "!change_request"})
		})
	}
	// precision@act is 10/15 = 0.667 < 0.75: neither identity nor vector may be written
	var buf bytes.Buffer
	old := stdout
	stdout = &buf
	err := cmdCalibrate([]string{specPath, "--store", storeDir, "--model", "m", "--write"})
	stdout = old
	if err == nil {
		t.Fatalf("calibrate must refuse to write when partial labels pull precision below act:\n%s", buf.String())
	}
	if sp := mustLoad(t, specPath); len(sp.Calibration) != 0 {
		t.Fatalf("nothing may be written: %+v", sp.Calibration)
	}
	if !strings.Contains(buf.String(), "vector 5-fold CV at act: precision=0.667") {
		t.Fatalf("vector CV must be scored on the same 15 labels (precision 0.667):\n%s", buf.String())
	}
}

// Audit C2/C3: when the same input had several truths (a file row and one a person added later / a correction on the same trace),
// calibrate and check counted differently. Both count "the latest truth per input".
func TestCalibrateAndCheckUseLatestTruthPerInput(t *testing.T) {
	dir := t.TempDir()
	storeDir := filepath.Join(dir, ".shikii")
	srv := fakeSystemOne(t, "m", map[string]float64{"change_request": 0.9, "question": 0.1, "other": 0})
	specPath := writeSpec(t, dir, "false")
	rows := `[{"inputs":{"message":"fix 0"},"truth":"change_request"},{"inputs":{"message":"fix 1"},"truth":"change_request"}]`
	labeledPath := filepath.Join(dir, "rows.json")
	if err := os.WriteFile(labeledPath, []byte(rows), 0o644); err != nil {
		t.Fatal(err)
	}
	capture(t, func() error {
		return cmdCalibrate([]string{specPath, "--labeled", labeledPath, "--local", srv.URL, "--store", storeDir, "--write"})
	})
	capture(t, func() error { return cmdCheck([]string{"--store", storeDir, specPath}) })
	// The same text "fix 0" is decided in real use, and a person corrects it to "it was a question"
	out := capture(t, func() error {
		return cmdDecide([]string{specPath, "--local", srv.URL, "--store", storeDir, "-i", "message=fix 0"})
	})
	tr := traceRe.FindStringSubmatch(out)[1]
	capture(t, func() error { return cmdLabel([]string{"--store", storeDir, "--source", "ask", tr, "question"}) })
	// check: with the latest truth, "fix 0" is wrong → 1/2 = 0.5 < act → fails
	if err := cmdCheck([]string{"--store", storeDir, specPath}); err == nil {
		t.Fatal("check must see the corrected truth and fail")
	}
	// calibrate looks at the same set too (n=2, latest truths). It does not grow to 3
	var buf bytes.Buffer
	old := stdout
	stdout = &buf
	_ = cmdCalibrate([]string{specPath, "--labeled", labeledPath, "--local", srv.URL, "--store", storeDir})
	stdout = old
	if !strings.Contains(buf.String(), " n=2 ") || !strings.Contains(buf.String(), "top-1 accuracy=0.500") {
		t.Fatalf("calibrate must count one latest truth per input (n=2, accuracy 0.5):\n%s", buf.String())
	}
	// Labeling the same trace twice still counts only the latest one
	capture(t, func() error { return cmdLabel([]string{"--store", storeDir, "--source", "ask", tr, "change_request"}) })
	buf.Reset()
	stdout = &buf
	_ = cmdCalibrate([]string{specPath, "--labeled", labeledPath, "--local", srv.URL, "--store", storeDir})
	stdout = old
	if !strings.Contains(buf.String(), " n=2 ") || !strings.Contains(buf.String(), "top-1 accuracy=1.000") {
		t.Fatalf("a corrected label must replace, not add:\n%s", buf.String())
	}
}

// Audit S1: text recorded with --tainted must never be sent to hosted, even when asked again later.
func TestTaintedTextIsNeverReplayedToHosted(t *testing.T) {
	t.Setenv("TYPESAFE_API_KEY", "k")
	var hostedCalls, localCalls atomic.Int32
	answer := func(model string, calls *atomic.Int32) *httptest.Server {
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls.Add(1)
			var req systemone.Request
			_ = json.NewDecoder(r.Body).Decode(&req)
			resp := systemone.Response{Model: model, Answers: map[string]systemone.Answer{}}
			for k := range req.Questions {
				resp.Answers[k] = systemone.Answer{Type: "choice", Choice: "change_request", Probabilities: map[string]float64{"change_request": 0.9, "question": 0.1, "other": 0}}
			}
			_ = json.NewEncoder(w).Encode(resp)
		}))
		t.Cleanup(s.Close)
		return s
	}
	hosted := answer("m", &hostedCalls)
	local := answer("m", &localCalls)
	dir := t.TempDir()
	storeDir := filepath.Join(dir, ".shikii")
	specPath := filepath.Join(dir, "intent.yaml")
	src := strings.Replace(strings.Replace(string(mustRead(t, writeSpec(t, dir, "false"))), "send_to: [local]", "send_to: [hosted, local]", 1), "min_n: 2", "min_n: 1", 1)
	if err := os.WriteFile(specPath, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	out := capture(t, func() error {
		return cmdDecide([]string{specPath, "--hosted", hosted.URL, "--local", local.URL, "--tainted", "--store", storeDir, "-i", "message=PROD ROW"})
	})
	if hostedCalls.Load() != 0 || localCalls.Load() != 1 {
		t.Fatalf("tainted decide must go local: hosted=%d local=%d", hostedCalls.Load(), localCalls.Load())
	}
	capture(t, func() error {
		return cmdLabel([]string{"--store", storeDir, traceRe.FindStringSubmatch(out)[1], "change_request"})
	})
	edited := strings.Replace(src, "question: What is asked?", "question: What is asked? Be strict.", 1)
	if err := os.WriteFile(specPath, []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	// Asking again (even when called without the tainted flag) does not send text that was tainted to hosted
	capture(t, func() error {
		return cmdCalibrate([]string{specPath, "--hosted", hosted.URL, "--local", local.URL, "--store", storeDir, "--labeled", "none"})
	})
	if hostedCalls.Load() != 0 {
		t.Fatalf("replay sent tainted text to hosted (%d calls)", hostedCalls.Load())
	}
	if localCalls.Load() != 2 {
		t.Fatalf("replay must have asked local once more, got %d", localCalls.Load())
	}
}

// Audit S5: check rejects a hand-written identity block (n below min_n, no fitted_at).
func TestCheckRejectsBlocksThatCalibrateWouldNotWrite(t *testing.T) {
	dir := t.TempDir()
	storeDir := filepath.Join(dir, ".shikii")
	srv := fakeSystemOne(t, "m", map[string]float64{"change_request": 0.9, "question": 0.1, "other": 0})
	specPath := writeSpec(t, dir, "false") // min_n 2
	out := capture(t, func() error {
		return cmdDecide([]string{specPath, "--local", srv.URL, "--store", storeDir, "-i", "message=fix"})
	})
	capture(t, func() error {
		return cmdLabel([]string{"--store", storeDir, traceRe.FindStringSubmatch(out)[1], "change_request"})
	})
	sp := mustLoad(t, specPath)
	sp.PutCalibration(spec.Calibration{Model: "m", SpecHash: sp.Hash(), Method: "identity", PrecisionAt: 1, N: 1})
	if err := sp.Save(); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	old := stdout
	stdout = &buf
	err := cmdCheck([]string{"--store", storeDir, specPath})
	stdout = old
	if err == nil || !strings.Contains(buf.String(), "min_n") {
		t.Fatalf("check must reject n=1 < min_n=2 and say so: err=%v\n%s", err, buf.String())
	}
}

// Audit C12: drift must not say ok with NaN for a line whose temperature was set to 0 by hand.
func TestDriftRejectsZeroTemperature(t *testing.T) {
	dir := t.TempDir()
	storeDir := filepath.Join(dir, ".shikii")
	srv := fakeSystemOne(t, "m", map[string]float64{"change_request": 0.9, "question": 0.1, "other": 0})
	specPath := writeSpec(t, dir, "false")
	out := capture(t, func() error {
		return cmdDecide([]string{specPath, "--local", srv.URL, "--store", storeDir, "-i", "message=fix"})
	})
	capture(t, func() error {
		return cmdLabel([]string{"--store", storeDir, traceRe.FindStringSubmatch(out)[1], "change_request"})
	})
	sp := mustLoad(t, specPath)
	sp.PutCalibration(spec.Calibration{Model: "m", SpecHash: sp.Hash(), Method: "vector", Temperature: 0, Biases: map[string]float64{}, N: 2, FittedAt: "2026-10-17"})
	if err := sp.Save(); err != nil {
		t.Fatal(err)
	}
	if err := cmdDrift([]string{specPath, "--store", storeDir, "--model", "m"}); err == nil || !strings.Contains(err.Error(), "temperature") {
		t.Fatalf("drift must refuse a block with temperature 0, got %v", err)
	}
}

// Even with enough cases, saving stops on separation or precision. Check the reason and the fact that nothing was saved together.
func TestCalibrationRefusalExplainsActualReason(t *testing.T) {
	for _, tc := range []struct {
		name   string
		n, min int
		choice bool
		want   string
		// reversed: a higher probability goes with "no" (the fitted slope is not positive). It is the same "cannot tell the answers apart" as an overlap,
		// so the page shows it as a quality refusal, not as a failure with the English fitting message (0.9.3: a person saw it after answering on the page)
		reversed bool
		oneSided bool // every truth is "yes": also a quality refusal (the page says only one kind of answer), not the English "need both" failure
		mixed    bool // the probability says almost nothing about the truth: no clear signal, though the share of "yes" alone passes the precision at act
	}{
		{"no-signal", 50, 50, false, "predictions", false, false, true},
		{"insufficient", 2, 50, false, "short of", false, false, false},
		{"precision", 2, 2, true, "predictions", false, false, false},
		{"reversed", 50, 50, false, "predictions", true, false, false},
		{"one-sided", 50, 50, false, "predictions", false, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := writeNoulSpec(t, dir)
			if tc.choice {
				path = writeSpec(t, dir, "false")
			}
			sp := mustLoad(t, path)
			sp.Eval.MinN = tc.min
			specData, err := yaml.Marshal(sp)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, specData, 0600); err != nil {
				t.Fatal(err)
			}
			before, _ := os.ReadFile(path)
			rows := []map[string]any{}
			for i := 0; i < tc.n; i++ {
				truth, raw := "no", any(0.2)
				inputs := map[string]string{"a": fmt.Sprint(i)}
				if i%2 == 0 {
					truth, raw = "yes", 0.7
				}
				if tc.oneSided {
					truth = "yes"
				}
				if tc.mixed { // "yes" 9 times in 10, so act at 0.70 is right 90% of the time on the share alone, but the probability barely ranks the answers (AUC about 0.54)
					truth, raw = "yes", 0.75
					if i%2 == 0 {
						raw = 0.8
					}
					if i == 1 || i == 3 || i == 5 || i == 10 || i == 12 {
						truth = "no"
					}
				}
				if tc.reversed {
					raw = 0.9
					if truth == "yes" {
						raw = 0.1
					}
				}
				if tc.n >= tc.min && !tc.choice && !tc.mixed && i == 0 {
					raw = 0.4
				}
				if tc.n >= tc.min && !tc.choice && !tc.mixed && i == 1 {
					raw = 0.6
				}
				if tc.choice {
					inputs = map[string]string{"message": fmt.Sprint(i)}
					truth = "question"
					if i%2 == 0 {
						truth = "change_request"
					}
					raw = map[string]float64{"change_request": 0.9, "question": 0.1, "other": 0}
				}
				rows = append(rows, map[string]any{"inputs": inputs, "truth": truth, "raw": raw})
			}
			file := filepath.Join(dir, "labels.json")
			data, _ := json.Marshal(rows)
			if err := os.WriteFile(file, data, 0600); err != nil {
				t.Fatal(err)
			}
			var report bytes.Buffer
			err = runCalibrate(path, calibrateOpts{output: &report, storeDir: filepath.Join(dir, "store"), labeled: file, write: true})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("wrong refusal: %v\n%s", err, report.String())
			}
			var refused *calibrationRefusal
			if !errors.As(err, &refused) || (tc.n >= tc.min && refused.reason() != "quality") {
				t.Fatalf("a refusal must be a calibrationRefusal (quality when there are enough truths), not a failure: %T %v", err, err)
			}
			if tc.n >= tc.min && (strings.Contains(err.Error(), "fewer human truths") || strings.Contains(err.Error(), "short of the")) {
				t.Fatalf("quality failure must not claim insufficient truths: %v", err)
			}
			after, _ := os.ReadFile(path)
			if !bytes.Equal(before, after) {
				t.Fatal("refused calibration changed spec")
			}
		})
	}
}

// 0.10: check fails a line whose probabilities do not tell the answers apart even when the share of "yes" alone passes the precision
// at act (an overlap alone no longer fails it). The rows carry raw probabilities, so no model is called.
func TestCheckFailsALineWithNoClearSignal(t *testing.T) {
	for _, tc := range []struct {
		name     string
		noSignal bool
	}{{"no-signal", true}, {"overlap", false}} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := writeNoulSpec(t, dir)
			rows := []map[string]any{}
			for i := 0; i < 50; i++ {
				truth, raw := "no", 0.2
				if i%2 == 0 {
					truth, raw = "yes", 0.8
				}
				if i == 1 {
					raw = 0.9 // one "no" above the "yes" answers
				}
				if tc.noSignal { // the same rows as the no-signal refusal: "yes" 9 times in 10, AUC about 0.54
					truth, raw = "yes", 0.75
					if i%2 == 0 {
						raw = 0.8
					}
					if i == 1 || i == 3 || i == 5 || i == 10 || i == 12 {
						truth = "no"
					}
				}
				rows = append(rows, map[string]any{"inputs": map[string]string{"a": fmt.Sprint(i)}, "truth": truth, "raw": raw})
			}
			file := filepath.Join(dir, "labels.json")
			data, _ := json.Marshal(rows)
			if err := os.WriteFile(file, data, 0o600); err != nil {
				t.Fatal(err)
			}
			src, _ := os.ReadFile(path)
			if err := os.WriteFile(path, bytes.Replace(src, []byte("eval:\n"), []byte("eval:\n  labeled: "+file+"\n"), 1), 0o644); err != nil {
				t.Fatal(err)
			}
			sp := mustLoad(t, path)
			sp.PutCalibration(spec.Calibration{Model: "m", SpecHash: sp.Hash(), Method: "identity", FittedOn: file, N: 50, FittedAt: "2026-10-28"})
			if err := sp.Save(); err != nil {
				t.Fatal(err)
			}
			var err error
			out := capture(t, func() error { err = cmdCheck([]string{"--store", filepath.Join(dir, "store"), path}); return nil })
			failed := err != nil && strings.Contains(out+err.Error(), "do not tell the answers apart")
			if failed != tc.noSignal {
				t.Fatalf("check must fail only the line with no clear signal: err=%v\n%s", err, out)
			}
		})
	}
}

// 0.10: a real model's answers always overlap somewhere. One "no" above one "yes" in an otherwise clear ranking no longer refuses the
// line (before, any overlapping pair did, so a question a person answered with AUC 0.84 could never get one); the precision at act still guards it.
func TestCalibrationDrawsALineThroughAnOverlap(t *testing.T) {
	dir := t.TempDir()
	path := writeNoulSpec(t, dir)
	rows := []map[string]any{}
	for i := 0; i < 50; i++ {
		truth, raw := "no", 0.2
		if i%2 == 0 {
			truth, raw = "yes", 0.8
		}
		switch i {
		case 0:
			raw = 0.4 // a "yes" below
		case 1:
			raw = 0.6 // a "no" above
		}
		rows = append(rows, map[string]any{"inputs": map[string]string{"a": fmt.Sprint(i)}, "truth": truth, "raw": raw})
	}
	file := filepath.Join(dir, "labels.json")
	data, _ := json.Marshal(rows)
	if err := os.WriteFile(file, data, 0o600); err != nil {
		t.Fatal(err)
	}
	var report bytes.Buffer
	if err := runCalibrate(path, calibrateOpts{output: &report, storeDir: filepath.Join(dir, "store"), labeled: file, write: true}); err != nil {
		t.Fatalf("one overlapping pair must not refuse the line: %v\n%s", err, report.String())
	}
	if !strings.Contains(report.String(), "discrimination: AUC=") {
		t.Fatalf("the report says how well the answers are told apart:\n%s", report.String())
	}
	if len(mustLoad(t, path).Calibration) == 0 {
		t.Fatalf("the line is written:\n%s", report.String())
	}
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

// (2) When truths are below eval.min_n, calibrate and tune say "what to do": collect truths, or, for a trial, lower the spec's eval.min_n.
// tune rejected with the same text even when --labeled was passed, so --labeled is not suggested as the fix (it is added only as how files are read, when not passed).
func TestTooFewTruthsSaysWhatToDoNext(t *testing.T) {
	dir := t.TempDir()
	storeDir := filepath.Join(dir, ".shikii")
	srv := fakeSystemOne(t, "m1", map[string]float64{"change_request": 0.9, "question": 0.08, "other": 0.02})
	specPath := writeSpec(t, dir, "false") // min_n 2
	b, _ := os.ReadFile(specPath)
	if err := os.WriteFile(specPath, append(b, []byte("variants:\n  - name: strict\n    question: What exactly is asked?\n")...), 0o644); err != nil {
		t.Fatal(err) // tune stops there without candidates, so put one candidate
	}
	out := capture(t, func() error {
		return cmdDecide([]string{specPath, "--local", srv.URL, "--store", storeDir, "-i", "message=fix it"})
	})
	trace := traceRe.FindStringSubmatch(out)[1]
	capture(t, func() error { return cmdLabel([]string{"--store", storeDir, trace, "change_request"}) })

	err := cmdCalibrate([]string{specPath, "--store", storeDir, "--model", "m1", "--write"})
	if err == nil || !strings.Contains(err.Error(), "lower the spec's eval.min_n") || !strings.Contains(err.Error(), "shikii label") {
		t.Fatalf("calibrate must name both ways out (collect truths, or lower eval.min_n for a trial): %v", err)
	}
	err = cmdTune([]string{specPath, "--local", srv.URL, "--store", storeDir})
	if err == nil || !strings.Contains(err.Error(), "lower eval.min_n") || !strings.Contains(err.Error(), "--labeled FILE") {
		t.Fatalf("tune without --labeled must name the ways out and how a file is read: %v", err)
	}
	rows := filepath.Join(dir, "rows.json")
	if err := os.WriteFile(rows, []byte(`[{"inputs":{"message":"fix it"},"truth":"change_request"}]`), 0o644); err != nil { // the same text as the store: one per input, so there is still one truth
		t.Fatal(err)
	}
	err = cmdTune([]string{specPath, "--local", srv.URL, "--store", storeDir, "--labeled", rows})
	if err == nil || strings.Contains(err.Error(), "--labeled") || !strings.Contains(err.Error(), "lower eval.min_n") {
		t.Fatalf("tune with --labeled already given must not suggest --labeled as the way out: %v", err)
	}
}

// (3) drift's ask agreement is n/a, not 0.000, when no truths answered on ask are in the window.
func TestDriftSaysNotApplicableWithoutAskLabels(t *testing.T) {
	dir := t.TempDir()
	storeDir := filepath.Join(dir, ".shikii")
	srv := fakeSystemOne(t, "m1", map[string]float64{"change_request": 0.95, "question": 0.03, "other": 0.02})
	specPath := writeSpec(t, dir, "false")
	for i := 0; i < 3; i++ {
		out := capture(t, func() error {
			return cmdDecide([]string{specPath, "--local", srv.URL, "--store", storeDir, "-i", fmt.Sprintf("message=fix %d", i)})
		})
		tr := traceRe.FindStringSubmatch(out)[1]
		capture(t, func() error { return cmdLabel([]string{"--store", storeDir, "--source", "ui", tr, "change_request"}) })
	}
	capture(t, func() error { return cmdCalibrate([]string{specPath, "--store", storeDir, "--model", "m1", "--write"}) })
	out := capture(t, func() error { return cmdDrift([]string{specPath, "--store", storeDir, "--model", "m1"}) })
	if !strings.Contains(out, "ask agreement:    n/a (no ask-sourced labels") || strings.Contains(out, "ask agreement:    0.000") {
		t.Fatalf("no ask labels means n/a, not 0.000:\n%s", out)
	}
}

// (6) bias runs on the same bank as calibrate / tune (the text of truths in the store). --labeled is not needed.
func TestBiasReadsStatesFromTheStoreLikeCalibrate(t *testing.T) {
	dir := t.TempDir()
	specPath := writeSpec(t, dir, "false")
	sp := mustLoad(t, specPath)
	storeDir := filepath.Join(dir, ".shikii")
	st := store.Store{Dir: storeDir}
	for i := 0; i < 4; i++ {
		in := map[string]string{"message": fmt.Sprintf("m%d", i)}
		l := store.Label{TraceID: store.NewTraceID(), SpecID: "intent", SpecHash: sp.Hash(), Type: "choice", Model: "m", InputsHash: store.HashInputs(in), Inputs: in,
			Raw: map[string]float64{"change_request": 0.9, "question": 0.1, "other": 0}, Truth: "change_request", Weight: 1, Source: "ui", At: time.Now()}
		if err := st.AppendLabel(l); err != nil {
			t.Fatal(err)
		}
	}
	fair := fakeSystemOne(t, "m", map[string]float64{"change_request": 0.45, "question": 0.40, "other": 0.15})
	out := capture(t, func() error {
		return cmdBias([]string{specPath, "--store", storeDir, "--local", fair.URL, "--permutations", "2"})
	})
	if !strings.Contains(out, "rows=4") || !strings.Contains(out, "\nok") {
		t.Fatalf("bias must take its states from the store's truths:\n%s", out)
	}
	err := cmdBias([]string{specPath, "--store", filepath.Join(dir, "empty"), "--local", fair.URL})
	if err == nil || !strings.Contains(err.Error(), "no states to ask") || !strings.Contains(err.Error(), "shikii label") {
		t.Fatalf("with nothing to ask, bias says where states come from: %v", err)
	}
	_ = spec.Choice
}

func TestProjectCalibrationsKeepTheirReports(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	a, b := gitRepo(t), gitRepo(t)
	aStore, aScope := initTestProject(t, a)
	bStore, bScope := initTestProject(t, b)
	enteredA, enteredB, releaseA, releaseB := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req systemone.Request
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
			return
		}
		if strings.Contains(req.State, "AUDIT_REPLAY_A") {
			close(enteredA)
			<-releaseA
		} else if strings.Contains(req.State, "AUDIT_REPLAY_B") {
			close(enteredB)
			<-releaseB
		} else {
			t.Errorf("unexpected synthetic input")
			return
		}
		ans := map[string]systemone.Answer{}
		for key := range req.Questions {
			ans[key] = systemone.Answer{Type: "choice", Choice: "change_request", Probabilities: map[string]float64{"change_request": .9, "question": .1, "other": 0}, Confidence: .9}
		}
		_ = json.NewEncoder(w).Encode(systemone.Response{Model: "m", Answers: ans})
	}))
	defer func() {
		select {
		case <-releaseA:
		default:
			close(releaseA)
		}
		select {
		case <-releaseB:
		default:
			close(releaseB)
		}
		model.Close()
	}()
	seed := func(root, name string) string {
		path := writeSpec(t, filepath.Join(root, "decisions"), "true")
		sp := mustLoad(t, path)
		st := store.Store{Dir: root}
		target := ""
		for i := 0; i < 3; i++ {
			input := map[string]string{"message": fmt.Sprintf("AUDIT_CURRENT_%s_%d", name, i)}
			hash := sp.Hash()
			if i == 2 {
				input["message"] = "AUDIT_REPLAY_" + name
				hash = "old-wording"
			}
			rec := store.Record{TraceID: store.NewTraceID(), SpecID: "intent", SpecHash: hash, Model: "m", Type: "choice", Inputs: input, InputsHash: store.HashInputs(input), Raw: map[string]float64{"change_request": .9, "question": .1, "other": 0}, Top: "change_request", P: .9, Band: "ask", At: time.Now()}
			if err := st.AppendRecord(rec); err != nil {
				t.Fatal(err)
			}
			if i == 1 {
				target = rec.TraceID
				continue
			}
			label, err := store.LabelFor(&rec, "change_request", 1, "ui")
			if err != nil {
				t.Fatal(err)
			}
			if err = st.AppendLabel(label); err != nil {
				t.Fatal(err)
			}
		}
		return target
	}
	traceA, traceB := seed(aStore, "A"), seed(bStore, "B")
	common, _ := agentHome()
	var ef endpointFlags
	ef.local, ef.model = model.URL, "m"
	hub := newProjectHub(common, serveOpts{ef: ef, autoCalibrate: true})
	var initial bytes.Buffer
	old := stdout
	stdout = &initial
	defer func() { stdout = old }()
	type result struct {
		code int
		body string
	}
	doneA, doneB := make(chan result, 1), make(chan result, 1)
	invoke := func(scope, trace string, ch chan result) {
		body := `{"trace_id":"` + trace + `","truth":"change_request","source":"ui"}`
		r := httptest.NewRequest("POST", "/p/"+projectID(scope)+"/v1/label", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		hub.ServeHTTP(w, r)
		ch <- result{w.Code, w.Body.String()}
	}
	go invoke(aScope, traceA, doneA)
	select {
	case <-enteredA:
	case <-time.After(5 * time.Second):
		t.Fatal("A's calibration does not reach the model")
	}
	go invoke(bScope, traceB, doneB)
	select {
	case <-enteredB:
	case <-time.After(5 * time.Second):
		t.Fatal("B's calibration does not reach the model")
	}
	close(releaseA)
	ra := <-doneA
	close(releaseB)
	rb := <-doneB
	wrongRestore := stdout != &initial
	stdout = &initial
	leaked := strings.Contains(rb.body, filepath.Join(aStore, "decisions", "intent.yaml"))
	ownMissing := !strings.Contains(ra.body, filepath.Join(aStore, "decisions", "intent.yaml"))
	if ra.code != 200 || rb.code != 200 || leaked || ownMissing || wrongRestore {
		t.Fatalf("concurrent calibrations must not mix reports and outputs: A=%d B=%d leaked=%v ownMissing=%v wrongRestore=%v", ra.code, rb.code, leaked, ownMissing, wrongRestore)
	}

}

// calibrate can read choice / score rows too when they carry raw (an object of probabilities per choice). It draws a line without calling a model,
// so a benchmark's LoRA arm (raw produced by another tool) can be lined up on the same scale as A and B. noul's raw stays a single number as before.
func TestCalibrateReadsChoiceRowsWithRawDistributions(t *testing.T) {
	dir := t.TempDir()
	specPath := writeSpec(t, dir, "false") // min_n 2
	rows := `[
	 {"inputs":{"message":"fix a"},"raw":{"change_request":0.9,"question":0.08,"other":0.02},"truth":"change_request"},
	 {"inputs":{"message":"fix b"},"raw":{"change_request":0.85,"question":0.1,"other":0.05},"truth":"change_request"},
	 {"inputs":{"message":"why a"},"raw":{"change_request":0.1,"question":0.88,"other":0.02},"truth":"question"},
	 {"inputs":{"message":"why b"},"raw":{"change_request":0.2,"question":0.75,"other":0.05},"truth":"question"}
	]`
	labeledPath := filepath.Join(dir, "rows.json")
	if err := os.WriteFile(labeledPath, []byte(rows), 0o644); err != nil {
		t.Fatal(err)
	}
	// Nowhere to send (no --local): there is raw, so no call is needed
	out := capture(t, func() error {
		return cmdCalibrate([]string{specPath, "--labeled", labeledPath, "--model", "lora-bench", "--store", filepath.Join(dir, ".shikii"), "--write"})
	})
	for _, must := range []string{"n=4", "top-1 accuracy=1.000", "wrote calibration"} {
		if !strings.Contains(out, must) {
			t.Fatalf("calibrate must score the given raw without a model; missing %q:\n%s", must, out)
		}
	}
	sp := mustLoad(t, specPath)
	if c := sp.CalibrationFor("lora-bench"); c == nil || c.N != 4 {
		t.Fatalf("the line is keyed by the given model name: %+v", sp.Calibration)
	}
	// If a raw key is not one of the spec's choices, it is an error (not silently 0)
	bad := strings.Replace(rows, `"other":0.02},"truth":"change_request"`, `"nope":0.02},"truth":"change_request"`, 1)
	if err := os.WriteFile(labeledPath, []byte(bad), 0o644); err != nil {
		t.Fatal(err)
	}
	var buf strings.Builder
	old := stdout
	stdout = &buf
	err := cmdCalibrate([]string{specPath, "--labeled", labeledPath, "--model", "lora-bench", "--store", filepath.Join(dir, ".shikii2")})
	stdout = old
	if err == nil || !strings.Contains(err.Error(), "nope") {
		t.Fatalf("a raw key that is not an option must be an error, got %v", err)
	}
}

// 0.7.3: whether delivered lessons changed behavior is counted from real-use observation, not a synthetic experiment. status shows the number of quotes delivered,
// the helpful / unused / harmful / unknown the reviews gave, and how many have no observation yet. Delivery of corrections is counted separately.
func TestStatusCountsDeliveredLessonsAndTheirEffect(t *testing.T) {
	st, scope, _, draft, revision := memoryEffectFixture(t)
	out := capture(t, func() error { return cmdStatus([]string{"--store", st.Dir}) })
	if !strings.Contains(out, "memory: no lessons delivered yet") {
		t.Fatalf("with no deliveries it must say so:\n%s", out)
	}
	memoryEffectPrompt(t, scope, "s1")
	out = capture(t, func() error { return cmdStatus([]string{"--store", st.Dir}) })
	if !strings.Contains(out, "memory: 1 quotes delivered") || !strings.Contains(out, "not yet assessed 1") {
		t.Fatalf("must count delivered quotes and show those awaiting observation:\n%s", out)
	}
	turn := memoryEffectComplete(t, st, scope, "s1") // an observation can be attached once the reply and the person's next message are both in
	if err := st.SaveMemoryAssessment(scope, store.MemoryAssessment{SourceID: turn, Key: "lesson:" + draft.ID + ":" + revision, Result: "helpful", Decision: "未初期化の場所の確認を手順に足した", Evidence: "返答に確認の手順が入った"}); err != nil {
		t.Fatal(err)
	}
	out = capture(t, func() error { return cmdStatus([]string{"--store", st.Dir}) })
	if !strings.Contains(out, "helpful 1, unused 0, harmful 0, unknown 0, not yet assessed 0") || !strings.Contains(out, "last 7 days: delivered 1, helpful 1") {
		t.Fatalf("once observed it counts as helpful and also shows the last 7 days:\n%s", out)
	}
}

// Audit R2: even in a store with no records and no specs, status shows the lines for delivered lessons and blocking checks (the README promises it).
func TestStatusWithoutRecordsStillPrintsMemoryAndGuards(t *testing.T) {
	dir := t.TempDir()
	store := filepath.Join(dir, ".shikii")
	if err := os.MkdirAll(store, 0o700); err != nil {
		t.Fatal(err)
	}
	out := capture(t, func() error { return cmdStatus([]string{"--store", store}) })
	if !strings.Contains(out, "memory:") || !strings.Contains(out, "guards:") {
		t.Fatalf("the memory and guards lines show even without records:\n%s", out)
	}
}

// TestMain removes the developer's keys (TYPESAFE_API_KEY / DECISIONS_API_KEY) and SHIKII_MODEL before the tests.
// Otherwise tests that assume the local fake model send requests to the real api.typesafe.ai,
// and the store's records, calibrations and bands change and the tests fail (2026-10-23: 6 failed right after the key was put in an environment variable).
// Tests that try Jev (hook_hosted_test.go) call t.Setenv themselves.
func TestMain(m *testing.M) {
	for _, k := range []string{"TYPESAFE_API_KEY", "DECISIONS_API_KEY", "SHIKII_MODEL"} {
		os.Unsetenv(k)
	}
	// No test starts real background processes (up, serve) or a browser (a test that skipped the harness forgot a stub, and a real one was left running. 0.9.1)
	hookSpawn = recordSpawn
	openBrowser = func(url string) { opened = append(opened, url) }
	stopProcess = func(int) error { return nil } // uninstall never signals a real process from tests (0.9.3)
	processCommand = func(int) string { return "" }
	// Writing files out to the disk (on macOS a flush of the disk's cache) made the eight test groups wait on each other: a test that
	// takes 0.3 s alone took 1 to 2 s. Tests do not need the data to survive a power cut.
	store.SyncTemp = func(*os.File) error { return nil }
	syncPersonFile = func(*os.File) error { return nil }
	os.Exit(m.Run())
}

// check measures a choice line's AUC on the raw probabilities, as calibrate does: a vector scaling that reorders the answers must not
// turn a clear raw ranking into "no clear signal". Raw: right answers at 0.9, wrong ones at 0.6 (AUC 1). A bias of ln 10 on "other"
// makes the wrong ones' top answer "other" at about 0.71, above the right ones' 0.62 (AUC 0 after scaling).
func TestCheckMeasuresAChoiceLineOnTheRawProbabilities(t *testing.T) {
	sp := mustLoad(t, writeSpec(t, t.TempDir(), "false"))
	var labels []store.Label
	for i := 0; i < 25; i++ {
		truth, top := "question", 0.9
		if i%5 == 0 {
			truth, top = "change_request", 0.6 // wrong: the raw top answer is question, with a lower probability
		}
		labels = append(labels, store.Label{Raw: map[string]float64{"question": top, "change_request": (1 - top) / 2, "other": (1 - top) / 2}, Truth: truth, Weight: 1})
	}
	if _, noSignal, _ := evaluateCalibration(sp, spec.Calibration{Method: "vector", Temperature: 1, Biases: map[string]float64{"other": math.Log(10)}}, labels); noSignal {
		t.Fatal("the raw probabilities tell right from wrong, so check does not call it no clear signal")
	}
}

// check fails a choice line whose top answer's probability does not tell right from wrong (0.10, the same rule as yes/no questions).
func TestCheckFailsAChoiceLineWithNoClearSignal(t *testing.T) {
	dir := t.TempDir()
	path := writeSpec(t, dir, "false")
	rows := []map[string]any{}
	for i := 0; i < 200; i++ {
		truth := "question"
		if i%10 == 0 {
			truth = "other"
		}
		rows = append(rows, map[string]any{"inputs": map[string]string{"message": fmt.Sprint(i)}, "truth": truth,
			"raw": map[string]float64{"question": 0.9, "change_request": 0.05, "other": 0.05}})
	}
	file := filepath.Join(dir, "labels.json")
	data, _ := json.Marshal(rows)
	if err := os.WriteFile(file, data, 0o600); err != nil {
		t.Fatal(err)
	}
	src, _ := os.ReadFile(path)
	if err := os.WriteFile(path, bytes.Replace(src, []byte("eval:\n"), []byte("eval:\n  labeled: "+file+"\n"), 1), 0o644); err != nil {
		t.Fatal(err)
	}
	sp := mustLoad(t, path)
	sp.PutCalibration(spec.Calibration{Model: "m", SpecHash: sp.Hash(), Method: "identity", FittedOn: file, N: 200, FittedAt: "2026-10-28"})
	if err := sp.Save(); err != nil {
		t.Fatal(err)
	}
	var err error
	out := capture(t, func() error { err = cmdCheck([]string{"--store", filepath.Join(dir, "store"), path}); return nil })
	if err == nil || !strings.Contains(out+err.Error(), "do not tell the answers apart") {
		t.Fatalf("check fails the choice line with no clear signal: err=%v\n%s", err, out)
	}
}

// 0.10: a choice question whose top answer's probability says nothing about whether it is right (always 0.9, right 9 times in 10)
// gets no line, though the share of right answers alone passes the precision at act (the same rule as yes/no questions).
func TestChoiceLineNeedsAClearSignal(t *testing.T) {
	dir := t.TempDir()
	path := writeSpec(t, dir, "false")
	rows := []map[string]any{}
	for i := 0; i < 200; i++ {
		truth := "question"
		if i%10 == 0 {
			truth = "other"
		}
		rows = append(rows, map[string]any{"inputs": map[string]string{"message": fmt.Sprint(i)}, "truth": truth,
			"raw": map[string]float64{"question": 0.9, "change_request": 0.05, "other": 0.05}})
	}
	file := filepath.Join(dir, "labels.json")
	data, _ := json.Marshal(rows)
	if err := os.WriteFile(file, data, 0o600); err != nil {
		t.Fatal(err)
	}
	var report bytes.Buffer
	err := runCalibrate(path, calibrateOpts{output: &report, storeDir: filepath.Join(dir, "store"), labeled: file, write: true})
	if err == nil || len(mustLoad(t, path).Calibration) > 0 {
		t.Fatalf("no line without a clear signal: %v\n%s", err, report.String())
	}
}

// 0.11: when the person's checks show an outcome's truths are clearly less right than act asks, calibrate does not use them and says so,
// status says so, and the line state counts like calibrate.
func TestUntrustedOutcomeTruthsAreNotUsed(t *testing.T) {
	root, sampled := outcomeFixture(t, 60)
	st := store.Store{Dir: root}
	for _, r := range sampled[:3] {
		labels, err := store.LabelsFor(&r, "no", 1, "audit")
		if err != nil {
			t.Fatal(err)
		}
		if err := st.AppendLabels(labels); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(root, "decisions", "changes-in-scope.yaml")
	var report bytes.Buffer
	_ = runCalibrate(path, calibrateOpts{output: &report, storeDir: root, labeled: "none", ef: endpointFlags{model: "m"}})
	if !strings.Contains(report.String(), `"yes" of 60: checked 3, agreed 0`) || !strings.Contains(report.String(), "not used") || !strings.Contains(report.String(), " n=3 ") {
		t.Fatalf("calibrate drops the untrusted outcome truths and says so (3 person truths left):\n%s", report.String())
	}
	out := capture(t, func() error { return cmdStatus([]string{"--store", root, path}) })
	if !strings.Contains(out, `"yes" of 60: checked 3, agreed 0`) || !strings.Contains(out, "not used to draw lines") {
		t.Fatalf("status says the outcome truths are not used:\n%s", out)
	}
	sp := mustLoad(t, path)
	_, labs, _ := st.Counts(sp.ID)
	if n := lineStateOf(sp, sp.Eval.MinN, "m", sp.Hash(), labs, false).N; n != 3 {
		t.Fatalf("the line state counts like calibrate: %d", n)
	}
	// the hook's automatic calibration counts the same way: 3 truths are below min_n, so it does not start (60 with the outcome's)
	if err := hookMaybeCalibrate(root, sp.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "calibrating-"+sp.ID)); !os.IsNotExist(err) {
		t.Fatalf("the hook does not start a calibration on the untrusted outcome's truths: %v", err)
	}
}
