package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

// Audit N of 0.31.0: the one state of the session's turn (sessionLatest), and the tests that read it.

// INVARIANT 9: "the person's request rewrites the state (a request outside every project too)". A request in project B whose hook
// stops before hookUserPrompt with an error other than errProjectInactive (here: B's entry of the project index cannot be replaced,
// so registerProject fails) does not note itself. The state still names request 1 in A, which ended; a completion notice of request
// 2's background work reopens request 1, judges it again, and the person's next message in A takes over its references.
func TestAuditNRequestInAProjectWhoseHookFails(t *testing.T) {
	f := auditCFinishedFirstRequest(t)
	if reason, blocked := stopReason(t, runUnreviewedHook(t, "stop", map[string]any{"session_id": "planning", "cwd": f.repo, "last_assistant_message": "Done."})); blocked {
		t.Fatalf("precondition: the Stop of request 1 passes: %s", reason)
	}
	first := loadSession(f.root, "planning").TurnID
	other := gitRepo(t)
	initTestProject(t, other)
	common, err := agentHome()
	if err != nil {
		t.Fatal(err)
	}
	entries, _ := filepath.Glob(filepath.Join(common, "projects", "*.json"))
	cut := 0
	for _, path := range entries {
		if p, ok := readEntry(path); ok && p.Path == feedbackScope(other) {
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(filepath.Join(path, "keep"), 0o700); err != nil { // the entry cannot be written over
				t.Fatal(err)
			}
			cut++
		}
	}
	if cut != 1 {
		t.Fatalf("precondition: B's entry is replaced by a folder (%d)", cut)
	}
	runUnreviewedHookErr(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": other, "prompt": "Now update the README of this other project and run its long check in the background."})
	latest, known, _ := readSessionLatest("planning")
	logged, _ := os.ReadFile(filepath.Join(common, "hook.log"))
	lines := strings.Split(strings.TrimSpace(string(logged)), "\n")
	t.Logf("hook.log: %.300s", lines[len(lines)-1])
	t.Logf("after request 2 in B (its hook failed): state=%+v known=%v (request 1 is %s)", latest, known, first)
	runUnreviewedHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": f.repo, "prompt": auditINotice})
	a := loadSession(f.root, "planning")
	latest, _, _ = readSessionLatest("planning")
	t.Logf("after the notice in A: state=%+v judged=%v", latest, a.Judged)
	if latest.Turn == first && !latest.Ended {
		t.Errorf("INVARIANT 9: request 2 did not rewrite the state, and the notice reopened request 1, which ended before request 2 came")
	}
	next := f.prepare(f.ask("Now rename the export button, a new task."), "export button")
	if carried := next["carried"]; carried != nil {
		t.Errorf("INVARIANT 9: the next request in A takes over the references of request 1, which had ended: %.200s", auditJSON(carried))
	}
}

// runUnreviewedHookErr is runUnreviewedHook for a hook that returns an error (the error is the hook's, logged here).
func runUnreviewedHookErr(t *testing.T, event string, in map[string]any) {
	t.Helper()
	b := auditJSON(in)
	old := stdin
	stdin = strings.NewReader(b)
	defer func() { stdin = old }()
	var err error
	_ = captureNoFail(func() error { err = cmdHook([]string{event}); return nil })
	t.Logf("hook %s: %v", event, err)
}

// captureNoFail runs fn with stdout captured, and returns the output.
func captureNoFail(fn func() error) string {
	old := stdout
	var buf strings.Builder
	stdout = &buf
	defer func() { stdout = old }()
	_ = fn()
	return buf.String()
}

// endedTurnForTest calls a request that no Stop ended "ended" as soon as another request was noted: it answers "is this request the
// session's last one and still open", not "did its turn end". The checks of the form `a.TurnID == first && a.endedTurnForTest() !=
// a.TurnID` after a later request (audit_0_31_k_turn_test.go, audit_0_31_j_turn_test.go) can never fail, and the preconditions
// `if !a.goesOnForTest() { first = a.TurnID }` are always met.
func TestAuditNEndedTurnForTestCallsAReplacedRequestEnded(t *testing.T) {
	freshMachine(t)
	noteSessionRequest("s1", "/p/a/.shikii", "turn-1") // request 1: no Stop comes
	noteSessionRequest("s1", "/p/b/.shikii", "turn-2") // request 2
	if got := (&hookSession{TurnID: "turn-1"}).endedTurnForTest(); got != "" {
		t.Errorf("endedTurnForTest says request 1 ended (%q), and no Stop ended it", got)
	}
}

// A guard for the line of runHookEvent that reads the Stop's turn when the Stop comes (stopTurn): a request noted while the Stop is
// being judged is left open. The shipped tests call sessionTurnEvent with the turn they read (turn_state_test.go) and never run a
// Stop across a request: reading the turn at the end of runHookEvent instead passes all of them (audit N, mutation N12). GREEN on
// the candidate; RED with that mutation.
func TestAuditNARequestNotedWhileTheStopIsJudgedStaysOpen(t *testing.T) {
	f := auditCFinishedFirstRequest(t)
	upstream := hookLocalURL
	var noted atomic.Bool
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && noted.CompareAndSwap(false, true) { // a decision, not the health probe before the Stop is read
			noteSessionRequest("planning", f.root, "the-next-request") // the person's next message, while the Stop waits for the model
		}
		req, _ := http.NewRequest(r.Method, upstream+r.URL.Path, r.Body)
		req.Header = r.Header
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()
		w.WriteHeader(resp.StatusCode)
		_, _ = io.Copy(w, resp.Body)
	}))
	defer model.Close()
	hookLocalURL = model.URL
	t.Cleanup(func() { hookLocalURL = upstream })
	runUnreviewedHook(t, "stop", map[string]any{"session_id": "planning", "cwd": f.repo, "last_assistant_message": "Done."})
	if !noted.Load() {
		t.Fatalf("precondition: the Stop asked the model")
	}
	latest, _, _ := readSessionLatest("planning")
	t.Logf("after the Stop: %+v", latest)
	if latest.Turn != "the-next-request" || latest.Ended {
		t.Errorf("INVARIANT 9: the Stop of request 1 ended the request noted while it was judged: %+v", latest)
	}
}

// A guard for pageOfReports's workReportSource: a page whose only source is the report of another work (an earlier request's
// result) is no observation either. The shipped tests write the page from the same receipt's result only: without workReportSource
// in pageOfReports they pass (audit N, mutation N26). GREEN on the candidate; RED with that mutation.
func TestAuditNAPageOfAnotherWorksResult(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	b, _ := loopBook(t)
	earlier, err := workCall(t, b, "prepare", map[string]any{"query": "invoice rounding", "request": "Look at invoice rounding"})
	if err != nil {
		t.Fatal(err)
	}
	id := auditKResultID(t, earlier["receipt"], func(fields map[string]any) (map[string]any, error) { return workCall(t, b, "record", fields) })
	ingested, err := improvementCall(t, b, map[string]any{"operation": "ingest", "sources": []string{id}, "title": "Invoices round per line", "lesson": "Invoices round per line.", "applies_to": "invoice rounding", "kind": "rule", "scene": "when computing invoices"})
	if err != nil {
		t.Fatal(err)
	}
	pageID, _ := loopObject(t, loopObject(t, ingested)["page"])["id"].(string)
	later, err := workCall(t, b, "prepare", map[string]any{"query": "lesson:" + pageID, "request": "Check invoice rounding again"})
	if err != nil {
		t.Fatal(err)
	}
	ref := ""
	for _, k := range asList(later["knowledge"]) {
		if m, _ := k.(map[string]any); m["id"] == pageID {
			ref, _ = m["ref"].(string)
		}
	}
	if ref == "" {
		t.Fatalf("precondition: the page is read: %.300s", auditJSON(later["knowledge"]))
	}
	got, err := workCall(t, b, "record", map[string]any{"receipt": later["receipt"], "result": map[string]any{"changed": "Invoices round per line.", "sufficient": true, "evidence": []string{ref}}})
	t.Logf("result citing a page of another work's result: err=%v complete=%v errors=%.300s", err, got["complete"], auditJSON(got["errors"]))
	if err == nil && got["complete"] == true {
		t.Errorf("INVARIANT 10: a result with nothing observed is complete on a page whose only source is another work's result")
	}
}

// A guard for checkWorkSource's "source = memo.Source" (a capture that failed once is finished as the kind its memo kept). The
// shipped TestAuditCReportWhoseCaptureWasFinishedLater asserts only that the result citing the first report is not complete: without
// the line, the interrupted report can never be finished (refused under its own title), the reworded result B is not complete
// either, and the test still passes, on an unrelated refusal ("unknown reasoning evidence observed1"). Audit N, mutation N31. GREEN on
// the candidate; RED with that mutation.
func TestAuditNAnInterruptedReportIsFinishedAsAReport(t *testing.T) {
	f := newAuditFlow(t)
	first := f.prepare(f.ask(planningTestRequest), "CSV export filters")
	old := workAfterWrite
	t.Cleanup(func() { workAfterWrite = old })
	failed := false
	workAfterWrite = func(kind string) error {
		if kind == "observation" && !failed {
			failed = true
			return errSyntheticInterrupt
		}
		return nil
	}
	if _, err := f.record(first, map[string]any{"result": map[string]any{"changed": "Renamed the button."}}); err != nil || !failed {
		t.Fatalf("precondition: the first report's save fails once: %v", err)
	}
	workAfterWrite = old
	b, err := f.record(first, map[string]any{"result": map[string]any{"changed": "Renamed the export button.", "checked": "Looked at it."}})
	if err != nil || b["complete"] != true {
		t.Errorf("the reworded result is not complete once the interrupted report is finished: %v %.300s", err, auditJSON(b["errors"]))
	}
	c, _ := f.record(first, map[string]any{"result": map[string]any{"changed": "Renamed the export button, checked.", "checked": "It works.", "sufficient": true, "evidence": []any{"observed1"}}})
	if raw := auditJSON(c["errors"]); !strings.Contains(raw, "a report this work wrote about itself") {
		t.Errorf("INVARIANT 10: the result citing the interrupted report is not refused as citing a report: %.300s", raw)
	}
}

var errSyntheticInterrupt = &syntheticError{"synthetic: the write was interrupted"}

type syntheticError struct{ s string }

func (e *syntheticError) Error() string { return e.s }

// INVARIANT 9: "a Stop ends the request only when the turn at the time the Stop came is still the last request". runHookEvent reads
// that turn (stopTurn) after prepareHookProject and ensureModel: a request noted while the Stop probes the model's health (the Stop
// had come before it) is the turn the Stop reads, and the Stop ends it.
func TestAuditNARequestNotedWhileTheStopProbesTheModel(t *testing.T) {
	f := auditCFinishedFirstRequest(t)
	upstream := hookLocalURL
	var noted atomic.Bool
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && noted.CompareAndSwap(false, true) {
			noteSessionRequest("planning", f.root, "the-next-request") // the person's next message, while the Stop asks /health
		}
		req, _ := http.NewRequest(r.Method, upstream+r.URL.Path, r.Body)
		req.Header = r.Header
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()
		w.WriteHeader(resp.StatusCode)
		_, _ = io.Copy(w, resp.Body)
	}))
	defer model.Close()
	hookLocalURL = model.URL
	t.Cleanup(func() { hookLocalURL = upstream })
	runUnreviewedHook(t, "stop", map[string]any{"session_id": "planning", "cwd": f.repo, "last_assistant_message": "Done."})
	if !noted.Load() {
		t.Fatalf("precondition: the Stop probed the model")
	}
	latest, _, _ := readSessionLatest("planning")
	t.Logf("after the Stop: %+v", latest)
	if latest.Turn != "the-next-request" || latest.Ended {
		t.Errorf("INVARIANT 9: the Stop of request 1 ended the request noted after the Stop came (while it probed the model): %+v", latest)
	}
}
