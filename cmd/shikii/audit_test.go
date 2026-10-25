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
	"sync/atomic"
	"testing"

	"github.com/hayaenfj/shikii/internal/spec"
	"github.com/hayaenfj/shikii/internal/store"
	"github.com/hayaenfj/shikii/internal/systemone"
)

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

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// Audit S2/S3: serve refuses POSTs from other origins, non-JSON POSTs and bodies that are too large.
func TestServeRejectsCrossOriginWrongTypeAndOversizedPosts(t *testing.T) {
	url, _, _ := newServeFixture(t, "false", map[string]float64{"change_request": 0.9, "question": 0.1, "other": 0})
	body := `{"spec":"intent","inputs":{"message":"x"}}`
	post := func(ctype, origin string, b string) int {
		req, _ := http.NewRequest(http.MethodPost, url+"/v1/decide", strings.NewReader(b))
		req.Header.Set("Content-Type", ctype)
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	if c := post("application/json", "https://evil.example", body); c != 403 {
		t.Fatalf("cross-origin POST must be 403, got %d", c)
	}
	if c := post("text/plain", "", body); c != 415 {
		t.Fatalf("non-JSON POST must be 415, got %d", c)
	}
	big := `{"spec":"intent","inputs":{"message":"` + strings.Repeat("x", 2<<20) + `"}}`
	if c := post("application/json", "", big); c != 413 {
		t.Fatalf("oversized POST must be 413, got %d", c)
	}
	if c := post("application/json", "http://127.0.0.1:8095", body); c != 200 {
		t.Fatalf("same-host origin must pass, got %d", c)
	}
	if c := post("application/json", "", body); c != 200 {
		t.Fatalf("no origin (curl, programs) must pass, got %d", c)
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

// Audit S6: labels through MCP show their source as "mcp". weight does not exceed 1.
func TestMCPLabelsAreMarkedAndWeightIsBounded(t *testing.T) {
	send, specPath := mcpSession(t, "false", map[string]float64{"change_request": 0.9, "question": 0.1, "other": 0})
	call := func(id int, name string, args map[string]any) map[string]any {
		return result(t, send(map[string]any{"jsonrpc": "2.0", "id": id, "method": "tools/call", "params": map[string]any{"name": name, "arguments": args}}))
	}
	d := call(1, "shikii_decide", map[string]any{"spec": "intent", "inputs": map[string]any{"message": "fix"}})["structuredContent"].(map[string]any)
	l := call(2, "shikii_label", map[string]any{"trace_id": d["trace_id"], "truth": "change_request"})
	if l["isError"] == true || l["structuredContent"].(map[string]any)["source"] != "mcp" {
		t.Fatalf("label from MCP must carry source=mcp: %v", l)
	}
	heavy := call(3, "shikii_label", map[string]any{"trace_id": d["trace_id"], "truth": "change_request", "weight": 5})
	if heavy["isError"] != true {
		t.Fatalf("weight > 1 must be rejected: %v", heavy)
	}
	st := store.Store{Dir: filepath.Join(filepath.Dir(filepath.Dir(specPath)), ".shikii")}
	labs, _ := st.LabeledTraces("intent")
	if len(labs) != 1 || labs[d["trace_id"].(string)].Source != "mcp" {
		t.Fatalf("stored label: %+v", labs)
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
