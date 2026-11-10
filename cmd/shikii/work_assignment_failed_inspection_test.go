package main

import (
	"encoding/json"
	"testing"
)

// An assignment counts a supplied article as inspected only when recording that inspection succeeded. Here the batch stops at a
// note that names an unknown reference, before the reviewed article is recorded: the reply is incomplete and saves no inspection.
// The next execution of the group must then get no "change since the inspected revision" for that article. Once the inspection is
// recorded on its own, the change is reported.
func TestAssignmentDoesNotCountAFailedInspectionAsInspected(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	b, s := loopBook(t)
	p := loopPage(t, b, s, map[string]any{"title": "Quartz ownership", "body": "Initial ownership condition."})
	first := assignmentInput("worker-1", "case-1", "worker", "work")
	v, err := workCall(t, b, "prepare", map[string]any{"query": "lesson:" + p.ID, "assignment": first})
	if err != nil {
		t.Fatal(err)
	}
	failed, err := workCall(t, b, "record", map[string]any{"assignment": first, "receipt": v["receipt"], "reviewed": []string{"knowledge1"}, "notes": []any{map[string]any{"ref": "note9", "target": "new"}}})
	if err != nil {
		t.Fatal(err)
	}
	saved, _ := json.Marshal(failed["saved"])
	errs, _ := json.Marshal(failed["errors"])
	t.Logf("recording with a failing note: complete=%v saved=%s errors=%s", failed["complete"], saved, errs)
	if failed["complete"] != false {
		t.Fatalf("precondition: the recording is incomplete: %v", failed)
	}
	for _, raw := range failed["saved"].([]any) {
		if raw.(map[string]any)["kind"] == "collective_review" {
			t.Fatalf("precondition: the inspection was not recorded: %s", saved)
		}
	}
	revised := loopPage(t, b, s, map[string]any{"id": p.ID, "body": "The owner must remain active."})
	updatesFor := func(id string) []any {
		t.Helper()
		next := assignmentInput(id, "case-1", "worker", "work")
		v, err := workCall(t, b, "prepare", map[string]any{"query": "lesson:" + p.ID, "assignment": next})
		if err != nil {
			t.Fatal(err)
		}
		team, ok := v["teamwork"].(map[string]any)
		if !ok {
			t.Fatalf("the next execution gets its teamwork context: %v", v)
		}
		updates, _ := team["changes"].(map[string]any)["updates"].([]any)
		return updates
	}
	updates := updatesFor("worker-2")
	data, _ := json.Marshal(updates)
	t.Logf("next execution after the failed recording: updates=%s", data)
	if len(updates) != 0 {
		t.Errorf("an inspection that was not recorded is not a baseline for changes: %s", data)
	}
	// the inspection recorded on its own succeeds, and the change since the inspected revision is then reported
	ok, err := workCall(t, b, "record", map[string]any{"assignment": first, "receipt": v["receipt"], "reviewed": []string{"knowledge1"}})
	if err != nil || ok["complete"] != true {
		t.Fatalf("recording the inspection on its own: %v %v", ok, err)
	}
	updates = updatesFor("worker-3")
	data, _ = json.Marshal(updates)
	t.Logf("next execution after the recorded inspection: updates=%s", data)
	if len(updates) != 1 || updates[0].(map[string]any)["after"].(map[string]any)["revision"] != revised.Revision {
		t.Errorf("a recorded inspection is the baseline for the change to the revised article: %s", data)
	}
}
