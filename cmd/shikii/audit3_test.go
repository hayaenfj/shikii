package main

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/decide"
	"github.com/hayaenfj/shikii/internal/spec"
	"github.com/hayaenfj/shikii/internal/store"
)

// 3 回目の監査(2026-10-18)の再現テスト。

// 監査 1: 機械の正解(source=context)は calibrate の聞き直しで source=replay に化けてはいけない(人の正解に数えられてしまう)。
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

// 監査 2: 線は人の正解が min_n 以上ないと書かない。機械の正解だけでは報告のみ。
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

// 監査 3: serve の自動較正は、正解が揃った段の線だけを引く。他の段の線は触らない。
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

// 監査 5: 入力の指紋は段ごと(その段の template が使う入力だけ)。文脈の入力が違っても最小の質問では同じ入力。
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
	// 同じ本文の file 行は同じ入力として 1 つに畳まれる
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
	// 聞き直しの材料: 最小の段の指紋からも本文が見つかる
	st := store.Store{Dir: storeDir}
	texts, err := st.InputsByHash("intent")
	if err != nil || texts[d.Minimal.InputsHash].Inputs["message"] != "fix a" {
		t.Fatalf("texts must be reachable by the minimal inputs hash too: %v %+v", err, texts)
	}
}

// 監査 7・8: status の人の確認数は判定(trace)で数える。level 付きの score では機械の正解を書かない(Top と P が別の事象)。
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
	if !strings.Contains(out, "human-checked so far: 1") {
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
