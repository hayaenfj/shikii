package main

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/store"
)

// 監査 4 回目の再現テスト。

// 2: 審判は、人の正解がある入力(別の trace で再判定されたもの)には正解を付けない。
func TestJudgeSkipsInputsThatPeopleAlreadyAnswered(t *testing.T) {
	dir := t.TempDir()
	specPath := writeSpec(t, dir, "false")
	storeDir := filepath.Join(dir, ".shikii")
	st := store.Store{Dir: storeDir}
	sp := mustLoad(t, specPath)
	in := func(m string) map[string]string { return map[string]string{"message": m} }
	raw := map[string]float64{"change_request": 0.5, "question": 0.5, "other": 0}
	for _, m := range []string{"fix a", "why a"} {
		truth := "change_request"
		if m == "why a" {
			truth = "question"
		}
		if err := st.AppendLabel(store.Label{TraceID: store.NewTraceID(), SpecID: "intent", SpecHash: sp.Hash(), Type: "choice", Model: "small", InputsHash: store.HashInputs(in(m)), Inputs: in(m), Raw: raw, Truth: truth, Weight: 1, Source: "ui", At: time.Now()}); err != nil {
			t.Fatal(err)
		}
	}
	// 同じ本文 "fix a" が別の trace で再判定されている(人の正解は上にある)
	again := store.Record{TraceID: store.NewTraceID(), SpecID: "intent", SpecHash: sp.Hash(), Type: "choice", Model: "small", Inputs: in("fix a"), InputsHash: store.HashInputs(in("fix a")), Raw: raw, Top: "change_request", P: 0.5, Band: "ask", At: time.Now()}
	if err := st.AppendRecord(again); err != nil {
		t.Fatal(err)
	}
	good := fakeByMessage(t, "big", map[string]map[string]float64{"fix a": {"change_request": 0.95, "question": 0.05, "other": 0}, "why a": {"change_request": 0.05, "question": 0.95, "other": 0}})
	out := capture(t, func() error {
		return cmdJudge([]string{specPath, "--local", good.URL, "--model", "big", "--store", storeDir})
	})
	if !strings.Contains(out, "labeled 0") || !strings.Contains(out, "already answered by people") {
		t.Fatalf("an input people answered must be skipped even under a new trace:\n%s", out)
	}
	// 判定モデルが外した入力での一致率も出す(同じ系統の審判が穴を埋められるかの目安)
	if !strings.Contains(out, "on inputs the decision model got wrong") {
		t.Fatalf("judge must report its agreement on the inputs the decision model missed:\n%s", out)
	}
}

// 9: raw 付きの行は、鍵が揃っていて 0..1 で、和が 1 に近いものだけ。noul の数字も 0..1。
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

// 10: import は Excel の UTF-8 BOM を剥がし、同じ列名が 2 つあれば誤りにする。
func TestImportHandlesBOMAndDuplicateColumns(t *testing.T) {
	dir := t.TempDir()
	specPath := writeSpec(t, dir, "false")
	csvPath := filepath.Join(dir, "past.csv")
	out := filepath.Join(dir, "truths.json")
	if err := os.WriteFile(csvPath, []byte("\xef\xbb\xbfmessage,truth\nfix a,change_request\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := capture(t, func() error { return cmdImport([]string{csvPath, "--spec", specPath, "--out", out}) })
	if !strings.Contains(got, "1 truths") {
		t.Fatalf("a BOM must not hide the first column:\n%s", got)
	}
	if err := os.WriteFile(csvPath, []byte("message,truth,message\nfix a,change_request,fix b\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := cmdImport([]string{csvPath, "--spec", specPath, "--out", out}); err == nil || !strings.Contains(err.Error(), "twice") {
		t.Fatalf("a duplicated column must be an error, got %v", err)
	}
}

// 11・12・7: 画面の wiki の口。配置違いの同じ土台は 1 つのモデルとして数える。下書きのまま confirmed にはできない。前書きの知らない項目は消えない。
func TestWikiClaimRulesFromAudit4(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	specsDir := filepath.Join(dir, "decisions")
	if err := os.MkdirAll(specsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeSpec(t, specsDir, "false")
	pdir := filepath.Join(dir, ".shikii", "wiki", "patterns")
	if err := os.MkdirAll(filepath.Join(pdir, "drafts"), 0o755); err != nil {
		t.Fatal(err)
	}
	page := "---\nclaim: c\nstatus: draft\nauthor: someone\nevidence:\n  - date: \"2026-10-18\"\n    model: m+state-last\n    result: r\n    verdict: supports\n  - date: \"2026-10-18\"\n    model: m+plain\n    result: r\n    verdict: supports\nretest:\n  spec: decisions/intent.yaml\n  better: plain\n  than: current\n---\n# d\n"
	draft := filepath.Join(pdir, "drafts", "intent-plain.md")
	if err := os.WriteFile(draft, []byte(page), 0o644); err != nil {
		t.Fatal(err)
	}
	var ef endpointFlags
	ef.model, ef.local = "m", "http://127.0.0.1:1"
	s := httptest.NewServer(newServer(serveOpts{specsDir: specsDir, storeDir: filepath.Join(dir, ".shikii"), ef: ef}))
	t.Cleanup(s.Close)
	var list struct {
		Claims []map[string]any `json:"claims"`
	}
	getJSON(t, s.URL+"/v1/wiki", &list)
	if len(list.Claims) != 1 || list.Claims[0]["confirmable"] != false || list.Claims[0]["supporting_models"] != 1.0 {
		t.Fatalf("two layouts of one base model are one model: %+v", list.Claims)
	}
	if code, _ := postJSON(t, s.URL+"/v1/wiki/claim", map[string]any{"path": "patterns/drafts/intent-plain.md", "action": "status", "status": "confirmed"}); code != 409 {
		t.Fatalf("a draft must be promoted before it can be confirmed, got %d", code)
	}
	pg, body, err := readClaimPage(draft)
	if err != nil {
		t.Fatal(err)
	}
	pg.Evidence = append(pg.Evidence, claimEvidence{Date: "2026-10-18", Model: "x", Result: "r", Verdict: "supports"})
	if err := writeClaimPage(draft, pg, body); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(draft)
	if !strings.Contains(string(b), "author: someone") {
		t.Fatalf("a key the machine does not know must survive a machine write:\n%s", b)
	}
}
