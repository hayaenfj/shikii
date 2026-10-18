package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/decide"
	"github.com/hayaenfj/shikii/internal/spec"
	"github.com/hayaenfj/shikii/internal/store"
)

// 冷たい出発を温める: 手元の過去の表(CSV: 入力の列 + truth [+ weight])を、calibrate / tune が読む正解の貯金(JSON)にする。
// 列名は spec の入力名と一致していること。truth は spec の選択肢(noul は yes/no/true/false)。
func TestImportTurnsACSVIntoTruths(t *testing.T) {
	dir := t.TempDir()
	specPath := writeSpec(t, dir, "false")
	csvPath := filepath.Join(dir, "past.csv")
	src := "message,truth,weight\nfix the typo,change_request,\n\"why, though?\",question,0.5\nok thanks,other,\n"
	if err := os.WriteFile(csvPath, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "truths.json")
	got := capture(t, func() error { return cmdImport([]string{csvPath, "--spec", specPath, "--out", out}) })
	if !strings.Contains(got, "3 truths") {
		t.Fatalf("import must say how many rows it wrote:\n%s", got)
	}
	var rows []labeled
	b, _ := os.ReadFile(out)
	if err := json.Unmarshal(b, &rows); err != nil || len(rows) != 3 {
		t.Fatalf("the output is the labeled JSON calibrate reads: %v %s", err, b)
	}
	if rows[1].Inputs["message"] != "why, though?" || string(rows[1].Truth) != `"question"` || rows[1].Weight != 0.5 || rows[0].Weight != 0 {
		t.Fatalf("rows: %+v", rows)
	}
	// 知らない選択肢・足りない列は行番号付きで誤り(黙って捨てない)
	bad := "message,truth\nfix,nope\n"
	if err := os.WriteFile(csvPath, []byte(bad), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := cmdImport([]string{csvPath, "--spec", specPath, "--out", out}); err == nil || !strings.Contains(err.Error(), "row 1") || !strings.Contains(err.Error(), "nope") {
		t.Fatalf("an unknown truth must fail with the row number: %v", err)
	}
	if err := os.WriteFile(csvPath, []byte("text,truth\nfix,change_request\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := cmdImport([]string{csvPath, "--spec", specPath, "--out", out}); err == nil || !strings.Contains(err.Error(), "message") {
		t.Fatalf("a missing required input column must fail naming the column: %v", err)
	}
}

// 領収書: act の判定には「何を根拠に動いてよいと言ったか」が付く。線の鍵・方法・引いた日・正解の数(人の分)・人が確かめた act の精度。線が無ければ領収書も無い。
func TestActDecisionsCarryAReceipt(t *testing.T) {
	dir := t.TempDir()
	srv := fakeSystemOne(t, "m", map[string]float64{"change_request": 0.9, "question": 0.1, "other": 0})
	specPath := writeSpec(t, dir, "false")
	storeDir := filepath.Join(dir, ".shikii")
	out := capture(t, func() error {
		return cmdDecide([]string{specPath, "-i", "message=fix a", "--local", srv.URL, "--store", storeDir, "--json"})
	})
	if strings.Contains(out, `"receipt"`) {
		t.Fatalf("no line, no receipt:\n%s", out)
	}
	sp := mustLoad(t, specPath)
	sp.PutCalibration(spec.Calibration{Model: "m", SpecHash: sp.Hash(), Method: "identity", N: 4, NHuman: 4, FittedAt: "2026-10-18", PrecisionAt: 1})
	if err := sp.Save(); err != nil {
		t.Fatal(err)
	}
	// 人が確かめた act の判定を 2 件(どちらも当たり)
	st := store.Store{Dir: storeDir}
	for _, m := range []string{"fix b", "fix c"} {
		in := map[string]string{"message": m}
		if err := st.AppendLabel(store.Label{TraceID: store.NewTraceID(), SpecID: "intent", SpecHash: sp.Hash(), Type: "choice", Model: "m", InputsHash: store.HashInputs(in), Inputs: in,
			Raw: map[string]float64{"change_request": 0.9, "question": 0.1, "other": 0}, Truth: "change_request", Weight: 1, Source: "audit", At: time.Now()}); err != nil {
			t.Fatal(err)
		}
	}
	out = capture(t, func() error {
		return cmdDecide([]string{specPath, "-i", "message=fix a", "--local", srv.URL, "--store", storeDir, "--json"})
	})
	var d struct {
		Band    string          `json:"band"`
		Receipt *decide.Receipt `json:"receipt"`
	}
	if err := json.Unmarshal([]byte(out), &d); err != nil {
		t.Fatal(err)
	}
	if d.Band != "act" || d.Receipt == nil || d.Receipt.Method != "identity" || d.Receipt.NHuman != 4 || d.Receipt.FittedAt != "2026-10-18" || d.Receipt.HumanCheckedAct != 2 || d.Receipt.HumanPrecision != 1 {
		t.Fatalf("receipt: %+v (band %s)", d.Receipt, d.Band)
	}
	text := capture(t, func() error {
		return cmdDecide([]string{specPath, "-i", "message=fix a", "--local", srv.URL, "--store", storeDir})
	})
	if !strings.Contains(text, "receipt:") || !strings.Contains(text, "human-checked act decisions 2") {
		t.Fatalf("the text output names the receipt:\n%s", text)
	}
}

// 審判: 別の端点(より大きいモデル)に同じ spec を聞き、人の正解との一致率が act に届く時だけ、正解の無い記録に重み付きの正解(source judge)を付ける。
func TestJudgeLabelsOnlyWhenItAgreesWithPeople(t *testing.T) {
	dir := t.TempDir()
	specPath := writeSpec(t, dir, "false") // min_n 2
	storeDir := filepath.Join(dir, ".shikii")
	st := store.Store{Dir: storeDir}
	sp := mustLoad(t, specPath)
	addHuman := func(m, truth string) {
		in := map[string]string{"message": m}
		if err := st.AppendLabel(store.Label{TraceID: store.NewTraceID(), SpecID: "intent", SpecHash: sp.Hash(), Type: "choice", Model: "small", InputsHash: store.HashInputs(in), Inputs: in,
			Raw: map[string]float64{"change_request": 0.5, "question": 0.5, "other": 0}, Truth: truth, Weight: 1, Source: "ui", At: time.Now()}); err != nil {
			t.Fatal(err)
		}
	}
	addHuman("fix a", "change_request")
	addHuman("why a", "question")
	unlabeled := store.Record{TraceID: store.NewTraceID(), SpecID: "intent", SpecHash: sp.Hash(), Type: "choice", Model: "small", Inputs: map[string]string{"message": "fix z"},
		InputsHash: store.HashInputs(map[string]string{"message": "fix z"}), Raw: map[string]float64{"change_request": 0.6, "question": 0.4, "other": 0}, Top: "change_request", P: 0.6, Band: "ask", At: time.Now()}
	if err := st.AppendRecord(unlabeled); err != nil {
		t.Fatal(err)
	}
	// 良い審判: 本文を読んで当てる
	good := fakeByMessage(t, "big", map[string]map[string]float64{
		"fix a": {"change_request": 0.95, "question": 0.05, "other": 0}, "why a": {"change_request": 0.05, "question": 0.95, "other": 0}, "fix z": {"change_request": 0.9, "question": 0.1, "other": 0}})
	out := capture(t, func() error {
		return cmdJudge([]string{specPath, "--local", good.URL, "--model", "big", "--store", storeDir})
	})
	if !strings.Contains(out, "agreement 1.000") || !strings.Contains(out, "labeled 1") {
		t.Fatalf("a judge that agrees with people labels the unlabeled records:\n%s", out)
	}
	labels := readLabels(t, storeDir, "intent")
	var judged []store.Label
	for _, l := range labels {
		if l.Source == "judge" {
			judged = append(judged, l)
		}
	}
	if len(judged) != 1 || judged[0].TraceID != unlabeled.TraceID || judged[0].Truth != "change_request" || judged[0].Weight != 1 || judged[0].Raw["change_request"] != 0.6 || judged[0].Model != "small" {
		t.Fatalf("the judge's truth is written against the small model's record (its raw, its model) with the agreement as weight: %+v", judged)
	}
	if store.HumanSource("judge") {
		t.Fatalf("a judge's truth is a machine truth")
	}
	// 悪い審判: 人と食い違う → 正解を書かない
	bad := fakeByMessage(t, "big2", map[string]map[string]float64{
		"fix a": {"change_request": 0.05, "question": 0.95, "other": 0}, "why a": {"change_request": 0.95, "question": 0.05, "other": 0}, "fix z": {"change_request": 0.9, "question": 0.1, "other": 0}})
	var buf strings.Builder
	old := stdout
	stdout = &buf
	err := cmdJudge([]string{specPath, "--local", bad.URL, "--model", "big2", "--store", storeDir})
	stdout = old
	if err == nil || !strings.Contains(err.Error(), "agreement") {
		t.Fatalf("a judge below act must refuse: %v\n%s", err, buf.String())
	}
	if len(readLabels(t, storeDir, "intent")) != 3 {
		t.Fatalf("the bad judge must not have written labels")
	}
}
