package main

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

// Audit M of 0.31.0: the one state of the session's turn (sessionLatest).

// INVARIANT 9: "the person's request rewrites the state (also a request outside every project)". The state is written in two
// places only: hookUserPrompt (after prepareHookProject succeeded) and the errProjectInactive branch of runHookEvent. A request in a
// project whose hook stops on any other error of prepareHookProject (here: a folder of its .shikii that is a link, which
// checkProjectStore refuses) leaves the state on request 1 in A, ended. The completion notice of request 2's background work, in
// A, reopens request 1, and the next request in A takes over its references.
func TestAuditMNoticeAfterARequestWhoseHookFailed(t *testing.T) {
	f, first := auditKEndedInA(t)
	other := gitRepo(t)
	otherRoot, _ := initTestProject(t, other)
	handoff := filepath.Join(otherRoot, "handoff")
	if err := os.RemoveAll(handoff); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(t.TempDir(), handoff); err != nil {
		t.Fatal(err)
	}
	runUnreviewedHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": other, "prompt": "Now update the README of this other project and run its long check in the background."})
	latest, known, _ := readSessionLatest("planning")
	t.Logf("after request 2 in B (its hook failed): state=%+v known=%v (request 1 is %s)", latest, known, first)
	runUnreviewedHook(t, "stop", map[string]any{"session_id": "planning", "cwd": other, "last_assistant_message": "The check runs in the background."})
	auditKNoticeInA(t, f, first, "request 2 in a project whose hook failed")
}

// INVARIANT 9: "a Stop a check did not stop ends the turn ... wherever the shell stands". runHookEvent returns on any error of
// prepareHookProject other than errProjectInactive before sessionTurnEvent: a Stop that comes while the shell stands in a project
// whose hook fails (a folder of its .shikii that is a link) says nothing (the agent's turn ends), and the state is left open. The
// person's next request in A takes over the references of request 1 as if it had been interrupted.
func TestAuditMStopInAProjectWhoseHookFailed(t *testing.T) {
	f := auditCFinishedFirstRequest(t)
	first := loadSession(f.root, "planning").TurnID
	other := gitRepo(t)
	otherRoot, _ := initTestProject(t, other)
	guards := filepath.Join(otherRoot, "guards")
	if err := os.RemoveAll(guards); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(t.TempDir(), guards); err != nil {
		t.Fatal(err)
	}
	out := runUnreviewedHook(t, "stop", map[string]any{"session_id": "planning", "cwd": other, "last_assistant_message": "Done."})
	latest, known, _ := readSessionLatest("planning")
	t.Logf("the Stop in B (its hook failed): out=%q state=%+v known=%v (request 1 is %s)", out, latest, known, first)
	if out != "" {
		t.Fatalf("precondition: the Stop says nothing, so the agent's turn ends")
	}
	if known && latest.Turn == first && !latest.Ended {
		t.Errorf("INVARIANT 9: the Stop that let the turn end (shell in a project whose hook failed) did not end request 1")
	}
	next := f.prepare(f.ask("Now rename the export button, a new task."), "export button")
	if carried := next["carried"]; carried != nil {
		t.Errorf("INVARIANT 9: the next request in A takes over the references of request 1, whose turn had ended: %.200s", auditJSON(carried))
	}
}

// The removal of old states in updateSessionLatest takes every regular *.json older than hookSessionTTL in the folder of the
// states, through a link: when ~/.shikii/agent/latest is a link to a folder of the person's (the states kept with other notes, a
// synced folder), the person's old JSON files there are removed at the next request. Other removals of the hook open the folder
// with os.Root (pruneSessionRecord) or take only the names they write (pruneStoppedLines: projectID(scope)+".stopped").
func TestAuditMTheRemovalOfOldStatesFollowsALink(t *testing.T) {
	home := freshMachine(t)
	common, err := agentHome()
	if err != nil {
		t.Fatal(err)
	}
	person := filepath.Join(home, "notes")
	if err := os.MkdirAll(person, 0o755); err != nil {
		t.Fatal(err)
	}
	mine := filepath.Join(person, "budget.json")
	if err := os.WriteFile(mine, []byte(`{"kept":"by the person"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-8 * 24 * time.Hour)
	if err := os.Chtimes(mine, old, old); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(common, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(person, filepath.Join(common, "latest")); err != nil {
		t.Fatal(err)
	}
	noteSessionRequest("planning", "", "")
	_, statErr := os.Stat(mine)
	t.Logf("after a request: the person's file %s: %v", mine, statErr)
	if os.IsNotExist(statErr) {
		t.Errorf("a file of the person's, in the folder ~/.shikii/agent/latest links to, was removed by the removal of old session states")
	}
}

// The same removal, with the folder a folder: it removes any old *.json there, not only the names it writes.
func TestAuditMTheRemovalOfOldStatesTakesAnyName(t *testing.T) {
	freshMachine(t)
	common, err := agentHome()
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(common, "latest")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(dir, "not a session state.json")
	if err := os.WriteFile(other, []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-8 * 24 * time.Hour)
	_ = os.Chtimes(other, old, old)
	noteSessionRequest("planning", "", "")
	_, statErr := os.Stat(other)
	t.Logf("after a request: %s: %v", other, statErr)
	if os.IsNotExist(statErr) {
		t.Errorf("removed: a name the hook never writes (sessionPath maps a session id to [A-Za-z0-9_-]+.json): the removal is no allowlist")
	}
}

// INVARIANT 9: an unreadable state is no state ("a session without one: nothing is handed over"). readSessionLatest returns the
// value json.Unmarshal filled before it failed, and hookUserPrompt reads it as previous with the error dropped
// (previous, _, _ := readSessionLatest): a state whose "ended" is not a boolean (written by hand, or by another version) keeps root
// and turn, Ended is left false, and the next request in A takes over the references of request 1, which had ended.
func TestAuditMAStateThatCannotBeReadIsHandedOver(t *testing.T) {
	f, first := auditKEndedInA(t)
	common, err := agentHome()
	if err != nil {
		t.Fatal(err)
	}
	path := sessionLatestPath(common, "planning")
	latest, known, err := readSessionLatest("planning")
	if err != nil || !known || !latest.Ended || latest.Turn != first {
		t.Fatalf("precondition: request 1 ended: %+v %v %v", latest, known, err)
	}
	broken := `{"root":` + strconv.Quote(latest.Root) + `,"turn":"` + first + `","ended":"yes"}`
	if err := os.WriteFile(path, []byte(broken), 0o600); err != nil {
		t.Fatal(err)
	}
	got, known, rerr := readSessionLatest("planning")
	t.Logf("the state %s reads as %+v known=%v err=%v", broken, got, known, rerr)
	next := f.prepare(f.ask("Now rename the export button, a new task."), "export button")
	if carried := next["carried"]; carried != nil {
		t.Errorf("INVARIANT 9: a state that cannot be read hands request 1 (ended) over: %.200s", auditJSON(carried))
	}
}
