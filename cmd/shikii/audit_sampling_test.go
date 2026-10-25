package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/spec"
	"github.com/hayaenfj/shikii/internal/store"
)

// The page's queue: after the ask decisions come spot checks of act decisions (1 in 20). The remaining act / discard come after. Spot checks are marked.
func TestQueuePutsAuditSamplesRightAfterAskItems(t *testing.T) {
	dir := t.TempDir()
	specsDir := filepath.Join(dir, "decisions")
	if err := os.MkdirAll(specsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	specPath := writeSpec(t, specsDir, "false")
	sp := mustLoad(t, specPath)
	sp.PutCalibration(spec.Calibration{Model: "m", SpecHash: sp.Hash(), Method: "identity", N: 2, FittedAt: "2026-10-18"})
	if err := sp.Save(); err != nil {
		t.Fatal(err)
	}
	storeDir := filepath.Join(dir, ".shikii")
	model := fakeSystemOne(t, "m", map[string]float64{"change_request": 0.9, "question": 0.1, "other": 0})
	var ef endpointFlags
	ef.model, ef.local = "m", model.URL
	s := httptest.NewServer(newServer(serveOpts{specsDir: specsDir, storeDir: storeDir, ef: ef}))
	t.Cleanup(s.Close)

	// 60 act records (added until a spot check appears) and 1 ask record (written by hand)
	st := store.Store{Dir: storeDir}
	sampled := 0
	for i := 0; i < 200 && sampled < 2; i++ {
		code, d := postJSON(t, s.URL+"/v1/decide", map[string]any{"spec": "intent", "inputs": map[string]string{"message": "fix " + string(rune('a'+i%26)) + string(rune('a'+i/26))}})
		if code != 200 || d["band"] != "act" {
			t.Fatalf("decide: %d %v", code, d)
		}
		if store.AuditSample(d["trace_id"].(string)) {
			sampled++
		}
	}
	askRec := store.Record{TraceID: store.NewTraceID(), SpecID: "intent", SpecHash: sp.Hash(), Type: "choice", Model: "m", Inputs: map[string]string{"message": "hmm"},
		InputsHash: store.HashInputs(map[string]string{"message": "hmm"}), Raw: map[string]float64{"change_request": 0.65, "question": 0.3, "other": 0.05}, Top: "change_request", P: 0.65, Band: "ask", At: time.Now()}
	if err := st.AppendRecord(askRec); err != nil {
		t.Fatal(err)
	}
	var q struct {
		Items []map[string]any `json:"items"`
	}
	getJSON(t, s.URL+"/v1/queue?spec=intent&limit=1000", &q)
	if len(q.Items) < 3 || q.Items[0]["band"] != "ask" || q.Items[0]["audit"] != false {
		t.Fatalf("ask first: %+v", q.Items[:3])
	}
	seenPlain := false
	nAudit := 0
	for _, it := range q.Items[1:] {
		audit := it["audit"] == true
		if audit != (it["band"] == "act" && store.AuditSample(it["trace_id"].(string))) {
			t.Fatalf("audit flag must be the 1-in-20 sample of act decisions: %+v", it)
		}
		if audit {
			nAudit++
			if seenPlain {
				t.Fatalf("audit samples must come before the unsampled act decisions: %+v", q.Items)
			}
		} else {
			seenPlain = true
		}
	}
	if nAudit != sampled {
		t.Fatalf("every sampled act decision is in the queue: %d vs %d", nAudit, sampled)
	}
	body := uiText(t, s.URL)
	if !strings.Contains(body, "audit") || !strings.Contains(body, "抜き取り") {
		t.Fatalf("the page must name the audit sample in both languages")
	}
}

func getBody(t *testing.T, url string) string {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return string(b)
}

// uiAssetRef is the page's own assets that index.html loads (script src and link href).
var uiAssetRef = regexp.MustCompile(`(?:src|href)="([^"]+)"`)

// uiText returns all text of the page: index.html joined with the assets it loads.
// The page is a Svelte bundle (web/dist embedded), so the wording lives in assets/*.js, not index.html.
// Every reference is relative to ./ (no external loads). Anything else fails.
func uiText(t *testing.T, url string) string {
	t.Helper()
	index := getBody(t, url+"/")
	var sb strings.Builder
	sb.WriteString(index)
	for _, m := range uiAssetRef.FindAllStringSubmatch(index, -1) {
		ref := m[1]
		if !strings.HasPrefix(ref, "./") {
			t.Fatalf("index.html must reference only its own assets (relative ./), found %q", ref)
		}
		sb.WriteString(getBody(t, url+ref[1:]))
	}
	return sb.String()
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
		l := store.Label{TraceID: store.NewTraceID(), SpecID: "intent", SpecHash: sp.Hash(), Type: "choice", Model: "m", InputsHash: store.HashInputs(in), Inputs: in,
			Raw: map[string]float64{"change_request": 0.9, "question": 0.1, "other": 0}, Truth: truth, Weight: 1, Source: source, At: time.Now()}
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
