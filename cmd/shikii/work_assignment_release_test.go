package main

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestAssignmentScopeFiltersAllAutomaticOriginals(t *testing.T) {
	for _, scope := range []string{"other", "private", ""} {
		t.Run("scope="+scope, func(t *testing.T) {
			b, _ := loopBook(t)
			a := assignmentInput("prior", "group", "researcher", "research")
			a["evidence_scope"] = "private"
			v, err := workCall(t, b, "prepare", map[string]any{"assignment": a, "query": "quartz"})
			if err != nil {
				t.Fatal(err)
			}
			marker := "SYNTHETIC_PRIVATE_REPORT"
			r, err := workCall(t, b, "record", map[string]any{"assignment": a, "receipt": v["receipt"], "event": map[string]any{"id": "result", "state": "completed", "summary": marker}})
			if err != nil || r["complete"] != true {
				t.Fatal(r, err)
			}
			a["id"], a["evidence_scope"] = "next", scope
			next, err := workCall(t, b, "prepare", map[string]any{"assignment": a, "query": "quartz"})
			if err != nil {
				t.Fatal(err)
			}
			raw, _ := json.Marshal(next)
			if strings.Contains(string(raw), marker) != (scope != "other") {
				t.Fatal("original report delivery must respect the evidence scope in every automatic field")
			}
		})
	}
}

func TestAssignmentOversizeEventDoesNotReserveItsIdentity(t *testing.T) {
	for _, escaping := range []bool{false, true} {
		t.Run(map[bool]string{false: "combined fields", true: "encoded text"}[escaping], func(t *testing.T) {
			b, _ := loopBook(t)
			a := assignmentInput("job", "group", "worker", "work")
			event := map[string]any{"id": "result", "state": "completed", "summary": strings.Repeat("<", 12000)}
			if !escaping {
				a["purpose"] = strings.Repeat("p", 8000)
				xs := []string{}
				for range 16 {
					xs = append(xs, strings.Repeat("x", 1000))
				}
				event["summary"], event["unknowns"], event["contradictions"], event["method"], event["reference"] = strings.Repeat("s", 24000), xs, xs, strings.Repeat("m", 2000), strings.Repeat("r", 1000)
			}
			v, err := workCall(t, b, "prepare", map[string]any{"assignment": a, "query": "quartz"})
			if err != nil {
				t.Fatal(err)
			}
			req := map[string]any{"assignment": a, "receipt": v["receipt"], "event": event}
			if r, err := workCall(t, b, "record", req); err == nil {
				t.Fatal("an unsaveable derived observation must fail before reservation", r["complete"])
			}
			event["summary"] = "A bounded synthetic result."
			if r, err := workCall(t, b, "record", req); err != nil || r["complete"] != true {
				t.Fatal("a rejected event must leave its identity available for correction", r, err)
			}
		})
	}

	t.Run("capture capacity", func(t *testing.T) {
		b, _ := loopBook(t)
		a := assignmentInput("job", "group", "worker", "work")
		v, err := workCall(t, b, "prepare", map[string]any{"assignment": a, "query": "quartz"})
		if err != nil {
			t.Fatal(err)
		}
		for i := range 32 {
			if r, err := workCall(t, b, "record", map[string]any{"assignment": a, "receipt": v["receipt"], "observations": []any{map[string]any{"title": "Synthetic capacity evidence", "content": strings.Repeat("x", i+1)}}}); err != nil || r["complete"] != true {
				t.Fatal(r, err)
			}
		}
		req := map[string]any{"assignment": a, "receipt": v["receipt"], "event": map[string]any{"id": "result", "state": "completed", "summary": "A bounded result."}}
		if r, err := workCall(t, b, "record", req); err == nil {
			t.Fatal("full observation capacity must reject before reserving an event", r["complete"])
		}
		v, err = workCall(t, b, "prepare", map[string]any{"assignment": a, "query": "quartz"})
		if err != nil {
			t.Fatal(err)
		}
		req["receipt"] = v["receipt"]
		if r, err := workCall(t, b, "record", req); err != nil || r["complete"] != true {
			t.Fatal("a preflight refusal must leave the event ID available on a fresh checkpoint", r, err)
		}
	})
}

func TestAssignmentEventSourceCannotUseAnAdditionalObservation(t *testing.T) {
	b, _ := loopBook(t)
	a := assignmentInput("job", "group", "worker", "work")
	v, err := workCall(t, b, "prepare", map[string]any{"assignment": a, "query": "quartz"})
	if err != nil {
		t.Fatal(err)
	}
	req := map[string]any{"assignment": a, "receipt": v["receipt"], "event": map[string]any{"id": "result", "state": "completed", "summary": "SYNTHETIC_EVENT"}, "observations": []any{map[string]any{"kind": "agent", "title": "Additional evidence", "content": "SYNTHETIC_OTHER_OBSERVATION"}}}
	original := workAfterWrite
	t.Cleanup(func() { workAfterWrite = original })
	first := true
	workAfterWrite = func(kind string) error {
		if kind == "observation" && first {
			first = false
			return errors.New("synthetic interrupted capture")
		}
		return nil
	}
	r, err := workCall(t, b, "record", req)
	if err != nil || r["complete"] != false {
		t.Fatal(r, err)
	}
	dir, err := assignmentDir(b.Root, b.Scope)
	if err != nil {
		t.Fatal(err)
	}
	defer dir.Close()
	record, err := readAssignment(dir, assignmentName("job"))
	if err != nil {
		t.Fatal(err)
	}
	state, _ := assignmentState(record)
	if record.Events["result"].Source != "" || state != "active" {
		t.Fatal("the additional observation cannot complete the uncaptured event", state)
	}
	workAfterWrite = original
	r, err = workCall(t, b, "record", req)
	if err != nil || r["complete"] != true {
		t.Fatal("the original event must recover on its identical retry", r, err)
	}
	record, err = readAssignment(dir, assignmentName("job"))
	if err != nil {
		t.Fatal(err)
	}
	if record.Events["result"].Source == "" {
		t.Fatal("the captured event needs its own source")
	}
}
