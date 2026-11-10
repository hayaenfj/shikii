package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A line the previous turn added that a forbidden-words guard stopped, and that was never fixed, is not a line "from before the
// request" at the next request: the next request's table leaves it out, so the next turn's Stop stops it again. The person's
// own uncommitted lines, lines in files the stopped turn did not change, and a stop that was fixed afterwards keep the table as
// before.

const turnForbiddenLine = "the turn wrote FORBIDDENWORD"

func stopHook(t *testing.T, repo, session string, active bool) string {
	t.Helper()
	return runHook(t, "stop", map[string]any{"session_id": session, "cwd": repo, "last_assistant_message": "added", "stop_hook_active": active})
}

func nextRequest(t *testing.T, repo, root, session string) {
	t.Helper()
	runHook(t, "user-prompt-submit", map[string]any{"session_id": session, "cwd": repo, "prompt": "finish the README section about the storage layout"})
	organizeTurn(t, session, loadSession(root, session).TurnID)
}

// The previous turn's forbidden line, stopped and left in place (the person interrupts), stops the next turn again when that turn
// changes the same file; also after the second Stop of the first turn recorded the failure as a recheck.
func TestBlockedLineOfThePreviousTurnStillStops(t *testing.T) {
	for name, recheck := range map[string]bool{"stopped once": false, "stopped, then a failing recheck": true} {
		t.Run(name, func(t *testing.T) {
			repo, root, readme := uncommittedLinesRepo(t)
			organizedRequest(t, repo, root, "w")
			writeFile(t, readme, "# project\n"+turnForbiddenLine+"\n")
			if out := stopHook(t, repo, "w", false); !strings.Contains(out, `"decision":"block"`) {
				t.Fatalf("premise: the turn's own forbidden line is refused: %q", out)
			}
			if recheck {
				stopHook(t, repo, "w", true)
				recs := guardRecordsFor(root, "no-word")
				if last := recs[len(recs)-1]; last.Pass || !last.Recheck {
					t.Fatalf("premise: the second Stop records a failing recheck: %+v", recs)
				}
			}
			nextRequest(t, repo, root, "w")
			writeFile(t, readme, "# project\n"+turnForbiddenLine+"\na clean line\n")
			if out := stopHook(t, repo, "w", false); !strings.Contains(out, `"decision":"block"`) || !strings.Contains(out, `README.md: \"FORBIDDENWORD\"`) {
				t.Fatalf("the forbidden line the previous turn wrote (and was refused for) passes at the next turn's Stop: %q", out)
			}
		})
	}
}

// Negative: the person's own draft, written after a turn that passed, is not an added line of the next turn.
func TestDraftAfterAPassingTurnIsNotAnAddedLine(t *testing.T) {
	repo, root, readme := uncommittedLinesRepo(t)
	organizedRequest(t, repo, root, "p")
	writeFile(t, readme, "# project\na clean line\n")
	if out := stopHook(t, repo, "p", false); stoppedOrEnded(out) {
		t.Fatalf("premise: the clean turn passes: %q", out)
	}
	writeFile(t, readme, "# project\na clean line\n"+draftLine+"\n")
	nextRequest(t, repo, root, "p")
	writeFile(t, readme, "# project\na clean line\n"+draftLine+"\nanother clean line\n")
	if out := stopHook(t, repo, "p", false); stoppedOrEnded(out) || strings.Contains(out, "no-word") {
		t.Fatalf("the person's draft after a passing turn stopped the next turn: %q", out)
	}
}

// Negative: the stopped word in a file the stopped turn did not change (the person's draft from before that turn) stays in the
// table, so the next turn changing that file is not stopped for it.
func TestBlockedWordInAFileThePreviousTurnDidNotChangeIsKept(t *testing.T) {
	repo, root, readme := uncommittedLinesRepo(t)
	notes := filepath.Join(repo, "notes.md")
	writeFile(t, notes, draftLine+"\n")
	organizedRequest(t, repo, root, "f")
	writeFile(t, readme, "# project\n"+turnForbiddenLine+"\n")
	if out := stopHook(t, repo, "f", false); !strings.Contains(out, `"decision":"block"`) {
		t.Fatalf("premise: the turn's own forbidden line is refused: %q", out)
	}
	nextRequest(t, repo, root, "f")
	writeFile(t, notes, draftLine+"\na clean line\n")
	if out := stopHook(t, repo, "f", false); stoppedOrEnded(out) || strings.Contains(out, "no-word") {
		t.Fatalf("the person's draft in a file the stopped turn did not change stopped the next turn: %q", out)
	}
}

// Negative: a stop the previous turn fixed (a later Stop passed) leaves the table as before: the person's draft written into the
// same file afterwards is not an added line.
func TestBlockedWordFixedInThePreviousTurnKeepsTheTable(t *testing.T) {
	repo, root, readme := uncommittedLinesRepo(t)
	organizedRequest(t, repo, root, "x")
	writeFile(t, readme, "# project\n"+turnForbiddenLine+"\n")
	if out := stopHook(t, repo, "x", false); !strings.Contains(out, `"decision":"block"`) {
		t.Fatalf("premise: the turn's own forbidden line is refused: %q", out)
	}
	writeFile(t, readme, "# project\nthe turn wrote a clean line\n")
	stopHook(t, repo, "x", true)
	if recs := guardRecordsFor(root, "no-word"); !recs[len(recs)-1].Pass {
		t.Fatalf("premise: the fix is recorded as a pass: %+v", recs)
	}
	writeFile(t, readme, "# project\nthe turn wrote a clean line\n"+draftLine+"\n")
	nextRequest(t, repo, root, "x")
	writeFile(t, readme, "# project\nthe turn wrote a clean line\n"+draftLine+"\nanother clean line\n")
	if out := stopHook(t, repo, "x", false); stoppedOrEnded(out) || strings.Contains(out, "no-word") {
		t.Fatalf("the person's draft after a fixed stop stopped the next turn: %q", out)
	}
}

// Negative: only the lines the stopped guard itself stops are left out, with its except and allow applied as it applies them: a
// line another guard would stop, one whose only hit is an allowed word, and one in a file the stopped guard excepts stay in the
// table, so that other guard does not stop the next turn for the person's drafts.
func TestBlockedLinesFollowTheStoppedGuardAlone(t *testing.T) {
	repo, root, readme := uncommittedLinesRepo(t)
	writeGuard(t, root, guard{ID: "no-word", Kind: guardKindForbiddenWords, Title: "do not use the word", Words: []string{"FORBIDDENWORD"}, Allow: []string{"FORBIDDENWORDLIKE"}, Except: []string{"notes/**"}})
	writeGuard(t, root, guard{ID: "other-word", Kind: guardKindForbiddenWords, Title: "do not use the other word", Words: []string{"OTHERWORD"}})
	notes := filepath.Join(repo, "notes", "n.md")
	if err := os.MkdirAll(filepath.Dir(notes), 0o755); err != nil {
		t.Fatal(err)
	}
	const draft, inNotes = "the person's FORBIDDENWORDLIKE OTHERWORD note", "FORBIDDENWORD and OTHERWORD in the person's notes"
	writeFile(t, readme, "# project\n"+draft+"\n")
	writeFile(t, notes, inNotes+"\n")
	organizedRequest(t, repo, root, "e")
	writeFile(t, readme, "# project\n"+draft+"\n"+turnForbiddenLine+"\n")
	writeFile(t, notes, inNotes+"\na clean line\n")
	if out := stopHook(t, repo, "e", false); !strings.Contains(out, `"decision":"block"`) || strings.Contains(out, "other-word") {
		t.Fatalf("premise: only no-word refuses the turn's own line: %q", out)
	}
	nextRequest(t, repo, root, "e")
	writeFile(t, readme, "# project\n"+draft+"\n"+turnForbiddenLine+"\na clean line\n")
	writeFile(t, notes, inNotes+"\na clean line\nanother clean line\n")
	out := stopHook(t, repo, "e", false)
	if !strings.Contains(out, `"decision":"block"`) || !strings.Contains(out, "no-word") {
		t.Fatalf("the line no-word stopped is stopped again: %q", out)
	}
	if strings.Contains(out, "other-word") {
		t.Fatalf("a draft line only another guard stops, an allowed word or an excepted file was left out of the table: %q", out)
	}
}

// A slash command's stretch after a request whose turn was stopped and never fixed: the stretch's table leaves out the stopped
// line, so committing it in the stretch is stopped.
func TestBlockedLineOfTheRequestTurnStillStopsTheSlashCommandStretch(t *testing.T) {
	repo, root, readme := uncommittedLinesRepo(t)
	organizedRequest(t, repo, root, "s")
	writeFile(t, readme, "# project\n"+turnForbiddenLine+"\n")
	if out := stopHook(t, repo, "s", false); !strings.Contains(out, `"decision":"block"`) {
		t.Fatalf("premise: the turn's own forbidden line is refused: %q", out)
	}
	stopHook(t, repo, "s", true) // the turn ends without fixing it and is judged
	if !loadSession(root, "s").Judged {
		t.Fatal("premise: the request's turn was judged, so the stretch is compared with its own start")
	}
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "s", "cwd": repo, "prompt": "/commit"})
	writeFile(t, readme, "# project\n"+turnForbiddenLine+"\na clean line\n")
	gitCommitAll(t, repo, "docs: the storage layout")
	if out := stopHook(t, repo, "s", false); !strings.Contains(out, `"decision":"block"`) || !strings.Contains(out, `README.md: \"FORBIDDENWORD\"`) {
		t.Fatalf("the stretch commits the line the request's turn was stopped for and passes: %q", out)
	}
}
