package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hayaenfj/shikii/internal/notebook"
	"github.com/hayaenfj/shikii/internal/store"
)

func recoveryOutput(t *testing.T, out string) map[string]any {
	t.Helper()
	if out == "" {
		return map[string]any{}
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("invalid hook output: %s", out)
	}
	return got
}

func requireRecoveryHalt(t *testing.T, out string) {
	t.Helper()
	got := recoveryOutput(t, out)
	if got["continue"] != false || got["stopReason"] == nil || got["decision"] == "block" {
		t.Fatalf("unchanged pending work must stop explicitly, without another automatic continuation: %s", out)
	}
}

func TestRitualRecoveryWaitsForSavedPlan(t *testing.T) {
	repo, root, _ := planningFixture(t)
	t.Chdir(repo)
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "recovery", "cwd": repo, "prompt": planningTestRequest})
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stop := map[string]any{"session_id": "recovery", "cwd": repo, "last_assistant_message": "Done."}
	first := runHook(t, "stop", stop)
	if recoveryOutput(t, first)["decision"] != "block" {
		t.Fatalf("first missing submit asks for recovery: %s", first)
	}
	if s := loadSession(root, "recovery"); s.Judged {
		t.Fatal("a missing required submit must not be judged as a finished turn")
	}
	stop["stop_hook_active"] = true
	requireRecoveryHalt(t, runHook(t, "stop", stop))
	if s := loadSession(root, "recovery"); s.Judged {
		t.Fatal("an ignored recovery must not create a completion judgment")
	}
	s := loadSession(root, "recovery")
	if _, err := planningCall(t, "submit", map[string]any{
		"session_id": "recovery", "turn_id": s.TurnID,
		"agreed": map[string]bool{"unknown": true}, "open": map[string]bool{"unknown": true},
	}); err != nil {
		t.Fatal(err)
	}
	got := recoveryOutput(t, runHook(t, "stop", stop))
	if got["continue"] == false || got["decision"] == "block" || !loadSession(root, "recovery").Judged {
		t.Fatalf("saving the required submit permits completion: %v", got)
	}
	st := countRitualStats(readGuardRecords(root))
	if st.Plan.Asked != 1 || st.Plan.Fixed != 1 {
		t.Fatalf("each turn is counted once, and saving is counted as recovery: %+v", st.Plan)
	}
}

func TestRitualRecoveryKeepsShortMessageOpen(t *testing.T) {
	repo, root, _ := planningFixture(t)
	t.Chdir(repo)
	continuesTurn(t, repo, root, "OK\u3067\u3059")
	stop := map[string]any{"session_id": "planning", "cwd": repo, "last_assistant_message": "Done."}
	first := runHook(t, "stop", stop)
	reason, block := stopReason(t, first)
	if !block {
		t.Fatalf("first Stop asks for grouping: %s", first)
	}
	// A notification must not turn the next Stop into a fresh recovery allowance.
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": "<task-notification>done</task-notification>"})
	requireRecoveryHalt(t, runHook(t, "stop", stop))
	if loadSession(root, "planning").Judged {
		t.Fatal("grouping remains open while its required answer is missing")
	}
	payload := askedJSON(t, reason)
	payload["continues_previous"] = true
	if _, err := planningCall(t, "submit", payload); err != nil {
		t.Fatal(err)
	}
	stop["stop_hook_active"] = true
	got := recoveryOutput(t, runHook(t, "stop", stop))
	s := loadSession(root, "planning")
	if got["continue"] == false || got["decision"] == "block" || !s.Judged || s.Completed.Request != continuesFirst+" / OK\u3067\u3059" {
		t.Fatalf("the saved grouping governs the final judgment: %v, %+v", got, s.Completed)
	}
}

func TestRitualRecoveryContinuesOnlyWithSavedProgress(t *testing.T) {
	repo, root, _ := planningFixture(t)
	t.Chdir(repo)
	scope := feedbackScope(repo)
	src := "0000000000000914"
	st := store.Store{Dir: root}
	if err := st.AppendFeedback(scope, store.Feedback{ID: src, Request: "r", Summary: "s", Reply: "x"}); err != nil {
		t.Fatal(err)
	}
	book := notebook.Book{Root: root, Scope: scope}
	var ids []string
	for i := range 3 {
		r, err := book.Ingest(notebook.Draft{Sources: []string{src}, Title: fmt.Sprintf("Legacy %d", i), Lesson: fmt.Sprintf("Keep data %d", i), AppliesTo: "testing", Kind: "rule", Scene: "testing"})
		if err != nil {
			t.Fatal(err)
		}
		if err := book.RewriteWithoutKindScene(r.Page.ID); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, r.Page.ID)
	}
	h := newServer(serveOpts{storeDir: root, projectScope: scope})
	reflectCall(t, h, map[string]any{"operation": "submit", "wiki": []map[string]any{{"sources": []string{src}}}})
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "kinds", "cwd": repo, "prompt": "Explain how existing project knowledge is used before a task."})
	stop := map[string]any{"session_id": "kinds", "cwd": repo, "last_assistant_message": "Done."}
	if out := runHook(t, "stop", stop); recoveryOutput(t, out)["decision"] != "block" {
		t.Fatalf("unclassified pages ask for recovery: %s", out)
	}
	if _, err := book.Classify(ids[0], "rule", "testing"); err != nil {
		t.Fatal(err)
	}
	stop["stop_hook_active"] = true
	out := runHook(t, "stop", stop)
	if recoveryOutput(t, out)["decision"] != "block" || strings.Contains(out, ids[0]) {
		t.Fatalf("saved progress continues with only the remaining pages: %s", out)
	}
	requireRecoveryHalt(t, runHook(t, "stop", stop))
	for _, id := range ids[1:] {
		if _, err := book.Classify(id, "rule", "testing"); err != nil {
			t.Fatal(err)
		}
	}
	got := recoveryOutput(t, runHook(t, "stop", stop))
	if got["continue"] == false || got["decision"] == "block" || !loadSession(root, "kinds").Judged {
		t.Fatalf("completion is allowed only after the saved pages are complete: %v", got)
	}
}

func TestRitualRecoveryVerificationFailureCannotBeJudgedLater(t *testing.T) {
	repo, root, _ := planningFixture(t)
	t.Chdir(repo)
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "broken", "cwd": repo, "prompt": "Please update the documentation with the complete installation steps."})
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// A directory where the receipt log belongs makes the write fail deterministically.
	p := guardRecordsPath(root)
	if err := os.MkdirAll(p, 0o700); err != nil {
		t.Fatal(err)
	}
	requireRecoveryHalt(t, runHook(t, "stop", map[string]any{"session_id": "broken", "cwd": repo, "last_assistant_message": "Done."}))
	if err := os.Remove(p); err != nil {
		t.Fatal(err)
	}
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "broken", "cwd": repo, "prompt": "Now explain how documentation is organized and where contributors should begin."})
	recs, err := (store.Store{Dir: root}).Records("done-as-asked", 0)
	if err != nil || len(recs) != 0 {
		t.Fatalf("a failed verification must not become a completion judgment at the next request: %d, %v", len(recs), err)
	}
}

func TestRitualRecoveryStateWriteFailureHalts(t *testing.T) {
	repo, root, _ := planningFixture(t)
	t.Chdir(repo)
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "unwritable", "cwd": repo, "prompt": "Please update the documentation with the complete installation steps."})
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	p := filepath.Dir(sessionPath(root, "unwritable"))
	if err := os.Chmod(p, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(p, 0o700) })
	requireRecoveryHalt(t, runHook(t, "stop", map[string]any{"session_id": "unwritable", "cwd": repo, "last_assistant_message": "Done."}))
}

func TestRitualRecoveryPartialReflectionUsesSavedResults(t *testing.T) {
	repo, root, _ := planningFixture(t)
	t.Chdir(repo)
	scope := feedbackScope(repo)
	ids := []string{"0000000000000915", "0000000000000916"}
	for _, id := range ids {
		if err := (store.Store{Dir: root}).AppendFeedback(scope, store.Feedback{ID: id, Request: "r", Summary: "s", Reply: "x"}); err != nil {
			t.Fatal(err)
		}
	}
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "reflection", "cwd": repo, "prompt": "Explain how existing project knowledge is used before a task."})
	stop := map[string]any{"session_id": "reflection", "cwd": repo, "last_assistant_message": "Done."}
	if out := runHook(t, "stop", stop); recoveryOutput(t, out)["decision"] != "block" {
		t.Fatalf("pending reflection requests recovery: %s", out)
	}
	h := newServer(serveOpts{storeDir: root, projectScope: scope})
	reflectCall(t, h, map[string]any{"operation": "submit", "wiki": []map[string]any{{"sources": ids[:1]}}})
	stop["stop_hook_active"] = true
	out := runHook(t, "stop", stop)
	if recoveryOutput(t, out)["decision"] != "block" || strings.Contains(out, ids[0]) || !strings.Contains(out, ids[1]) {
		t.Fatalf("partial saved reflection returns only the remaining conversation: %s", out)
	}
	requireRecoveryHalt(t, runHook(t, "stop", stop))
	reflectCall(t, h, map[string]any{"operation": "submit", "wiki": []map[string]any{{"sources": ids[1:]}}})
	out = runHook(t, "stop", stop)
	if got := recoveryOutput(t, out); got["continue"] == false || got["decision"] == "block" || !loadSession(root, "reflection").Judged {
		t.Fatalf("saved reflection permits completion: %s", out)
	}
}

func TestRitualRecoveryOlderStopCannotReplenishProgress(t *testing.T) {
	repo, root, _ := planningFixture(t)
	t.Chdir(repo)
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "overlap", "cwd": repo, "prompt": planningTestRequest})
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	snapshot := loadSession(root, "overlap")
	out, muts, err := hookStop(root, &hookInput{SessionID: "overlap", CWD: repo, LastAssistantMessage: "Done."}, snapshot)
	if err != nil || !out.Block {
		t.Fatalf("premise: the older Stop requested recovery: %v, %+v", err, out)
	}
	if err := updateSession(root, "overlap", func(s *hookSession) {
		// A later check has already closed the continuation budget. Apply the older Stop afterwards.
		s.RitualRecovery = &ritualRecovery{TurnID: s.TurnID, Left: 0, Pending: true}
		for _, m := range muts {
			m(s)
		}
	}); err != nil {
		t.Fatal(err)
	}
	if r := loadSession(root, "overlap").RitualRecovery; r == nil || r.Left != 0 {
		t.Fatalf("an older Stop must not increase the saved continuation budget: %+v", r)
	}
}
