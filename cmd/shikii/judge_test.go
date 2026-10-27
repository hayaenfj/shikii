package main

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/store"
)

// Reproduction tests for audit round 4.

// 2: the judge gives no truth to inputs that have a human truth (re-decided in another trace).
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
	// The same text "fix a" was re-decided in another trace (the human truth is above)
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
	// Also report the agreement on inputs the decision model got wrong (a hint whether a judge of the same family can fill the gaps)
	if !strings.Contains(out, "on inputs the decision model got wrong") {
		t.Fatalf("judge must report its agreement on the inputs the decision model missed:\n%s", out)
	}
}
