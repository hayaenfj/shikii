package main

import "testing"

// The status count and the queue must apply one rule. A decision asked again leaves the queue (the new decision replaces it), so the
// status must not keep counting the original as unanswered or as waiting for the person.
func TestStatusDoesNotCountARedoneOriginalAsWaiting(t *testing.T) {
	url, _, _ := newServeFixture(t, "false", map[string]float64{"change_request": 0.6, "question": 0.4, "other": 0})
	code, d := postJSON(t, url+"/v1/decide", map[string]any{"spec": "intent", "inputs": map[string]string{"message": "fix the typo"}})
	if code != 200 {
		t.Fatalf("decide: %d %v", code, d)
	}
	counts := func() (queued int, unlabeled, waiting float64) {
		t.Helper()
		var q struct{ Items []map[string]any }
		if code := getJSON(t, url+"/v1/queue?spec=intent", &q); code != 200 {
			t.Fatalf("queue: %d", code)
		}
		var stat struct{ Specs []map[string]any }
		if code := getJSON(t, url+"/v1/status", &stat); code != 200 {
			t.Fatalf("status: %d", code)
		}
		return len(q.Items), stat.Specs[0]["unlabeled"].(float64), stat.Specs[0]["waiting_for_person"].(float64)
	}
	if queued, unlabeled, waiting := counts(); queued != 1 || unlabeled != 1 || waiting != 1 {
		t.Fatalf("before the redo one decision waits: queue=%d unlabeled=%v waiting=%v", queued, unlabeled, waiting)
	}
	code, res := postJSON(t, url+"/v1/redo", map[string]any{"spec": "intent", "trace_id": d["trace_id"]})
	if code != 200 || res["recorded"] != true {
		t.Fatalf("redo: %d %v", code, res)
	}
	if queued, unlabeled, waiting := counts(); queued != 1 || unlabeled != 1 || waiting != 1 {
		t.Errorf("after the redo only the new decision waits: queue=%d unlabeled=%v waiting=%v", queued, unlabeled, waiting)
	}
	newTrace := res["after"].(map[string]any)["trace_id"]
	if code, body := postJSON(t, url+"/v1/label", map[string]any{"spec": "intent", "trace_id": newTrace, "truth": "question"}); code != 200 {
		t.Fatalf("label: %d %v", code, body)
	}
	if queued, unlabeled, waiting := counts(); queued != 0 || unlabeled != 0 || waiting != 0 {
		t.Errorf("after the person answered, nothing waits: queue=%d unlabeled=%v waiting=%v", queued, unlabeled, waiting)
	}
}
