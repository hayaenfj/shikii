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

// 監査 5 の 8: serve の起動は --local があればモデルの口を待ち、無ければ(hosted だけ)待たない。待ちの失敗は起動の失敗。
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
	// 鍵があって Jev を使う時は、--local があっても待たない(手元のモデルは起こさない決まりなので、待つと画面が開かない。2026-10-23)
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

// 過去の判定をやり直す(本人の求め、2026-10-18): (1) 答えたものの一覧と答えの直し(新しい答えが勝つ)、
// (2) AI にもう一度聞く: 今の聞き方と線で同じ本文を再判定し、redo_of 付きの新しい記録を残す。元の記録は列から消え、人の答えは新しい記録に写る。
func TestPastDecisionsCanBeRedone(t *testing.T) {
	url, _, storeDir := newServeFixture(t, "false", map[string]float64{"change_request": 0.6, "question": 0.4, "other": 0})
	code, d := postJSON(t, url+"/v1/decide", map[string]any{"spec": "intent", "inputs": map[string]string{"message": "fix the typo"}})
	if code != 200 {
		t.Fatalf("decide: %d %v", code, d)
	}
	trace := d["trace_id"].(string)
	// まだ答えていない記録をやり直す → 元は列から消え、新しい記録が出る
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
	// 人が答える → 答えたものに出る。答えを直すと新しい答えが使われる
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
	// 答えた記録をやり直す → 人の答えは新しい記録に写り、前と今の正否が返る
	code, res = postJSON(t, url+"/v1/redo", map[string]any{"spec": "intent", "trace_id": newTrace})
	if code != 200 || res["truth"] != "change_request" || res["correct_before"] != true || res["correct_after"] != true {
		t.Fatalf("redo of an answered decision carries the answer and compares: %d %v", code, res)
	}
	// 監査 6 の 4: 回答済みは元を出さず 1 件(redo_of 付き)、回答済みの数は人が答えた本文の数 = 1 のまま
	getJSON(t, url+"/v1/answered?spec=intent", &a)
	if len(a.Items) != 1 || a.Items[0]["redo_of"] != newTrace {
		t.Fatalf("the redone record replaces the original in the answered list too: %v", a.Items)
	}
	var stat struct{ Specs []map[string]any }
	getJSON(t, url+"/v1/status", &stat)
	if stat.Specs[0]["labels"] != 1.0 {
		t.Fatalf("answered count is the number of texts a person answered (1), got %v", stat.Specs[0]["labels"])
	}
	// 監査 6 の 8a: limit(2 件答えてから 1 件に絞る)
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
	// 本文が無い記録はやり直せない
	if err := st.AppendRecord(store.Record{TraceID: "old", SpecID: "intent", SpecHash: "h", Type: "choice", Band: "ask", InputsHash: "x", At: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if code, _ := postJSON(t, url+"/v1/redo", map[string]any{"spec": "intent", "trace_id": "old"}); code != 409 {
		t.Fatalf("no text → 409, got %d", code)
	}
	// 機械の正解(context / judge)しか付いていない記録は「答えたもの」ではない
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
	// 画面に部品がある
	body := uiText(t, url)
	for _, must := range []string{"v1/answered", "v1/redo", "回答済み", "AI にもう一度聞く", "別のボタンを押すと答えを直せます"} {
		if !strings.Contains(body, must) {
			t.Fatalf("UI must offer the redo: missing %q", must)
		}
	}
}

// 監査 6 の 5・8c: やり直しは /v1/decide と同じ作法で記録する。文脈の段で act に届けば最小の段に機械の正解(source context)が残り、
// 人の答えの写しは 2 段(文脈のハッシュと最小のハッシュ)に付く。
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
	// 人が元に答えてから、やり直す → 写しは 2 段
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
