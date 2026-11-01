package main

import (
	"encoding/json"
	"fmt"
	"github.com/hayaenfj/shikii/internal/notebook"
	"github.com/hayaenfj/shikii/internal/spec"
	"github.com/hayaenfj/shikii/internal/store"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorkPrepareRetainsHumanCorrections(t *testing.T) {
	st, scope, h := reviewFixture(t)
	id, trace := reviewSeed(t, st, scope, 1)
	reviewCall(t, h, map[string]any{"operation": "submit", "source_id": id, "trace_id": trace, "truth": "yes", "reason": "CORRECTION_CONTEXT_MARKER synthetic earlier scoring"})
	capture(t, func() error { return cmdLabel([]string{"--store", st.Dir, "--source", "audit", trace, "no"}) })
	reviewSeed(t, st, scope, 2)
	raw, err := reflectQuery(st.Dir, scope)
	if err != nil {
		t.Fatal(err)
	}
	expected, _ := json.Marshal(raw.Corrections)
	if !strings.Contains(string(expected), "CORRECTION_CONTEXT_MARKER") {
		t.Fatal("\u5408\u6210\u8a02\u6b63\u306e\u6e96\u5099\u306b\u5931\u6557")
	}
	b := notebook.Book{Root: st.Dir, Scope: scope}
	got, err := workCall(t, b, "prepare", map[string]any{"query": "README language"})
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(got)
	if !strings.Contains(string(data), "CORRECTION_CONTEXT_MARKER") {
		t.Fatal("\u65e2\u5b58\u306e\u632f\u308a\u8fd4\u308a\u304c\u8fd4\u3059\u4eba\u306e\u8a02\u6b63\u3092\u6e96\u5099\u304c\u8fd4\u3055\u306a\u3044")
	}
}

func TestWorkPrepareRetainsSettledReviewOriginals(t *testing.T) {
	st, scope, h := reviewFixture(t)
	id, trace := reviewSeed(t, st, scope, 1)
	rec, err := st.FindRecord("done-as-asked", trace)
	if err != nil {
		t.Fatal(err)
	}
	original := rec.Inputs["summary"] + strings.Repeat("x", 1800) + " FULL_ORIGINAL_MARKER retain date filters"
	if err = st.SaveRecordText(*rec, map[string]string{"summary": original}); err != nil {
		t.Fatal(err)
	}
	reviewCall(t, h, map[string]any{"operation": "submit", "source_id": id, "trace_id": trace, "truth": "unknown", "reason": "Synthetic result is not established."})
	b := notebook.Book{Root: st.Dir, Scope: scope}
	view, err := b.ReadSources([]string{id})
	if err != nil {
		t.Fatal(err)
	}
	available, _ := json.Marshal(view)
	if !strings.Contains(string(available), "FULL_ORIGINAL_MARKER") {
		t.Fatal("\u539f\u6587\u304c\u4fdd\u5b58\u3055\u308c\u3066\u3044\u306a\u3044")
	}
	got, err := workCall(t, b, "prepare", map[string]any{"query": "README language"})
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(got)
	if !strings.Contains(string(data), "FULL_ORIGINAL_MARKER") {
		t.Fatal("\u63a1\u70b9\u6e08\u307f\u3067\u77e5\u8b58\u3078\u306e\u6574\u7406\u304c\u6b8b\u308b\u4f1a\u8a71\u306e\u539f\u6587\u3092\u6e96\u5099\u304c\u8fd4\u3055\u306a\u3044")
	}
}

// A complete conversation can have its score settled before its wiki work.
func workPrepareSourceFixture(t *testing.T, original string, legacy bool) (notebook.Book, string) {
	t.Helper()
	st, scope, h := reviewFixture(t)
	sp, err := spec.Load(filepath.Join(st.Dir, "decisions", "done-as-asked.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	summary := "Synthetic complete summary."
	if legacy {
		summary = strings.Repeat("s", 1500)
	}
	id, trace := store.NewTraceID(), store.NewTraceID()
	inputs := map[string]string{"request": "Synthetic source request", "summary": summary, "files": "source.go"}
	raw, _ := json.Marshal(map[string]any{"trace_id": trace, "spec_id": sp.ID, "spec_hash": sp.Hash(), "type": "noul", "model": "test-model", "inputs": inputs, "inputs_hash": store.HashInputs(inputs), "top": "yes", "p": 0.9, "band": "ask", "review_pending": true})
	var rec store.Record
	if err = json.Unmarshal(raw, &rec); err != nil {
		t.Fatal(err)
	}
	if err = st.AppendRecord(rec); err != nil {
		t.Fatal(err)
	}
	raw, _ = json.Marshal(map[string]any{"id": id, "request": inputs["request"], "summary": summary, "reply": "Synthetic complete reply", "targets": []map[string]string{{"spec": sp.ID, "trace_id": trace}}})
	var source store.Feedback
	if err = json.Unmarshal(raw, &source); err != nil {
		t.Fatal(err)
	}
	if err = st.AppendFeedback(scope, source); err != nil {
		t.Fatal(err)
	}
	if original != "" {
		if err = st.SaveRecordText(rec, map[string]string{"summary": summary + original}); err != nil {
			t.Fatal(err)
		}
	}
	reviewCall(t, h, map[string]any{"operation": "submit", "source_id": id, "trace_id": trace, "truth": "unknown", "reason": "Synthetic outcome is not established."})
	return notebook.Book{Root: st.Dir, Scope: scope}, id
}

func TestWorkPrepareSourceBudgetReturnsUsableContinuation(t *testing.T) {
	const marker = "SOURCE_BUDGET_END_MARKER"
	b, id := workPrepareSourceFixture(t, strings.Repeat("<", 48<<10)+marker, false)
	got, err := workCall(t, b, "prepare", map[string]any{"query": "synthetic source"})
	if err != nil {
		t.Fatal(err)
	}
	pending := got["pending"].([]any)
	if len(pending) != 1 {
		t.Fatal(got)
	}
	source := pending[0].(map[string]any)
	if source["head"] != true || source["ref"] != nil {
		t.Fatal("a partial source must not receive a writable recording reference", source)
	}
	query, ok := source["source_query"].(map[string]any)
	if !ok {
		t.Fatal("a partial source needs a usable query", source)
	}
	if query["operation"] != "query" || query["sources"].([]any)[0] != id {
		t.Fatal(query)
	}
	page := source["source"].(map[string]any)
	joined := ""
	parts := 0
	for {
		if piece, ok := page["original_part"].(map[string]any); ok {
			joined += piece["json"].(string)
			parts++
		}
		cursor, _ := page["next_cursor"].(string)
		if cursor == "" {
			break
		}
		if parts > 32 {
			t.Fatal("source pagination did not finish")
		}
		value, e := improvementCall(t, b, map[string]any{"operation": "query", "sources": []string{id}, "cursor": cursor})
		if e != nil {
			t.Fatal(e)
		}
		page = loopObject(t, value)
	}
	if !strings.Contains(joined, marker) {
		t.Fatal("the deeper query must recover the end of the saved original")
	}
	rejected, err := workCall(t, b, "record", map[string]any{"receipt": got["receipt"], "reviewed": []string{"source1"}})
	if err != nil || rejected["complete"] != false {
		t.Fatal("partial source alias must not be accepted", rejected, err)
	}
}

func TestWorkPrepareSourceShortageRemainsExplicit(t *testing.T) {
	b, _ := workPrepareSourceFixture(t, "", true)
	got, err := workCall(t, b, "prepare", map[string]any{"query": "synthetic source"})
	if err != nil {
		t.Fatal(err)
	}
	pending := got["pending"].([]any)
	if len(pending) != 1 {
		t.Fatal(got)
	}
	source := pending[0].(map[string]any)
	raw, _ := json.Marshal(source)
	if source["head"] != true || source["ref"] != nil || !strings.Contains(string(raw), `"legacy"`) {
		t.Fatal("unavailable historical originals must be explicit before recording", source)
	}
	if source["source_query"] == nil {
		t.Fatal("source limits need a concrete read instruction", source)
	}
}

func TestWorkPrepareSupportingContextRetainsCorrectionsWithinBudget(t *testing.T) {
	view := reflectView{Corrections: []any{map[string]any{"human_truth": "no", "ai_truth": "yes", "ai_reason": "RETAIN_CORRECTION_CONTEXT"}}}
	for i := 0; i < 20; i++ {
		view.Lessons = append(view.Lessons, notebook.Page{ID: fmt.Sprintf("%016x", i+1), Revision: fmt.Sprintf("%016x", i+101), Title: "Related source-backed procedure", Lesson: "Check before applying.", Body: strings.Repeat("<", 16000), Kind: "howto", Scene: "checking"})
	}
	context, err := preparedReflectionContext(view)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(context)
	if len(raw) > workSourceBudget || !strings.Contains(string(raw), "RETAIN_CORRECTION_CONTEXT") {
		t.Fatal("supporting evidence must retain corrections inside its encoded budget", len(raw))
	}
	references, full := 0, 0
	for _, entry := range context["lessons"].([]any) {
		page := entry.(map[string]any)
		if page["incomplete"] == true {
			references++
			query := page["query"].(map[string]any)
			if query["operation"] != "query" || !strings.Contains(query["query"].(string), "@") {
				t.Fatal("omitted article needs an exact revision query", query)
			}
		} else if page["body"] != "" {
			full++
		}
	}
	if references == 0 || full == 0 || context["incomplete"] != true {
		t.Fatal("large related articles need explicit bounded references", references, full, context)
	}
	view.Corrections = strings.Repeat("<", 128<<10)
	if _, err = preparedReflectionContext(view); err == nil {
		t.Fatal("oversized corrections must fail explicitly rather than disappear")
	}
}
