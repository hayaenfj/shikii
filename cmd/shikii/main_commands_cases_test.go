package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/spec"
	"github.com/hayaenfj/shikii/internal/store"
	"github.com/hayaenfj/shikii/internal/systemone"
)

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
