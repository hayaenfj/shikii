package main

import (
	"context"
	"errors"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/spec"
	"github.com/hayaenfj/shikii/internal/store"
)

// Audit 5 item 8: serve's startup waits for the model's endpoint when there is --local, and does not wait without it (hosted only). A failure to wait is a startup failure.
func TestServeWaitsForTheLocalModelOnlyWhenThereIsOne(t *testing.T) {
	var asked []string
	fake := func(ctx context.Context, url string, every time.Duration, logf func(string, ...any)) error {
		asked = append(asked, url)
		if dl, ok := ctx.Deadline(); !ok || time.Until(dl) < 14*time.Minute {
			t.Fatalf("the wait must have a deadline of about 15 minutes")
		}
		if url == "http://127.0.0.1:1" {
			return errors.New("not ready")
		}
		return nil
	}
	if err := waitForLocalModel("", false, fake); err != nil || len(asked) != 0 {
		t.Fatalf("hosted only: nothing to wait for, got %v %v", err, asked)
	}
	// When there is a key and Jev is used, it does not wait even with --local (the rule is not to start a local model, so waiting would keep the page from opening; 2026-10-23)
	if err := waitForLocalModel("http://127.0.0.1:1", true, fake); err != nil || len(asked) != 0 {
		t.Fatalf("with a key the page must open without a local model: %v %v", err, asked)
	}
	if err := waitForLocalModel("http://127.0.0.1:8080", false, fake); err != nil || len(asked) != 1 || asked[0] != "http://127.0.0.1:8080" {
		t.Fatalf("a local model is waited for at its URL: %v %v", err, asked)
	}
	if err := waitForLocalModel("http://127.0.0.1:1", false, fake); err == nil {
		t.Fatalf("a model that never answers must fail the start")
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
