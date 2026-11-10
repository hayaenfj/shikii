package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/store"
)

// slashGuardRepo is a repository with one commit and two enabled guards: a forbidden word and a commit body.
func slashGuardRepo(t *testing.T) (repo, root, readme string) {
	t.Helper()
	model := fakeAgentModel(t, func(string) float64 { return .1 })
	hookEnv(t, model)
	repo = gitRepo(t)
	t.Chdir(repo)
	capture(t, func() error { return cmdInit(nil) })
	root = filepath.Join(repo, ".shikii")
	readme = filepath.Join(repo, "README.md")
	if err := os.WriteFile(readme, []byte("# project\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitCommitAll(t, repo, "init")
	writeGuard(t, root, guard{ID: "no-word", Kind: guardKindForbiddenWords, Title: "do not use the word", Words: []string{"FORBIDDENWORD"}})
	writeGuard(t, root, guard{ID: "commit-body", Kind: guardKindCommitBody, Title: "the body names the problem and the verification", Required: []string{"Problem", "Verification"}})
	return repo, root, readme
}

// decisionRecordLines counts the kept decisions of the hook's four questions (two at the request, two at the end of the turn).
func decisionRecordLines(root string) int {
	n := 0
	for _, id := range hookSpecs {
		b, _ := os.ReadFile(filepath.Join(root, "records", id+".jsonl"))
		n += bytes.Count(b, []byte("\n"))
	}
	return n
}

func builtinGuardRecords(root string) int {
	n := 0
	for _, r := range readGuardRecords(root) {
		if isBuiltinGuard(r.Guard) {
			n++
		}
	}
	return n
}

// A turn started by a slash command (/commit and the like) is not a request, but the person's guards still look at it: the
// request hook keeps the working tree and HEAD of that moment for the guards alone, and the Stop applies the guards to what
// changed and what was committed since then. A guard that fails stops the turn once, as in any other turn.
func TestGuardsStopATurnStartedByASlashCommand(t *testing.T) {
	// the slash command's turn: a forbidden line, committed with a subject only
	slashTurn := func(t *testing.T, repo, readme string) string {
		t.Helper()
		if out := runHook(t, "user-prompt-submit", map[string]any{"session_id": "c", "cwd": repo, "prompt": "/commit the README change"}); out != "" {
			t.Fatalf("the request hook says nothing for a slash command: %s", out)
		}
		if err := os.WriteFile(readme, []byte("# project\n\nFORBIDDENWORD here\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		gitCommitAll(t, repo, "docs: subject only")
		return runHook(t, "stop", map[string]any{"session_id": "c", "cwd": repo, "last_assistant_message": "committed"})
	}

	t.Run("the first message of the session", func(t *testing.T) {
		repo, root, readme := slashGuardRepo(t)
		out := slashTurn(t, repo, readme)
		if !strings.Contains(out, `"decision":"block"`) || !strings.Contains(out, "- no-word:") || !strings.Contains(out, "- commit-body:") {
			t.Fatalf("both guards stop the slash command's turn: %q", out)
		}
		if s := loadSession(root, "c"); s.TurnID != "" || s.Request != "" || s.Planning != nil {
			t.Fatalf("a slash command is still not kept as a request: turn=%q request=%q planning=%v", s.TurnID, s.Request, s.Planning != nil)
		}
		stretch := ""
		for _, id := range []string{"no-word", "commit-body"} {
			recs := guardRecordsFor(root, id)
			if len(recs) != 1 || recs[0].Pass || recs[0].Recheck || recs[0].Turn == "" {
				t.Fatalf("the stop of %s is recorded under the slash command's own stretch: %+v", id, recs)
			}
			if stretch != "" && recs[0].Turn != stretch {
				t.Fatalf("both guards record the same stretch: %s and %s", stretch, recs[0].Turn)
			}
			stretch = recs[0].Turn
		}
		if line := guardsStatusLine(t, root); !strings.Contains(line, "stopped 1 turns, fixed before the turn ended 0") {
			t.Fatalf("status counts the stopped turn: %s", line)
		}
		// the Stop after the stop, not fixed: once per turn, and the turn is not ended as unverifiable either
		out = runHook(t, "stop", map[string]any{"session_id": "c", "cwd": repo, "last_assistant_message": "not yet", "stop_hook_active": true})
		if strings.Contains(out, `"decision":"block"`) || strings.Contains(out, `"continue":false`) || strings.Contains(out, "no-word") {
			t.Fatalf("the guards stop the slash command's turn once: %q", out)
		}
		for _, id := range []string{"no-word", "commit-body"} {
			if recs := guardRecordsFor(root, id); len(recs) != 2 || recs[1].Pass || !recs[1].Recheck || recs[1].Turn != stretch {
				t.Fatalf("the Stop after the stop of %s is a recheck of the same stretch: %+v", id, recs)
			}
		}
		// fixed: the line is removed and the commit gets its body
		if err := os.WriteFile(readme, []byte("# project\n\na clean line\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		gitAs(t, repo, "add", "-A")
		gitAs(t, repo, "commit", "-q", "--amend", "-m", "docs: a line\n\nProblem: the line was missing.\nVerification: read the README.")
		out = runHook(t, "stop", map[string]any{"session_id": "c", "cwd": repo, "last_assistant_message": "fixed", "stop_hook_active": true})
		for _, id := range []string{"no-word", "commit-body"} {
			if recs := guardRecordsFor(root, id); strings.Contains(out, id) || len(recs) != 3 || !recs[2].Pass || !recs[2].Recheck {
				t.Fatalf("the fix of %s is recorded: %q %+v", id, out, recs)
			}
		}
		if line := guardsStatusLine(t, root); !strings.Contains(line, "stopped 1 turns, fixed before the turn ended 1") {
			t.Fatalf("status counts the fix: %s", line)
		}
	})

	t.Run("after an earlier request, whose record is left as it was", func(t *testing.T) {
		repo, root, readme := slashGuardRepo(t)
		runHook(t, "user-prompt-submit", map[string]any{"session_id": "c", "cwd": repo, "prompt": "add a notes file about the storage layout"})
		first := loadSession(root, "c").TurnID
		submitOrganizing(t, "c", first)
		if err := os.WriteFile(filepath.Join(repo, "NOTES.md"), []byte("the storage layout\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if out := runHook(t, "stop", map[string]any{"session_id": "c", "cwd": repo, "last_assistant_message": "added the notes"}); strings.Contains(out, "block") || !loadSession(root, "c").Judged {
			t.Fatalf("premise: the first turn ends clean and is judged: %s", out)
		}
		before := loadSession(root, "c")
		listingBefore, err := os.ReadFile(sessionFilesPath(root, "c"))
		if err != nil || len(before.Watch) == 0 {
			t.Fatalf("premise: the first turn keeps its listing and a watch on its changed files: %v %+v", err, before.Watch)
		}
		decisions, routines := decisionRecordLines(root), builtinGuardRecords(root)

		out := slashTurn(t, repo, readme)
		if !strings.Contains(out, `"decision":"block"`) || !strings.Contains(out, "- no-word:") || !strings.Contains(out, "- commit-body:") {
			t.Fatalf("both guards stop the slash command's turn: %q", out)
		}
		if strings.Contains(out, "NOTES.md") {
			t.Fatalf("the guards look at what changed since the slash command, not since the earlier request: %q", out)
		}
		for _, id := range []string{"no-word", "commit-body"} {
			recs := guardRecordsFor(root, id)
			if len(recs) != 2 || recs[1].Pass || recs[1].Recheck || recs[1].Turn == "" || recs[1].Turn == first {
				t.Fatalf("the stop of %s is a stop of the slash command's stretch, not a recheck of the earlier turn %s: %+v", id, first, recs)
			}
		}
		if line := guardsStatusLine(t, root); !strings.Contains(line, "stopped 1 turns, fixed before the turn ended 0") {
			t.Fatalf("status counts the stopped turn: %s", line)
		}
		// the earlier request's record: the request, its turn, that it was judged, its reply and its watch are untouched
		after := loadSession(root, "c")
		if after.Request != before.Request || after.RequestText != before.RequestText || after.TurnID != first || !after.Judged || after.Previous != before.Previous ||
			after.Head != before.Head || !reflect.DeepEqual(after.Watch, before.Watch) || !reflect.DeepEqual(after.Pending, before.Pending) || !reflect.DeepEqual(after.Completed, before.Completed) {
			t.Fatalf("the earlier request's record changed:\nbefore %+v\nafter  %+v", before, after)
		}
		if listingAfter, err := os.ReadFile(sessionFilesPath(root, "c")); err != nil || !bytes.Equal(listingAfter, listingBefore) {
			t.Fatalf("the earlier request's listing changed: %v", err)
		}
		if d, r := decisionRecordLines(root), builtinGuardRecords(root); d != decisions || r != routines {
			t.Fatalf("the slash command's turn asked a question or ran the routine check: decisions %d -> %d, routine records %d -> %d", decisions, d, routines, r)
		}
		// the person's next request closes the stretch: its Stop is the request's own again
		runHook(t, "user-prompt-submit", map[string]any{"session_id": "c", "cwd": repo, "prompt": "remove the forbidden line from the README"})
		next := loadSession(root, "c")
		if next.TurnID == first || next.TurnID == "" || next.GuardTurn != nil {
			t.Fatalf("the next request starts its own turn and closes the slash command's stretch: %+v", next.GuardTurn)
		}
		submitOrganizing(t, "c", next.TurnID)
		if err := os.WriteFile(readme, []byte("# project\n\nanother FORBIDDENWORD\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		out = runHook(t, "stop", map[string]any{"session_id": "c", "cwd": repo, "last_assistant_message": "removed"})
		if recs := guardRecordsFor(root, "no-word"); !strings.Contains(out, "- no-word:") || recs[len(recs)-1].Turn != next.TurnID || recs[len(recs)-1].Recheck {
			t.Fatalf("the next request's Stop is checked as that request's turn: %q %+v", out, recs[len(recs)-1])
		}
	})
}

// A slash command whose turn changes nothing (/model and the like) stays silent: the request hook and the Stop output nothing
// and nothing is stopped. What was already in the repository before the slash command is not this turn's: a forbidden word in a
// committed file, a commit with only a subject, and what an earlier request's turn left unfixed are all outside the stretch.
func TestSlashCommandTurnWithoutChangesOutputsNothing(t *testing.T) {
	for name, earlier := range map[string]bool{"the first message of the session": false, "after an earlier request that left a guard unfixed": true} {
		t.Run(name, func(t *testing.T) {
			repo, root, readme := slashGuardRepo(t)
			// before any turn: the word is already in a committed file, and that commit has no body
			if err := os.WriteFile(readme, []byte("# project\n\nFORBIDDENWORD from before\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			gitCommitAll(t, repo, "docs: subject only, before the session")
			first := ""
			if earlier {
				runHook(t, "user-prompt-submit", map[string]any{"session_id": "m", "cwd": repo, "prompt": "add a notes file about the storage layout"})
				first = loadSession(root, "m").TurnID
				submitOrganizing(t, "m", first)
				if err := os.WriteFile(filepath.Join(repo, "NOTES.md"), []byte("FORBIDDENWORD in the notes\n"), 0o644); err != nil {
					t.Fatal(err)
				}
				if out := runHook(t, "stop", map[string]any{"session_id": "m", "cwd": repo, "last_assistant_message": "added the notes"}); !strings.Contains(out, "- no-word:") {
					t.Fatalf("premise: the guard stops the earlier request's turn: %s", out)
				}
				if out := runHook(t, "stop", map[string]any{"session_id": "m", "cwd": repo, "last_assistant_message": "left as it is", "stop_hook_active": true}); strings.Contains(out, "block") || !loadSession(root, "m").Judged {
					t.Fatalf("premise: the earlier turn ends unfixed and is judged: %s", out)
				}
			}
			stops := guardsStatusLine(t, root)
			if out := runHook(t, "user-prompt-submit", map[string]any{"session_id": "m", "cwd": repo, "prompt": "/model opus"}); out != "" {
				t.Fatalf("the request hook says nothing for a slash command: %q", out)
			}
			if out := runHook(t, "stop", map[string]any{"session_id": "m", "cwd": repo, "last_assistant_message": "Set model to opus"}); out != "" {
				t.Fatalf("the Stop of a slash command's turn that changed nothing says nothing: %q", out)
			}
			for _, id := range []string{"no-word", "commit-body"} {
				recs := guardRecordsFor(root, id)
				if len(recs) == 0 {
					t.Fatalf("the guard %s was not applied to the slash command's turn", id)
				}
				if last := recs[len(recs)-1]; !last.Pass || last.Turn == "" || last.Turn == first {
					t.Fatalf("the guard %s passes for the slash command's own stretch: %+v", id, recs)
				}
			}
			if line := guardsStatusLine(t, root); line != stops {
				t.Fatalf("status counts no new stop: %s -> %s", stops, line)
			}
		})
	}
}

// A slash command's turn gets none of what a request gets: no question is asked at the request or at the end of the turn, and the
// routine check does not run, even when the earlier request was left unfinished (its Stop never came and its organizing was not
// saved). That request's record stays as it was, still not judged.
func TestSlashCommandTurnGetsNoQuestionsAndNoRoutineCheck(t *testing.T) {
	repo, root, readme := slashGuardRepo(t)
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "q", "cwd": repo, "prompt": "add a section about the storage layout to the README"})
	before := loadSession(root, "q")
	if before.Planning == nil || before.TurnID == "" {
		t.Fatal("premise: the request asks for the organizing")
	}
	// the agent changed a file, the person interrupted: no Stop, and the organizing was never saved
	if err := os.WriteFile(readme, []byte("# project\n\na clean line\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	decisions, routines := decisionRecordLines(root), builtinGuardRecords(root)
	if decisions == 0 {
		t.Fatal("premise: the request's questions were kept")
	}

	if out := runHook(t, "user-prompt-submit", map[string]any{"session_id": "q", "cwd": repo, "prompt": "/commit"}); out != "" {
		t.Fatalf("the request hook adds nothing for a slash command (no question, no lesson, no organizing or reflection notice): %q", out)
	}
	gitAs(t, repo, "add", "-A")
	gitAs(t, repo, "commit", "-q", "-m", "docs: a line\n\nProblem: the line was missing.\nVerification: read the README.")
	// the Stop as production runs it: the unfinished request's ending review is not supplied, and it is not what this Stop verifies
	out := runUnreviewedHook(t, "stop", map[string]any{"session_id": "q", "cwd": repo, "last_assistant_message": "committed"})
	if out != "" {
		t.Fatalf("the Stop of the slash command's turn runs no routine check and asks nothing: %q", out)
	}
	if d, r := decisionRecordLines(root), builtinGuardRecords(root); d != decisions || r != routines {
		t.Fatalf("the slash command's turn asked a question or ran the routine check: decisions %d -> %d, routine records %d -> %d", decisions, d, routines, r)
	}
	after := loadSession(root, "q")
	if after.Request != before.Request || after.TurnID != before.TurnID || after.Judged || after.RitualStopped != "" || after.Planning == nil || after.Planning.Result != nil {
		t.Fatalf("the unfinished request's record changed: %+v", after)
	}
	// the guards did look at the slash command's turn: both passed, under the stretch's id
	for _, id := range []string{"no-word", "commit-body"} {
		if recs := guardRecordsFor(root, id); len(recs) != 1 || !recs[0].Pass || recs[0].Turn == before.TurnID || recs[0].Turn == "" {
			t.Fatalf("the guard %s passed for the slash command's stretch: %+v", id, recs)
		}
	}
}

// A command a forbidden-commands guard refuses in a slash command's turn is recorded under that stretch, so the Stop of the
// same stretch closes it as fixed (the refused command never ran).
func TestCommandRefusedInASlashCommandTurnCountsAsFixedAtItsStop(t *testing.T) {
	repo, root, _ := slashGuardRepo(t)
	writeGuard(t, root, guard{ID: "no-pipe-commit", Kind: guardKindForbiddenCommands, Title: "do not pipe a check into tail before committing", Patterns: []string{`\|\s*tail\b[^\n]*\bgit\s+commit\b`}, Example: "make test | tail -3; git commit -m x"})
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "t", "cwd": repo, "prompt": "/commit"})
	out := runHook(t, "tool-lesson", map[string]any{"session_id": "t", "cwd": repo, "hook_event_name": "PreToolUse", "tool_name": "Bash", "tool_input": map[string]any{"command": "make test | tail -3 && git commit -m x"}})
	if !strings.Contains(out, `"permissionDecision":"deny"`) {
		t.Fatalf("premise: the command is refused before it runs: %q", out)
	}
	if out := runHook(t, "stop", map[string]any{"session_id": "t", "cwd": repo, "last_assistant_message": "did not commit"}); out != "" {
		t.Fatalf("nothing else fails: %q", out)
	}
	recs := guardRecordsFor(root, "no-pipe-commit")
	if len(recs) != 2 || !recs[0].Tool || recs[0].Turn == "" || recs[1].Turn != recs[0].Turn || !recs[1].Pass || !recs[1].Recheck {
		t.Fatalf("the refusal and the passing Stop belong to the same stretch: %+v", recs)
	}
	if line := guardsStatusLine(t, root); !strings.Contains(line, "stopped 1 turns, fixed before the turn ended 1") {
		t.Fatalf("status counts the refusal as stopped and fixed: %s", line)
	}
}

// A completion notice of background work is not a slash command: work that goes on after it belongs to the kept request again, as
// it did before slash commands opened a stretch. The notice closes the stretch and the request is judged once more at the next
// Stop, where the guards compare with the request's start. Without a kept request there is nothing to give the turn back to.
func TestNoticeAfterASlashCommandGivesTheTurnBackToTheRequest(t *testing.T) {
	notice := "<task-notification>\n<task-id>b1</task-id>\n<status>completed</status>\n</task-notification>"
	t.Run("after an earlier request", func(t *testing.T) {
		repo, root, readme := slashGuardRepo(t)
		runHook(t, "user-prompt-submit", map[string]any{"session_id": "n", "cwd": repo, "prompt": "explain what the README says about the storage layout"})
		first := loadSession(root, "n").TurnID
		submitOrganizing(t, "n", first)
		if out := runHook(t, "stop", map[string]any{"session_id": "n", "cwd": repo, "last_assistant_message": "explained"}); strings.Contains(out, "block") || !loadSession(root, "n").Judged {
			t.Fatalf("premise: the first turn ends clean and is judged: %s", out)
		}
		runHook(t, "user-prompt-submit", map[string]any{"session_id": "n", "cwd": repo, "prompt": "/commit"})
		if loadSession(root, "n").GuardTurn == nil {
			t.Fatal("premise: the slash command opens a stretch")
		}
		if out := runHook(t, "user-prompt-submit", map[string]any{"session_id": "n", "cwd": repo, "prompt": notice}); out != "" {
			t.Fatalf("a notice outputs nothing: %q", out)
		}
		s := loadSession(root, "n")
		if s.GuardTurn != nil || s.Judged || s.TurnID != first {
			t.Fatalf("the notice closes the stretch and reopens the request's judgment: stretch=%v judged=%v turn=%s", s.GuardTurn != nil, s.Judged, s.TurnID)
		}
		if _, err := os.Stat(sessionGuardFilesPath(root, "n")); !os.IsNotExist(err) {
			t.Fatalf("the stretch's listing is removed with it: %v", err)
		}
		if err := os.WriteFile(readme, []byte("# project\n\nFORBIDDENWORD from the background work\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		out := runHook(t, "stop", map[string]any{"session_id": "n", "cwd": repo, "last_assistant_message": "the background work finished"})
		recs := guardRecordsFor(root, "no-word")
		if !strings.Contains(out, "- no-word:") || recs[len(recs)-1].Turn != first || recs[len(recs)-1].Recheck {
			t.Fatalf("the Stop after the notice is the request's own turn again: %q %+v", out, recs[len(recs)-1])
		}
	})
	t.Run("the session began with the slash command", func(t *testing.T) {
		repo, root, readme := slashGuardRepo(t)
		runHook(t, "user-prompt-submit", map[string]any{"session_id": "n", "cwd": repo, "prompt": "/commit"})
		stretch := loadSession(root, "n").GuardTurn
		if stretch == nil {
			t.Fatal("premise: the slash command opens a stretch")
		}
		runHook(t, "user-prompt-submit", map[string]any{"session_id": "n", "cwd": repo, "prompt": notice})
		if g := loadSession(root, "n").GuardTurn; g == nil || g.ID != stretch.ID {
			t.Fatalf("without a kept request the stretch stays: %+v", g)
		}
		if err := os.WriteFile(readme, []byte("# project\n\nFORBIDDENWORD from the background work\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		out := runHook(t, "stop", map[string]any{"session_id": "n", "cwd": repo, "last_assistant_message": "the background work finished"})
		if recs := guardRecordsFor(root, "no-word"); !strings.Contains(out, "- no-word:") || len(recs) != 1 || recs[0].Turn != stretch.ID {
			t.Fatalf("the guards still look at the slash command's stretch: %q %+v", out, recs)
		}
	})
}

// A slash command between a turn that waits for its late judgment and the person's next request does not swallow that judgment:
// the next request still judges the request's own turn (not the stretch), and the stretch ends there.
func TestSlashCommandBeforeTheNextRequestKeepsTheLateJudgment(t *testing.T) {
	repo, root, _ := planningFixture(t)
	writeGuard(t, root, guard{ID: "no-word", Kind: guardKindForbiddenWords, Title: "do not use the word", Words: []string{"FORBIDDENWORD"}})
	continuesTurn(t, repo, root, "tabs, not spaces")
	out := runHook(t, "stop", map[string]any{"session_id": "planning", "cwd": repo, "last_assistant_message": ""})
	if _, block := stopReason(t, out); !block {
		t.Fatalf("premise: the Stop of a short message asks for continues_previous: %s", out)
	}
	if _, err := planningCall(t, "submit", askedJSON(t, recoveryOutput(t, out)["reason"].(string))); err != nil {
		t.Fatal(err)
	}
	asked := loadSession(root, "planning").TurnID
	// no Stop after the block; the person runs a slash command, then asks the next thing
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": "/commit"})
	if s := loadSession(root, "planning"); s.GuardTurn == nil || s.Judged || s.TurnID != asked {
		t.Fatalf("premise: the slash command opens a stretch and leaves the asked turn waiting: %+v", s.GuardTurn)
	}
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": "Write the release notes for the next version, list every change since 0.9.2, and prepare them in English and Japanese."})
	recs, err := (store.Store{Dir: root}).Records("done-as-asked", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 2 {
		t.Fatalf("the asked turn is still judged late at the next request: %d records", len(recs))
	}
	if s := loadSession(root, "planning"); s.GuardTurn != nil || s.TurnID == asked {
		t.Fatalf("the next request starts its own turn and ends the stretch: %+v", s.GuardTurn)
	}
}

// The listing a slash command's turn kept is cleaned up with its session record, not before it: an old listing of a session
// still in use stays, and the listing of an old, finished session goes when its record goes.
func TestSlashCommandListingIsPrunedWithItsSessionRecord(t *testing.T) {
	root := t.TempDir()
	old := time.Now().Add(-hookSessionTTL - time.Hour)
	for i, id := range []string{"live", "gone"} {
		if err := saveSession(root, id, &hookSession{GuardTurn: &guardTurn{ID: fmt.Sprintf("%016d", i+1)}}); err != nil {
			t.Fatal(err)
		}
		if err := writeAtomic(sessionGuardFilesPath(root, id), []byte(listingHeader(fmt.Sprintf("%016d", i+1)))); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(sessionGuardFilesPath(root, id), old, old); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Chtimes(sessionPath(root, "gone"), old, old); err != nil {
		t.Fatal(err)
	}
	pruneSessions(root)
	if _, err := os.Stat(sessionGuardFilesPath(root, "live")); err != nil {
		t.Fatalf("the listing of a session still in use was removed: %v", err)
	}
	if _, err := os.Stat(sessionPath(root, "live")); err != nil {
		t.Fatalf("the record of a session still in use was removed: %v", err)
	}
	for _, p := range []string{sessionPath(root, "gone"), sessionGuardFilesPath(root, "gone")} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Fatalf("%s of an old session is left behind: %v", filepath.Base(p), err)
		}
	}
}

func gitAs(t *testing.T, repo string, args ...string) {
	t.Helper()
	full := append([]string{"-C", repo, "-c", "user.name=t", "-c", "user.email=t@example.com"}, args...)
	if out, err := exec.Command("git", full...).CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v %s", strings.Join(args, " "), err, out)
	}
}
