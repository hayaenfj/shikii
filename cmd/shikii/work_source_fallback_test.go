package main

import (
	"encoding/json"
	"github.com/hayaenfj/shikii/internal/store"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorkFeedbackFallbackRejectsChangedSource(t *testing.T) {
	b, _ := loopBook(t)
	first, err := workCall(t, b, "prepare", map[string]any{"request": "Check synthetic source data", "query": "unmatched"})
	if err != nil {
		t.Fatal(err)
	}
	st := store.Store{Dir: b.Root}
	f := store.Feedback{ID: "00000000000000a1", Request: "Synthetic request", Summary: "Synthetic result", Reply: "Initial condition"}
	if err = st.AppendFeedback(b.Scope, f); err != nil {
		t.Fatal(err)
	}
	changed := false
	reply, err := prepareWork(b.Root, b.Scope, workRequest{Receipt: first["receipt"].(string), Query: f.ID, responseSize: func(v any) int {
		if !changed {
			changed = true
			f.Reply = "Changed condition"
			if e := st.AppendFeedback(b.Scope, f); e != nil {
				t.Fatal(e)
			}
		}
		return workContextSize(v)
	}})
	if err == nil || !strings.Contains(err.Error(), "changed while reading") {
		t.Fatalf("a changed feedback fallback must fail before commit: %v, %T", err, reply)
	}
	next, err := workCall(t, b, "prepare", map[string]any{"receipt": first["receipt"], "query": f.ID})
	if err != nil || next["delivery"] != first["delivery"].(float64)+1 || next["task"] != first["task"] {
		t.Fatal("a failed source read must leave its receipt and delivery unchanged", next, err)
	}
}

func TestWorkOriginalFallbackRejectsChangedText(t *testing.T) {
	b, _ := loopBook(t)
	first, err := workCall(t, b, "prepare", map[string]any{"request": "Check synthetic source data", "query": "unmatched"})
	if err != nil {
		t.Fatal(err)
	}
	st := store.Store{Dir: b.Root}
	trace := "00000000000000b1"
	f := store.Feedback{ID: "00000000000000b2", Request: "Synthetic request", Summary: "Synthetic result", Reply: "Synthetic reply", Targets: []store.ReviewTarget{{Spec: "done-as-asked", TraceID: trace}}}
	rec := store.Record{TraceID: trace, SpecID: "done-as-asked", InputsHash: "synthetic", Inputs: map[string]string{"request": f.Request, "summary": f.Summary}}
	if err = st.AppendRecord(rec); err != nil {
		t.Fatal(err)
	}
	if err = st.SaveRecordText(rec, map[string]string{"summary": f.Summary + " Condition unresolved"}); err != nil {
		t.Fatal(err)
	}
	if err = st.AppendFeedback(b.Scope, f); err != nil {
		t.Fatal(err)
	}
	changed := false
	reply, err := prepareWork(b.Root, b.Scope, workRequest{Receipt: first["receipt"].(string), Query: f.ID, responseSize: func(v any) int {
		if !changed {
			changed = true
			raw, e := json.Marshal(map[string]any{"inputs_hash": rec.InputsHash, "inputs": map[string]string{"summary": f.Summary + " Condition resolved"}})
			if e != nil {
				t.Fatal(e)
			}
			if e = os.WriteFile(filepath.Join(b.Root, "records", "text", trace+".json"), raw, 0600); e != nil {
				t.Fatal(e)
			}
		}
		return workContextSize(v)
	}})
	if err == nil || !strings.Contains(err.Error(), "changed while reading") {
		t.Fatalf("a changed original fallback must fail before commit: %v, %T", err, reply)
	}
	next, err := workCall(t, b, "prepare", map[string]any{"receipt": first["receipt"], "query": f.ID})
	if err != nil || next["delivery"] != first["delivery"].(float64)+1 || next["task"] != first["task"] {
		t.Fatal("a failed original read must leave its receipt and delivery unchanged", next, err)
	}
}
