package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/spec"
	"github.com/hayaenfj/shikii/internal/store"
)

// On one endpoint: ask → it appears in the queue → a label like a button press → it leaves the queue.
func TestServeDecideQueueAndLabel(t *testing.T) {
	url, _, _ := newServeFixture(t, "true", map[string]float64{"change_request": 0.9, "question": 0.1, "other": 0})
	code, d := postJSON(t, url+"/v1/decide", map[string]any{"spec": "intent", "inputs": map[string]string{"message": "fix the typo"}})
	if code != 200 || d["band"] != "ask" || d["trace_id"] == "" {
		t.Fatalf("decide: %d %v", code, d)
	}
	trace := d["trace_id"].(string)

	var q struct {
		Items []map[string]any `json:"items"`
	}
	if getJSON(t, url+"/v1/queue?spec=intent", &q) != 200 || len(q.Items) != 1 {
		t.Fatalf("queue must list the unlabeled decision: %+v", q)
	}
	item := q.Items[0]
	if item["trace_id"] != trace || item["inputs"].(map[string]any)["message"] != "fix the typo" {
		t.Fatalf("queue item must carry the stored text (pii: true): %v", item)
	}
	choices, _ := item["choices"].([]any)
	if len(choices) != 3 {
		t.Fatalf("queue item must offer the spec's options as choices: %v", item)
	}

	code, l := postJSON(t, url+"/v1/label", map[string]any{"trace_id": trace, "truth": "change_request", "source": "ui"})
	if code != 200 || l["truth"] != "change_request" {
		t.Fatalf("label: %d %v", code, l)
	}
	getJSON(t, url+"/v1/queue?spec=intent", &q)
	if len(q.Items) != 0 {
		t.Fatalf("labeled decision must leave the queue: %+v", q)
	}
	var st map[string]any
	getJSON(t, url+"/v1/status", &st)
	specs := st["specs"].([]any)
	if len(specs) != 1 || specs[0].(map[string]any)["labels"].(float64) != 1 {
		t.Fatalf("status must count the label: %v", st)
	}
}

// An unknown spec is 404, a truth not in the spec is 400. Nothing is let through silently.
func TestServeRejectsUnknownSpecAndBadTruth(t *testing.T) {
	url, _, _ := newServeFixture(t, "true", map[string]float64{"change_request": 0.9, "question": 0.1, "other": 0})
	if code, _ := postJSON(t, url+"/v1/decide", map[string]any{"spec": "nope", "inputs": map[string]string{"message": "x"}}); code != 404 {
		t.Fatalf("unknown spec must be 404, got %d", code)
	}
	_, d := postJSON(t, url+"/v1/decide", map[string]any{"spec": "intent", "inputs": map[string]string{"message": "x"}})
	if code, _ := postJSON(t, url+"/v1/label", map[string]any{"trace_id": d["trace_id"], "truth": "banana"}); code != 400 {
		t.Fatalf("truth outside the spec must be 400, got %d", code)
	}
	if code, _ := postJSON(t, url+"/v1/label", map[string]any{"trace_id": "0000000000000000", "truth": "question"}); code != 404 {
		t.Fatalf("unknown trace must be 404, got %d", code)
	}
}

// An (old) record whose text is not kept (from before text was kept) cannot be answered by a person. The queue says so with answerable=false and gives no button material (choices).
func TestServeQueueMarksItemsWithoutTextAsUnanswerable(t *testing.T) {
	url, specPath, storeDir := newServeFixture(t, "true", map[string]float64{"change_request": 0.9, "question": 0.1, "other": 0})
	sp, err := spec.Load(specPath)
	if err != nil {
		t.Fatal(err)
	}
	old := store.Record{TraceID: store.NewTraceID(), SpecID: sp.ID, SpecHash: sp.Hash(), Type: "choice", Model: "m",
		InputsHash: store.HashInputs(map[string]string{"message": "fix"}), Raw: map[string]float64{"change_request": 0.9},
		Top: "change_request", P: 0.9, Band: "ask", At: time.Now()} // no Inputs
	if err := (store.Store{Dir: storeDir}).AppendRecord(old); err != nil {
		t.Fatal(err)
	}
	var q struct {
		Items []map[string]any `json:"items"`
	}
	getJSON(t, url+"/v1/queue?spec=intent", &q)
	if len(q.Items) != 1 {
		t.Fatalf("queue: %+v", q)
	}
	it := q.Items[0]
	if it["answerable"] != false || len(it["choices"].([]any)) != 0 || it["inputs"] != nil {
		t.Fatalf("without text the item must be marked unanswerable and carry no choices: %v", it)
	}
}

// Where it listens is fixed: on a host, only 127.0.0.1 (never exposed to the LAN). Inside a container (where /.dockerenv exists), every interface (the host side limits port publishing to 127.0.0.1).
func TestBindHostIsLoopbackExceptInsideAContainer(t *testing.T) {
	if got := bindHostFor(filepath.Join(t.TempDir(), "no-such-file")); got != "127.0.0.1" {
		t.Fatalf("on a host shikii must listen on loopback only, got %q", got)
	}
	marker := filepath.Join(t.TempDir(), ".dockerenv")
	if err := os.WriteFile(marker, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if got := bindHostFor(marker); got != "0.0.0.0" {
		t.Fatalf("inside a container the port must be reachable from the host, got %q", got)
	}
}

// Redoing past decisions (the owner's request, 2026-10-18): (1) a list of what was answered and correcting answers (the new answer wins),
// (2) asking the AI again: decide the same text again with the current wording and line, keeping a new record with redo_of. The original leaves the queue, and the person's answer is copied to the new record.
func TestPastDecisionsCanBeRedone(t *testing.T) {
	url, _, storeDir := newServeFixture(t, "false", map[string]float64{"change_request": 0.6, "question": 0.4, "other": 0})
	code, d := postJSON(t, url+"/v1/decide", map[string]any{"spec": "intent", "inputs": map[string]string{"message": "fix the typo"}})
	if code != 200 {
		t.Fatalf("decide: %d %v", code, d)
	}
	trace := d["trace_id"].(string)
	// Redo a record not yet answered → the original leaves the queue and a new record appears
	code, res := postJSON(t, url+"/v1/redo", map[string]any{"spec": "intent", "trace_id": trace})
	if code != 200 || res["recorded"] != true || res["before"].(map[string]any)["trace_id"] != trace || res["after"].(map[string]any)["trace_id"] == trace {
		t.Fatalf("redo must decide again and record a new trace: %d %v", code, res)
	}
	newTrace := res["after"].(map[string]any)["trace_id"].(string)
	var q struct{ Items []map[string]any }
	getJSON(t, url+"/v1/queue?spec=intent", &q)
	ids := map[string]bool{}
	for _, it := range q.Items {
		ids[it["trace_id"].(string)] = true
	}
	if ids[trace] || !ids[newTrace] {
		t.Fatalf("the redone record replaces the original in the queue: %v", ids)
	}
	// A person answers → it appears in answered. Correcting the answer uses the new answer
	postJSON(t, url+"/v1/label", map[string]any{"spec": "intent", "trace_id": newTrace, "truth": "question"})
	var a struct{ Items []map[string]any }
	getJSON(t, url+"/v1/answered?spec=intent", &a)
	if len(a.Items) != 1 || a.Items[0]["truth"] != "question" || a.Items[0]["correct"] != false || a.Items[0]["redo_of"] != trace {
		t.Fatalf("answered list shows the person's answer and whether the AI matched: %v", a.Items)
	}
	postJSON(t, url+"/v1/label", map[string]any{"spec": "intent", "trace_id": newTrace, "truth": "change_request"})
	getJSON(t, url+"/v1/answered?spec=intent", &a)
	if len(a.Items) != 1 || a.Items[0]["truth"] != "change_request" || a.Items[0]["correct"] != true {
		t.Fatalf("the newest answer wins: %v", a.Items)
	}
	// Redo an answered record → the person's answer is copied to the new record, and right/wrong before and now are returned
	code, res = postJSON(t, url+"/v1/redo", map[string]any{"spec": "intent", "trace_id": newTrace})
	if code != 200 || res["truth"] != "change_request" || res["correct_before"] != true || res["correct_after"] != true {
		t.Fatalf("redo of an answered decision carries the answer and compares: %d %v", code, res)
	}
	// Audit 6 item 4: answered shows 1 item (with redo_of), not the original, and the answered count stays at the number of texts a person answered = 1
	getJSON(t, url+"/v1/answered?spec=intent", &a)
	if len(a.Items) != 1 || a.Items[0]["redo_of"] != newTrace {
		t.Fatalf("the redone record replaces the original in the answered list too: %v", a.Items)
	}
	var stat struct{ Specs []map[string]any }
	getJSON(t, url+"/v1/status", &stat)
	if stat.Specs[0]["labels"] != 1.0 {
		t.Fatalf("answered count is the number of texts a person answered (1), got %v", stat.Specs[0]["labels"])
	}
	// Audit 6 item 8a: limit (answer 2, then narrow to 1)
	_, d2 := postJSON(t, url+"/v1/decide", map[string]any{"spec": "intent", "inputs": map[string]string{"message": "why is it slow?"}})
	postJSON(t, url+"/v1/label", map[string]any{"spec": "intent", "trace_id": d2["trace_id"], "truth": "question"})
	getJSON(t, url+"/v1/answered?spec=intent", &a)
	if len(a.Items) != 2 {
		t.Fatalf("two texts answered: %d", len(a.Items))
	}
	getJSON(t, url+"/v1/answered?spec=intent&limit=1", &a)
	if len(a.Items) != 1 {
		t.Fatalf("limit must cap the answered list: %d", len(a.Items))
	}
	st := store.Store{Dir: storeDir}
	labeled, _ := st.LabeledTraces("intent")
	if l, ok := labeled[res["after"].(map[string]any)["trace_id"].(string)]; !ok || l.Truth != "change_request" || !store.HumanSource(l.Source) {
		t.Fatalf("the person's answer must be carried to the new record as a human truth: %+v", l)
	}
	getJSON(t, url+"/v1/queue?spec=intent", &q)
	if len(q.Items) != 0 {
		t.Fatalf("a redone answered decision does not come back to the queue: %v", q.Items)
	}
	// A record without text cannot be redone
	if err := st.AppendRecord(store.Record{TraceID: "old", SpecID: "intent", SpecHash: "h", Type: "choice", Band: "ask", InputsHash: "x", At: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if code, _ := postJSON(t, url+"/v1/redo", map[string]any{"spec": "intent", "trace_id": "old"}); code != 409 {
		t.Fatalf("no text → 409, got %d", code)
	}
	// A record with only machine truths (context / judge) is not "answered"
	mach := store.Record{TraceID: "mach", SpecID: "intent", SpecHash: "h", Type: "choice", Band: "act", Top: "question", P: 0.9, Inputs: map[string]string{"message": "why?"}, InputsHash: "y", Model: "m", At: time.Now()}
	if err := st.AppendRecord(mach); err != nil {
		t.Fatal(err)
	}
	if err := st.AppendLabel(store.Label{TraceID: "mach", SpecID: "intent", SpecHash: "h", Type: "choice", Model: "m", InputsHash: "y", Truth: "question", Weight: 0.9, Source: "judge", At: time.Now()}); err != nil {
		t.Fatal(err)
	}
	getJSON(t, url+"/v1/answered?spec=intent", &a)
	for _, it := range a.Items {
		if it["trace_id"] == "mach" {
			t.Fatalf("a machine truth is not a person's answer: %v", it)
		}
	}
	// The page has the parts
	body := uiText(t, url)
	for _, must := range []string{"v1/answered", "v1/redo", "回答済み", "AI にもう一度聞く", "別のボタンを押すと答えを直せます"} {
		if !strings.Contains(body, must) {
			t.Fatalf("UI must offer the redo: missing %q", must)
		}
	}
}

// Audit 6 items 5 and 8c: a redo is recorded with the same manners as /v1/decide. If it reaches act on a context rung, a machine truth (source context) stays on the minimal rung,
// and the copy of the person's answer goes on 2 rungs (the context hash and the minimal hash).
func TestRedoRecordsLikeDecideOnTheContextRung(t *testing.T) {
	dir := t.TempDir()
	srv, _ := contextModel(t)
	specPath := writeContextSpec(t, dir)
	sp := mustLoad(t, specPath)
	sp.PutCalibration(spec.Calibration{Model: "m", SpecHash: sp.Hash(), Method: "identity", N: 2, FittedAt: "2026-10-18"})
	sp.PutCalibration(spec.Calibration{Model: "m", SpecHash: sp.ContextHash(), Method: "identity", N: 2, FittedAt: "2026-10-18"})
	if err := sp.Save(); err != nil {
		t.Fatal(err)
	}
	storeDir := filepath.Join(dir, ".shikii")
	var ef endpointFlags
	ef.model, ef.local = "m", srv.URL
	ts := httptest.NewServer(newServer(serveOpts{specsDir: filepath.Join(dir, "decisions"), storeDir: storeDir, ef: ef}))
	defer ts.Close()
	inputs := map[string]string{"message": "fix a", "previous": "I renamed the module"}
	code, d := postJSON(t, ts.URL+"/v1/decide", map[string]any{"spec": "intent", "inputs": inputs})
	if code != 200 || d["rung"] != "context" || d["band"] != "act" {
		t.Fatalf("fixture: context act expected, got %d %v", code, d)
	}
	if n := len(readLabels(t, storeDir, "intent")); n != 1 {
		t.Fatalf("decide on the context rung leaves one machine truth, got %d", n)
	}
	code, res := postJSON(t, ts.URL+"/v1/redo", map[string]any{"spec": "intent", "trace_id": d["trace_id"]})
	if code != 200 || res["recorded"] != true {
		t.Fatalf("redo: %d %v", code, res)
	}
	labels := readLabels(t, storeDir, "intent")
	if len(labels) != 2 || labels[1].Source != "context" || labels[1].Rung != "minimal" {
		t.Fatalf("redo must record like decide: a second context truth for the minimal rung, got %+v", labels)
	}
	// A person answers the original, then redoes it → the copy is on 2 rungs
	postJSON(t, ts.URL+"/v1/label", map[string]any{"spec": "intent", "trace_id": d["trace_id"], "truth": "question"})
	before := len(readLabels(t, storeDir, "intent"))
	postJSON(t, ts.URL+"/v1/redo", map[string]any{"spec": "intent", "trace_id": d["trace_id"]})
	after := readLabels(t, storeDir, "intent")
	copied := 0
	for _, l := range after[before:] {
		if l.Source == "ui" && l.Truth == "question" {
			copied++
		}
	}
	if copied != 2 {
		t.Fatalf("the person's answer is carried to both rungs of the new record (2 labels), got %d of %+v", copied, after[before:])
	}
}

// 0.9.4 audit: the page's quality note says "the AI will try other wordings". Only the hook's questions get other wordings tried
// (variantsContext / hook variants submit accept hookSpecs only), so the result says whether that is so, and a one-sided set of truths
// is told as one kind of answer, not as "this wording cannot tell the answers apart".
func TestServeRefusalSaysWhetherWordingsAreTriedAndOneAnswer(t *testing.T) {
	for _, tc := range []struct {
		name, id, want  string
		oneSided, tries bool
	}{
		{"own question, reversed", "own-check", "quality", false, false},
		{"hook question, reversed", "needs-plan", "quality", false, true},
		{"hook question, one-sided", "needs-plan", "one_answer", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			specsDir := filepath.Join(dir, "decisions")
			if err := os.MkdirAll(specsDir, 0o755); err != nil {
				t.Fatal(err)
			}
			writeNamedNoulSpec(t, specsDir, tc.id)
			table := map[string]float64{}
			for i := 0; i < 4; i++ {
				table[fmt.Sprint(i)] = 0.9
				if i%2 == 0 {
					table[fmt.Sprint(i)] = 0.1
				}
			}
			model := fakeNoulByMessage(t, "m", table)
			var ef endpointFlags
			ef.model, ef.local = "m", model.URL
			srv := httptest.NewServer(newServer(serveOpts{specsDir: specsDir, storeDir: filepath.Join(dir, ".shikii"), ef: ef, autoCalibrate: true}))
			t.Cleanup(srv.Close)
			for i := 0; i < 4; i++ {
				_, d := postJSON(t, srv.URL+"/v1/decide", map[string]any{"spec": tc.id, "inputs": map[string]string{"a": fmt.Sprint(i)}})
				truth := "no"
				if i%2 == 0 || tc.oneSided {
					truth = "yes"
				}
				if code, l := postJSON(t, srv.URL+"/v1/label", map[string]any{"trace_id": d["trace_id"], "truth": truth}); code != 200 {
					t.Fatalf("label: %d %v", code, l)
				}
			}
			var status struct {
				Specs []map[string]any `json:"specs"`
			}
			getJSON(t, srv.URL+"/v1/status", &status)
			result, _ := status.Specs[0]["calibration_result"].(map[string]any)
			if result["reason"] != tc.want || result["tries_wordings"] != tc.tries {
				t.Fatalf("want reason %s, tries_wordings %v: %v", tc.want, tc.tries, result)
			}
		})
	}
}

// 0.9.7 re-audit C-6: a page whose model answered first (the usual case), stopped by the check, returned "use of closed network connection".
func TestReauditCOwnerStopAfterModelReadyReturnsError(t *testing.T) {
	hookEnv(t, nil)
	common, _ := agentHome()
	repo := gitRepo(t)
	initAt(t, repo)
	ticks := pageOwnerTicksByHand(t)
	up := pageListening(t)
	done := make(chan error, 1)
	go func() { // no --local: the model is ready at once
		done <- cmdServe([]string{"--store", filepath.Join(repo, ".shikii"), "--port", "0", "--local", ""})
	}()
	select {
	case <-up:
	case err := <-done:
		t.Fatalf("precondition: the page started: %v", err)
	}
	if err := os.Remove(pagePath(common, repo)); err != nil {
		t.Fatal(err)
	}
	ticks <- time.Now() // the owner check runs now
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("a page stopped by the check ends with an error: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("it does not stop")
	}
}

// 0.9.7 re-audit C-1: a page started by hand with --port 0 and a store not named .shikii was stopped by the minute check, although its record pointed at it.
// Only the hook's page (a project's .shikii on a free port) runs that check, so a serve started by hand is never stopped by it.
func TestReauditCHandServeWithOtherStoreNameIsNotStopped(t *testing.T) {
	for _, c := range []struct {
		hub   bool
		port  int
		store string
		want  bool
	}{
		{false, 0, "/work/p/.shikii", true},     // the hook's page
		{false, 0, "/data/other", false},        // by hand, a store under another name (C-1)
		{false, 8095, "/work/p/.shikii", false}, // by hand, a fixed port
		{true, 0, "/home/.shikii/agent", false}, // the hub
	} {
		if got := startedByHook(c.hub, c.port, c.store); got != c.want {
			t.Errorf("startedByHook(%v, %d, %s) = %v, want %v: only the hook's page is stopped by the owner check", c.hub, c.port, c.store, got, c.want)
		}
	}
}

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

	// act records with fixed trace ids, 2 of them spot-check samples, written plain, sampled, plain, sampled, plain so the queue has to move
	// the samples forward; and 1 ask record. Fixed ids: the case is there on every run (random ids left it out about 1 run in 30,000).
	st := store.Store{Dir: storeDir}
	var samples, plains []string
	for i := 0; len(samples) < 2 || len(plains) < 3; i++ {
		id := fmt.Sprintf("%016x", i)
		if store.AuditSample(id) {
			if len(samples) < 2 {
				samples = append(samples, id)
			}
		} else if len(plains) < 3 {
			plains = append(plains, id)
		}
	}
	at := time.Now().Add(-time.Hour)
	for n, id := range []string{plains[0], samples[0], plains[1], samples[1], plains[2]} {
		in := map[string]string{"message": fmt.Sprintf("fix %d", n)}
		rec := store.Record{TraceID: id, SpecID: "intent", SpecHash: sp.Hash(), Type: "choice", Model: "m", Inputs: in, InputsHash: store.HashInputs(in),
			Raw: map[string]float64{"change_request": 0.9, "question": 0.1, "other": 0}, Top: "change_request", P: 0.9, Band: "act", At: at.Add(time.Duration(n) * time.Minute)}
		if err := st.AppendRecord(rec); err != nil {
			t.Fatal(err)
		}
	}
	sampled := len(samples)
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
	if !strings.Contains(body, "shown to check") || !strings.Contains(body, "確かめのため") {
		t.Fatalf("the page must name the audit sample in both languages")
	}
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

// (5) When /v1/redo (and every endpoint that takes a spec) has no spec, it says "a spec is needed" and what to add, instead of `unknown spec ""`.
func TestMissingSpecIdSaysSpecIsRequired(t *testing.T) {
	url, _, _ := newServeFixture(t, "false", map[string]float64{"change_request": 0.6, "question": 0.4, "other": 0})
	code, d := postJSON(t, url+"/v1/decide", map[string]any{"spec": "intent", "inputs": map[string]string{"message": "fix the typo"}})
	if code != 200 {
		t.Fatalf("decide: %d %v", code, d)
	}
	for _, path := range []string{"/v1/redo", "/v1/decide", "/v1/tune/run"} {
		code, res := postJSON(t, url+path, map[string]any{"trace_id": d["trace_id"], "inputs": map[string]string{"message": "x"}})
		if code != 400 || !strings.Contains(fmt.Sprint(res["error"]), "spec is required") || !strings.Contains(fmt.Sprint(res["error"]), "decisions/") {
			t.Fatalf("%s without spec must say spec is required and where the id comes from: %d %v", path, code, res)
		}
	}
	code, res := postJSON(t, url+"/v1/redo", map[string]any{"spec": "nope", "trace_id": d["trace_id"]})
	if code != 404 || !strings.Contains(fmt.Sprint(res["error"]), `unknown spec "nope"`) {
		t.Fatalf("a wrong id is still unknown: %d %v", code, res)
	}
}

// The page (serve) does the same; a store without specs makes it return right after reading its flags, so no page is started.
func TestServeLocalReachesHookDecisions(t *testing.T) {
	oldLocal := hookLocalURL
	t.Cleanup(func() { hookLocalURL = oldLocal })
	t.Setenv("HOME", t.TempDir())
	t.Chdir(t.TempDir())
	hookLocalURL = "http://127.0.0.1:8080"
	if err := cmdServe([]string{"--store", t.TempDir(), "--local", "http://127.0.0.1:47392"}); err == nil {
		t.Fatal("a store without specs is refused before a page starts")
	}
	if hookLocalURL != "http://127.0.0.1:47392" {
		t.Fatalf("serve's --local reaches the decisions made through hookDecide: %s", hookLocalURL)
	}
}

// 0.8.0: for contradiction decisions, once the draft is saved (the insertion place got an outcome truth), the items for pages it did not touch are "undecided"
// and are neither counted as waiting for an outcome nor shown in the queue (even after saving, 61 items looked like they waited for an outcome forever). Before saving, they wait for an outcome.
func TestUndecidedConflictJudgmentsLeaveTheQueueOnceTheDraftIsSaved(t *testing.T) {
	model := fakeAgentModel(t, func(string) float64 { return .1 })
	hookEnv(t, model)
	repo := gitRepo(t)
	initAt(t, repo)
	root := filepath.Join(repo, ".shikii")
	st := store.Store{Dir: root}
	draft := "新しい教訓の文"
	st.AppendRecord(store.Record{TraceID: "wc1", SpecID: "wiki-conflict", Type: "noul", Model: "m", Inputs: map[string]string{"lesson": "既存の記事 A", "new_lesson": draft}, InputsHash: "h1", Raw: map[string]float64{"yes": .2, "no": .8}, Top: "no", Band: "discard", At: time.Now()})
	st.AppendRecord(store.Record{TraceID: "wp1", SpecID: "wiki-place", SpecHash: "sh", Type: "noul", Model: "m", Inputs: map[string]string{"lesson": "既存の記事 A", "new_lesson": draft}, InputsHash: "h2", Raw: map[string]float64{"yes": .2, "no": .8}, Top: "no", Band: "discard", At: time.Now()})
	o := serveOpts{storeDir: root, specsDir: filepath.Join(root, "decisions"), projectScope: repo}
	h := newServer(o)
	status := func() map[string]any {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/status", nil))
		var body struct {
			Specs []map[string]any `json:"specs"`
		}
		json.Unmarshal(rec.Body.Bytes(), &body)
		for _, sp := range body.Specs {
			if sp["id"] == "wiki-conflict" {
				return sp
			}
		}
		return nil
	}
	if sp := status(); sp["awaiting_results"] != float64(1) || sp["undecided"] != float64(0) {
		t.Fatalf("before saving, waiting for an outcome: %v", sp)
	}
	// The draft was saved (the insertion-place decision got an outcome truth)
	labels, err := store.LabelsFor(&store.Record{TraceID: "wp1", SpecID: "wiki-place", SpecHash: "sh", Type: "noul", Model: "m", InputsHash: "h2", Raw: map[string]float64{"yes": .2, "no": .8}, Top: "no"}, "no", placeOtherWeight, "outcome")
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range labels {
		if err := st.AppendLabel(l); err != nil {
			t.Fatal(err)
		}
	}
	if sp := status(); sp["awaiting_results"] != float64(0) || sp["undecided"] != float64(1) || sp["unlabeled"] != float64(0) {
		t.Fatalf("after saving, undecided (settled) and not counted as waiting for an outcome: %v", sp)
	}
	q := httptest.NewRecorder()
	h.ServeHTTP(q, httptest.NewRequest(http.MethodGet, "/v1/queue?spec=wiki-conflict", nil))
	if strings.Contains(q.Body.String(), `"wc1"`) {
		t.Fatalf("undecided records are not shown in the queue: %s", q.Body.String()[:200])
	}
}

// Before and after narrowing the count, the priority order, the exclusion of redo originals and the original text returned do not change.
func TestServeQueueLimitKeepsPriorityAndOriginals(t *testing.T) {
	url, path, dir := newServeFixture(t, "false", map[string]float64{"change_request": 1})
	sp, err := spec.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	st := store.Store{Dir: dir}
	ids := []string{"0000000000000001", "0000000000000002", "0000000000000003", "0000000000000004"}
	for i, id := range ids {
		inputs := map[string]string{"message": "fixture"}
		rec := store.Record{TraceID: id, SpecID: "intent", SpecHash: sp.Hash(), Type: "choice", Band: "ask", Inputs: inputs, InputsHash: store.HashInputs(inputs), At: time.Unix(int64(i), 0)}
		if i == 1 {
			rec.Band = "discard"
		}
		if i == 3 {
			rec.RedoOf = ids[2]
		}
		if err := st.AppendRecord(rec); err != nil {
			t.Fatal(err)
		}
		if err := st.SaveRecordText(rec, map[string]string{"message": "fixture original " + id}); err != nil {
			t.Fatal(err)
		}
	}
	for _, check := range []struct {
		query string
		want  []string
	}{{"&limit=1", []string{ids[3]}}, {"&limit=2", []string{ids[3], ids[0]}}, {"", []string{ids[3], ids[0], ids[1]}}} {
		var result struct {
			Items []struct {
				TraceID   string            `json:"trace_id"`
				Originals map[string]string `json:"original_inputs"`
			} `json:"items"`
		}
		if code := getJSON(t, url+"/v1/queue?spec=intent"+check.query, &result); code != 200 || len(result.Items) != len(check.want) {
			t.Fatalf("queue %s: status=%d rows=%+v", check.query, code, result.Items)
		}
		for i, item := range result.Items {
			if item.TraceID != check.want[i] || item.Originals["message"] != "fixture original "+item.TraceID {
				t.Fatalf("queue %s row %d: got %+v, want %s with its original", check.query, i, item, check.want[i])
			}
		}
	}
}

// prompt-kind's outcome truths are not checked by the person (0.11): the request alone cannot tell what the agent will do, so the
// question is left stopped and the person's time goes to the questions whose outcome rule is in doubt.
func TestQueueDoesNotAskToCheckPromptKindOutcomes(t *testing.T) {
	repo := gitRepo(t)
	hookEnv(t, nil)
	initAt(t, repo)
	root := filepath.Join(repo, ".shikii")
	sp := mustLoad(t, filepath.Join(root, "decisions", "prompt-kind.yaml"))
	st := store.Store{Dir: root}
	sampled := 0
	for i := 0; i < 60; i++ {
		inputs := map[string]string{"request": fmt.Sprint("do ", i)}
		r := store.Record{TraceID: fmt.Sprintf("%016x", i+1), SpecID: sp.ID, SpecHash: sp.Hash(), Type: "choice", Inputs: inputs, InputsHash: store.HashInputs(inputs), Model: "m", Raw: map[string]float64{"edit": .6, "explain": .4}, Top: "edit", P: .6, Band: "ask", At: time.Now()}
		if err := st.AppendRecord(r); err != nil {
			t.Fatal(err)
		}
		labels, err := store.LabelsFor(&r, "edit", 1, "outcome")
		if err != nil {
			t.Fatal(err)
		}
		if err := st.AppendLabels(labels); err != nil {
			t.Fatal(err)
		}
		if store.OutcomeCheckSample(r.TraceID) {
			sampled++
		}
	}
	if sampled == 0 {
		t.Fatal("the fixed sample picks some of 60")
	}
	r := httptest.NewRecorder()
	newServer(serveOpts{storeDir: root, specsDir: filepath.Join(root, "decisions")}).ServeHTTP(r, httptest.NewRequest("GET", "/v1/queue?spec=prompt-kind&limit=1000", nil))
	var body struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(r.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Items) != 0 {
		t.Fatalf("no outcome check is asked for prompt-kind: %d", len(body.Items))
	}
}
