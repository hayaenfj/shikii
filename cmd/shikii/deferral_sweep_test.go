package main

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/store"
)

// 0.9.3: a deferred outcome whose session was closed or killed mid-turn (no organizing, no Stop, no next request) is settled
// by any later hook of the project or a reflect query once it is older than deferralStale, with the provisional grouping, once.

// a long message on other work: settles the previous outcome as "yes" when it stands alone
const sweepReply = "Draft a migration guide for v0.8 users explaining how backups work now."

// doneLabelCount is how many label lines the trace has (LabeledTraces keeps only the newest).
func doneLabelCount(t *testing.T, root, trace string) int {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, "labels", "done-as-asked.jsonl"))
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return strings.Count(string(b), `"`+trace+`"`)
}

// ageDeferral moves the waiting turn's start back past the limit (as if the session was left alone that long).
func ageDeferral(t *testing.T, root string) {
	t.Helper()
	if err := updateSession(root, "planning", func(s *hookSession) { s.Planning.At = time.Now().Add(-deferralStale - time.Minute) }); err != nil {
		t.Fatal(err)
	}
}

func deferredTurn(t *testing.T, reply string) (repo, root, done string) {
	t.Helper()
	repo, root, _ = planningFixture(t)
	done = continuesTurn(t, repo, root, reply)
	if !openDeferral(loadSession(root, "planning")) {
		t.Fatalf("the outcome must wait for the organizing or the Stop: %+v", loadSession(root, "planning").Planning)
	}
	if _, err := os.Stat(deferralMarkerPath(root, "planning")); err != nil {
		t.Fatalf("an open deferral must have its marker: %v", err)
	}
	if l, ok := doneLabel(t, root, done); ok {
		t.Fatalf("not settled yet: %+v", l)
	}
	return repo, root, done
}

func otherPrompt(t *testing.T, repo string) {
	t.Helper()
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "other", "cwd": repo, "prompt": "Explain how the page lists waiting answers."})
}

// another session's prompt settles a stale deferral with the provisional grouping, and the original session coming back adds nothing
func TestSweepStaleDeferralByOtherSession(t *testing.T) {
	repo, root, done := deferredTurn(t, sweepReply)
	ageDeferral(t, root)
	otherPrompt(t, repo)
	if l, ok := doneLabel(t, root, done); !ok || l.Truth != "yes" || l.Source != "outcome" {
		t.Fatalf("another session's prompt must settle a stale deferral: %+v %v", l, ok)
	}
	s := loadSession(root, "planning")
	if g := groupingOf(s); g == nil || !g.Settled || g.Joined || s.Request != sweepReply {
		t.Fatalf("settled with the provisional grouping (standing alone): %+v request=%q", g, s.Request)
	}
	if _, err := os.Stat(deferralMarkerPath(root, "planning")); !os.IsNotExist(err) {
		t.Fatalf("the marker must go once settled: %v", err)
	}
	// the original session comes back: its organizing, Stop and next request find it settled
	continuesSubmit(t, root, true)
	runHook(t, "stop", map[string]any{"session_id": "planning", "cwd": repo, "last_assistant_message": "Wrote them."})
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": "Now add the upgrade notes for people on 0.8."})
	if n := doneLabelCount(t, root, done); n != 1 {
		t.Fatalf("the outcome must be attached once, got %d", n)
	}
	if s := loadSession(root, "planning"); s.Completed != nil && s.Completed.Request == continuesFirst+" / "+sweepReply {
		t.Fatalf("a late organizing must not regroup a settled turn: %+v", s.Completed)
	}
}

// a reflect query (the CLI, MCP shikii_reflect and /v1/reflect all go through runReflect) settles a stale deferral too
func TestSweepStaleDeferralByReflectQuery(t *testing.T) {
	_, root, done := deferredTurn(t, sweepReply)
	ageDeferral(t, root)
	if _, err := runReflect(root, loadSession(root, "planning").Scope, reflectRequest{Operation: "query"}); err != nil {
		t.Fatal(err)
	}
	if l, ok := doneLabel(t, root, done); !ok || l.Truth != "yes" {
		t.Fatalf("a reflect query must settle a stale deferral: %+v %v", l, ok)
	}
}

// a deferral within the time is left for its own session (not settled, marker kept)
func TestSweepLeavesFreshDeferral(t *testing.T) {
	repo, root, done := deferredTurn(t, sweepReply)
	otherPrompt(t, repo)
	runHook(t, "stop", map[string]any{"session_id": "other", "cwd": repo, "last_assistant_message": "Explained."})
	if _, err := runReflect(root, loadSession(root, "planning").Scope, reflectRequest{Operation: "query"}); err != nil {
		t.Fatal(err)
	}
	if l, ok := doneLabel(t, root, done); ok {
		t.Fatalf("a deferral still waiting within the time must not be settled: %+v", l)
	}
	if !openDeferral(loadSession(root, "planning")) {
		t.Fatal("the deferral must stay open for its own session")
	}
}

// a record that moved on (its turn was judged) no longer waits, whatever the time
func TestSweepMovedOnDeferral(t *testing.T) {
	repo, root, done := deferredTurn(t, sweepReply)
	if err := updateSession(root, "planning", func(s *hookSession) { s.Judged = true }); err != nil {
		t.Fatal(err)
	}
	otherPrompt(t, repo)
	if l, ok := doneLabel(t, root, done); !ok || l.Truth != "yes" {
		t.Fatalf("a deferral whose session moved on must be settled: %+v %v", l, ok)
	}
}

// the sweep uses the same fixed rules: a short reply or an acknowledgement left waiting gets no "yes"
func TestSweepKeepsOutcomeRules(t *testing.T) {
	for _, reply := range []string{"no", "merge main"} {
		t.Run(reply, func(t *testing.T) {
			repo, root, done := deferredTurn(t, reply)
			ageDeferral(t, root)
			otherPrompt(t, repo)
			if l, ok := doneLabel(t, root, done); ok {
				t.Fatalf("a short reply must not be labeled by the sweep: %+v", l)
			}
			if openDeferral(loadSession(root, "planning")) {
				t.Fatal("the grouping is still made final")
			}
		})
	}
}

// sweeps running at the same time settle a stale deferral once
func TestSweepConcurrentOnce(t *testing.T) {
	_, root, done := deferredTurn(t, sweepReply)
	ageDeferral(t, root)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			sweepDeferrals(root, "")
		}()
	}
	close(start)
	wg.Wait()
	if n := doneLabelCount(t, root, done); n != 1 {
		t.Fatalf("concurrent sweeps must attach the outcome once, got %d", n)
	}
}

// a deferral its own session settles between the sweep's first read and its lock is not settled again (the record is read again inside the lock)
func TestSweepRereadsInsideLock(t *testing.T) {
	_, root, done := deferredTurn(t, sweepReply)
	ageDeferral(t, root)
	old := sweepPeeked
	t.Cleanup(func() { sweepPeeked = old })
	sweepPeeked = func(id string) {
		if err := updateSession(root, id, func(s *hookSession) { settleGrouping(root, s) }); err != nil {
			t.Error(err)
		}
	}
	sweepDeferrals(root, "")
	if n := doneLabelCount(t, root, done); n != 1 {
		t.Fatalf("the outcome must be attached once, got %d", n)
	}
}

// a sweep does not take a session another hook is holding (it is left for the next sweep)
func TestSweepSkipsLockedSession(t *testing.T) {
	_, root, done := deferredTurn(t, sweepReply)
	ageDeferral(t, root)
	unlock := lockSession(root, "planning")
	sweepDeferrals(root, "")
	unlock()
	if l, ok := doneLabel(t, root, done); ok {
		t.Fatalf("a session whose lock is held must be left alone: %+v", l)
	}
	sweepDeferrals(root, "")
	if n := doneLabelCount(t, root, done); n != 1 {
		t.Fatalf("the next sweep settles it once, got %d", n)
	}
}

// settling a grouping makes it final: the Stop settles it and the next request does not settle it again (test hole: mutation M2)
func TestSettledGroupingIsNotSettledTwice(t *testing.T) {
	repo, root, done := deferredTurn(t, sweepReply)
	runHook(t, "stop", map[string]any{"session_id": "planning", "cwd": repo, "last_assistant_message": "Wrote them."})
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": "Now add the upgrade notes for people on 0.8."})
	sweepDeferrals(root, "")
	if n := doneLabelCount(t, root, done); n != 1 {
		t.Fatalf("the previous outcome must be attached once, got %d", n)
	}
}

// a late Stop of an earlier turn does not settle the grouping of the turn that came after it (test hole: mutation M17)
func TestStaleStopLeavesNewTurnGrouping(t *testing.T) {
	repo, root, _ := deferredTurn(t, sweepReply)
	runHook(t, "stop", map[string]any{"session_id": "planning", "cwd": repo, "last_assistant_message": "Wrote them."})
	done2 := ""
	for _, r := range loadSession(root, "planning").Reviews {
		if r.Spec == "done-as-asked" {
			done2 = r.Trace
		}
	}
	if done2 == "" {
		t.Fatal("the second request must have a done-as-asked decision")
	}
	snapshot := loadSession(root, "planning") // a second Stop of this turn (after a notification) reads its snapshot
	_, muts, err := hookStop(root, &hookInput{SessionID: "planning", CWD: repo, LastAssistantMessage: "Also checked the dates."}, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": "Explain how the hook decides which wiki lessons to deliver with each request."})
	if !openDeferral(loadSession(root, "planning")) {
		t.Fatal("the new turn's outcome waits for its organizing")
	}
	if err := updateSession(root, "planning", func(s *hookSession) {
		for _, m := range muts {
			m(s)
		}
	}); err != nil {
		t.Fatal(err)
	}
	if !openDeferral(loadSession(root, "planning")) {
		t.Fatal("a late Stop of the earlier turn must not settle the new turn's grouping")
	}
	if l, ok := doneLabel(t, root, done2); ok {
		t.Fatalf("a late Stop must not settle the new turn's outcome: %+v", l)
	}
}

// 0.9.3: the outcome truth goes through AppendLabelsOnce, so a hook that proceeds without the session lock (after waiting 2 seconds)
// or retries after a failed save cannot add the same outcome twice.
func TestOutcomeTruthIsWrittenOnceThroughLabelTrace(t *testing.T) {
	model := fakeAgentModel(t, func(string) float64 { return .1 })
	repo := gitRepo(t)
	agent := hookEnv(t, model, repo)
	t.Chdir(repo)
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "once", "cwd": repo, "prompt": "update the README install steps for macOS and Linux users"})
	runHook(t, "stop", map[string]any{"session_id": "once", "cwd": repo, "last_assistant_message": "done"})
	st := store.Store{Dir: agent}
	recs, _ := st.Records("done-as-asked", 0)
	if len(recs) != 1 {
		t.Fatalf("one done-as-asked record, got %d", len(recs))
	}
	for i := 0; i < 3; i++ {
		if err := labelTrace(st, "done-as-asked", recs[0].TraceID, "yes", outcomeKeptWeight, "outcome"); err != nil {
			t.Fatal(err)
		}
	}
	n := 0
	all, _ := st.Labels("done-as-asked", recs[0].SpecHash, recs[0].Model)
	for _, l := range all {
		if l.TraceID == recs[0].TraceID && l.Source == "outcome" {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("the same outcome truth lands once, got %d", n)
	}
}
