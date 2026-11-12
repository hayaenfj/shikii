package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/hayaenfj/shikii/internal/notebook"
)

// Audit O of 0.31.0: work states written before the fix (and before 0.31.0) read by the candidate.

// auditOInterruptedResult records a result whose save is interrupted once after the write (the capture memo stays without an id),
// and returns the flow and the receipt.
func auditOInterruptedResult(t *testing.T) (*auditFlow, map[string]any) {
	t.Helper()
	f := newAuditFlow(t)
	first := f.prepare(f.ask(planningTestRequest), "CSV export filters")
	old := workAfterWrite
	failed := false
	workAfterWrite = func(kind string) error {
		if kind == "observation" && !failed {
			failed = true
			return errSyntheticInterrupt
		}
		return nil
	}
	_, err := f.record(first, map[string]any{"result": map[string]any{"changed": "Renamed the button."}})
	workAfterWrite = old
	if err != nil || !failed {
		t.Fatalf("precondition: the first report's save fails once: %v", err)
	}
	return f, first
}

// A capture memo written before 0.31.0 kept no kind (workCapture.Source is new in 0.31.0, and TestAnOlderRecordHandsNoReportOver
// models such memos by setting Source to the zero value) and was keyed by workHash of the observation. The candidate finishes a
// pending memo as an observation of the agent (finishWorkCompletion sets original.source from the memo: the zero value), and
// checkWorkSource refuses the title of the report: the work prepared before the upgrade, whose report's save was interrupted once,
// never closes with its receipt. GREEN control: the same flow with the memo as the candidate writes it closes.
func TestAuditOAPendingReportOfAnOlderMemoIsNeverFinished(t *testing.T) {
	t.Run("control: the memo as the candidate writes it", func(t *testing.T) {
		f, first := auditOInterruptedResult(t)
		b, err := f.record(first, map[string]any{"result": map[string]any{"changed": "Renamed the export button.", "checked": "Looked at it."}})
		if err != nil || b["complete"] != true {
			t.Fatalf("control: %v %.300s", err, auditJSON(b["errors"]))
		}
	})
	t.Run("the memo as an older version wrote it", func(t *testing.T) {
		f, first := auditOInterruptedResult(t)
		moved := 0
		if err := updateSession(f.root, "planning", func(s *hookSession) {
			w := s.Planning.Work
			for key, capture := range w.Captures {
				if capture.ID != "" || capture.Original == nil || capture.Source != notebook.WorkResult() {
					continue
				}
				capture.Source = notebook.WorkSource{} // written before the memo kept the kind
				delete(w.Captures, key)
				w.Captures[workHash(*capture.Original)] = capture // keyed by the observation, as before 0.31.0
				moved++
			}
			syncReasoningCaptures(w)
		}); err != nil {
			t.Fatal(err)
		}
		if moved != 1 {
			t.Fatalf("precondition: one pending capture of the report: %d", moved)
		}
		b, err := f.record(first, map[string]any{"result": map[string]any{"changed": "Renamed the export button.", "checked": "Looked at it."}})
		raw := auditJSON(b["errors"])
		t.Logf("second record: err=%v complete=%v errors=%.400s next=%.200v", err, b["complete"], raw, b["next_action"])
		if b["complete"] != true {
			t.Errorf("a pending report of a memo written before the memo kept its kind is never finished: %.300s", raw)
		}
		c, _ := f.record(first, map[string]any{"result": map[string]any{"changed": "Renamed the export button.", "checked": "Looked at it."}})
		t.Logf("third record (the same again): complete=%v errors=%.300s", c["complete"], auditJSON(c["errors"]))
		again := f.prepare(map[string]any{"receipt": first["receipt"]}, "CSV export filters")
		d, _ := f.record(again, map[string]any{"result": map[string]any{"changed": "Renamed the export button.", "checked": "Looked at it."}})
		t.Logf("after preparing again: complete=%v errors=%.300s", d["complete"], auditJSON(d["errors"]))
		for i := 1; i <= 2; i++ {
			out := runUnreviewedHook(t, "stop", map[string]any{"session_id": "planning", "cwd": f.repo, "last_assistant_message": "Done.", "stop_hook_active": i > 1})
			reason, blocked := stopReason(t, out)
			t.Logf("Stop %d: blocked=%v reason=%.300s out=%.200s", i, blocked, reason, out)
		}
		next := f.prepare(f.ask("Now rename the import button, a new task."), "import button")
		e, _ := f.record(next, map[string]any{"observations": []any{map[string]any{"kind": "test", "title": "Import", "content": "The button reads Import."}}, "result": map[string]any{"changed": "Renamed the import button.", "checked": "Looked at it.", "evidence": []any{"new1"}}})
		t.Logf("the next request's own work: complete=%v errors=%.300s", e["complete"], auditJSON(e["errors"]))
		if e["complete"] != true {
			t.Errorf("the pending report of the older memo goes over to the next request, whose own work cannot close either: %.300s", auditJSON(e["errors"]))
		}
	})
}

// workCapture.Source is read by WorkSource.UnmarshalJSON, which refuses anything but a number. encoding/json hands a JSON null to an
// Unmarshaler of a value field, and the convention (encoding/json's documentation) is to treat it as no value. A memo with
// "source":null makes the whole record unreadable; the uint8 before the fix read it as zero.
func TestAuditOANullSourceMakesTheMemoUnreadable(t *testing.T) {
	var c workCapture
	err := json.Unmarshal([]byte(`{"ref":"observed1","at":"2026-11-12T00:00:00Z","version":"v","source":null}`), &c)
	t.Logf("a memo with source null: %v", err)
	if err != nil && strings.Contains(err.Error(), "unknown work source null") {
		t.Errorf("a capture memo with \"source\":null cannot be read (the uint8 before read it as zero): %v", err)
	}
}

// GREEN check of the memo's JSON: a kind is written as its number (as the uint8 before the fix wrote it), the zero value is left
// out, a memo written before the fix (a number) reads back as the same kind, and a number of no kind is refused.
func TestAuditOTheKindOfAMemoRoundTrips(t *testing.T) {
	for _, k := range notebook.WorkSources() {
		data, err := json.Marshal(workCapture{Ref: "observed1", Source: k})
		if err != nil {
			t.Fatal(err)
		}
		var back workCapture
		if err := json.Unmarshal(data, &back); err != nil || back.Source != k {
			t.Errorf("%q: %s reads back as %+v (%v)", k.Title(), data, back.Source, err)
		}
	}
	if data, _ := json.Marshal(workCapture{Ref: "observed1"}); strings.Contains(string(data), "source") {
		t.Errorf("the zero value is written: %s", data)
	}
	var old workCapture
	if err := json.Unmarshal([]byte(`{"ref":"observed1","at":"2026-11-12T00:00:00Z","version":"v","source":8}`), &old); err != nil || old.Source != notebook.WorkTrialPlan() {
		t.Errorf("a memo written before the fix (source 8) reads as %+v: %v", old.Source, err)
	}
	if data, _ := json.Marshal(workCapture{Ref: "observed1", Source: notebook.WorkTrialPlan()}); !strings.Contains(string(data), `"source":8`) {
		t.Errorf("the kind is not written as the number the uint8 wrote: %s", data)
	}
	for _, bad := range []string{"13", "-1", "255"} {
		var c workCapture
		if err := json.Unmarshal([]byte(`{"ref":"observed1","source":`+bad+`}`), &c); err == nil {
			t.Errorf("a number of no kind (%s) is read: %+v", bad, c.Source)
		}
	}
}
