package main

import (
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The lines a forbidden-words guard stopped at a Stop are kept as keyed hashes, once per repository (records/stopped-lines/,
// guard_stopped.go), and every comparison counts exactly those lines as added: they stay added lines until they are removed
// or committed, and every other uncommitted line (the person's drafts, in the same file or another one) is a line from
// before the request. The first four tests reproduce what the audit of 0.30.8 found with an earlier rule (all the guard's
// lines of the files the stopped turn changed were left out).

// stoppedRecordOf is the record of the stopped lines of repo.
func stoppedRecordOf(root, repo string) string {
	return stoppedLinesPath(root, feedbackScope(repo))
}

// A1: the person's draft from before request 1 sits in the file where turn 1 wrote the forbidden line it was stopped for. The
// next turn fixes its own line; the draft is not an added line.
func TestDraftBeforeAStoppedTurnInTheSameFileIsNotAnAddedLine(t *testing.T) {
	repo, root, readme := uncommittedLinesRepo(t)
	writeFile(t, readme, "# project\n"+draftLine+"\n")
	organizedRequest(t, repo, root, "a1")
	writeFile(t, readme, "# project\n"+draftLine+"\n"+turnForbiddenLine+"\n")
	if out := stopHook(t, repo, "a1", false); !strings.Contains(out, `"decision":"block"`) {
		t.Fatalf("premise: %q", out)
	}
	nextRequest(t, repo, root, "a1")
	writeFile(t, readme, "# project\n"+draftLine+"\nthe turn fixed its line\n")
	if out := stopHook(t, repo, "a1", false); stoppedOrEnded(out) || strings.Contains(out, "no-word") {
		t.Fatalf("the person's draft from before the request stopped the next turn: %q", out)
	}
}

// A2: after turn 1 was stopped in README, the person writes a draft into another file between the turns. The next turn
// changing that file is not stopped for the draft.
func TestDraftBetweenTurnsInAnotherFileIsNotAnAddedLine(t *testing.T) {
	repo, root, readme := uncommittedLinesRepo(t)
	notes := filepath.Join(repo, "notes.md")
	organizedRequest(t, repo, root, "a2")
	writeFile(t, readme, "# project\n"+turnForbiddenLine+"\n")
	if out := stopHook(t, repo, "a2", false); !strings.Contains(out, `"decision":"block"`) {
		t.Fatalf("premise: %q", out)
	}
	writeFile(t, notes, draftLine+"\n") // the person, between the turns
	nextRequest(t, repo, root, "a2")
	writeFile(t, notes, draftLine+"\na clean line\n")
	if out := stopHook(t, repo, "a2", false); stoppedOrEnded(out) || strings.Contains(out, "no-word") {
		t.Fatalf("the person's draft in a file the stopped turn did not change stopped the next turn: %q", out)
	}
}

// A3: the request's turn writes a forbidden line and is interrupted (no Stop); the /commit stretch changes another file and is
// stopped for the request's line (compared with the request's start); nothing is fixed. The next turn changing README is
// stopped again for that line (0.30.7 stopped it too).
func TestLineAStretchStoppedForTheRequestTurnStopsTheNextTurn(t *testing.T) {
	repo, root, readme := uncommittedLinesRepo(t)
	other := filepath.Join(repo, "other.md")
	organizedRequest(t, repo, root, "a3")
	writeFile(t, readme, "# project\n"+turnForbiddenLine+"\n") // interrupted: no Stop comes
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "a3", "cwd": repo, "prompt": "/commit"})
	if loadSession(root, "a3").GuardTurn == nil {
		t.Fatal("premise: stretch opened")
	}
	writeFile(t, other, "a clean line\n")
	if out := stopHook(t, repo, "a3", false); !strings.Contains(out, `"decision":"block"`) || !strings.Contains(out, "README.md") {
		t.Fatalf("premise: the stretch is stopped for the request's line: %q", out)
	}
	stopHook(t, repo, "a3", true)
	nextRequest(t, repo, root, "a3")
	writeFile(t, readme, "# project\n"+turnForbiddenLine+"\na clean line\n")
	if out := stopHook(t, repo, "a3", false); !strings.Contains(out, `"decision":"block"`) || !strings.Contains(out, `README.md: \"FORBIDDENWORD\"`) {
		t.Fatalf("a line stopped and never fixed passes at the next turn: %q", out)
	}
}

// B: the /commit stretch (after a judged request turn) writes a forbidden line, is stopped and ends without fixing it. The
// next request's turn changing the same file is stopped again for it.
func TestLineTheStretchItselfStoppedStopsTheNextRequest(t *testing.T) {
	repo, root, readme := uncommittedLinesRepo(t)
	organizedRequest(t, repo, root, "b")
	writeFile(t, readme, "# project\na clean line\n")
	if out := stopHook(t, repo, "b", false); stoppedOrEnded(out) {
		t.Fatalf("premise: %q", out)
	}
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "b", "cwd": repo, "prompt": "/commit"})
	writeFile(t, readme, "# project\na clean line\n"+turnForbiddenLine+"\n")
	if out := stopHook(t, repo, "b", false); !strings.Contains(out, `"decision":"block"`) {
		t.Fatalf("premise: the stretch is stopped: %q", out)
	}
	stopHook(t, repo, "b", true)
	nextRequest(t, repo, root, "b")
	writeFile(t, readme, "# project\na clean line\n"+turnForbiddenLine+"\nanother clean line\n")
	if out := stopHook(t, repo, "b", false); !strings.Contains(out, `"decision":"block"`) {
		t.Fatalf("a line the stretch was stopped for and never fixed passes at the next turn: %q", out)
	}
}

// wordStop runs a Stop and returns the record no-word kept for it. The routine check of the hook may also stop a later turn of
// these sessions (its reflection was not saved), so whether the guard stopped is read from the guard's own record.
func wordStop(t *testing.T, repo, root, session string, active bool) guardRecord {
	t.Helper()
	before := len(guardRecordsFor(root, "no-word"))
	out := stopHook(t, repo, session, active)
	recs := guardRecordsFor(root, "no-word")
	if len(recs) != before+1 {
		t.Fatalf("premise: the Stop keeps one record of no-word: %d -> %d (%q)", before, len(recs), out)
	}
	return recs[len(recs)-1]
}

func stoppedForTheLine(r guardRecord) bool {
	return !r.Pass && strings.Contains(r.Reason, `README.md: "FORBIDDENWORD"`)
}

// stoppedOnce makes turn 1 of session write the forbidden line into README and be stopped for it, then end without fixing it.
func stoppedOnce(t *testing.T, repo, root, readme, session string) {
	t.Helper()
	organizedRequest(t, repo, root, session)
	writeFile(t, readme, "# project\n"+turnForbiddenLine+"\n")
	if r := wordStop(t, repo, root, session, false); !stoppedForTheLine(r) || r.Recheck {
		t.Fatalf("premise: the turn's own forbidden line is refused: %+v", r)
	}
	wordStop(t, repo, root, session, true)
	if _, err := os.Stat(stoppedRecordOf(root, repo)); err != nil {
		t.Fatalf("premise: the stopped line is kept: %v", err)
	}
}

// The stopped line is carried from request to request: it stops the next turn that changes README, a turn that does not
// touch README passes and keeps it, and the turn after that changing README is stopped for it again.
func TestStoppedLineIsCarriedAcrossAnIdleTurn(t *testing.T) {
	repo, root, readme := uncommittedLinesRepo(t)
	stoppedOnce(t, repo, root, readme, "c")
	nextRequest(t, repo, root, "c")
	writeFile(t, readme, "# project\n"+turnForbiddenLine+"\na clean line\n")
	if r := wordStop(t, repo, root, "c", false); !stoppedForTheLine(r) {
		t.Fatalf("turn 2: the stopped line passes: %+v", r)
	}
	wordStop(t, repo, root, "c", true)
	nextRequest(t, repo, root, "c") // turn 3 changes nothing
	if r := wordStop(t, repo, root, "c", false); !r.Pass {
		t.Fatalf("premise: the idle turn passes: %+v", r)
	}
	if _, err := os.Stat(stoppedRecordOf(root, repo)); err != nil {
		t.Fatalf("the idle turn dropped the stopped line: %v", err)
	}
	nextRequest(t, repo, root, "c")
	writeFile(t, readme, "# project\n"+turnForbiddenLine+"\na clean line\nanother clean line\n")
	if r := wordStop(t, repo, root, "c", false); !stoppedForTheLine(r) {
		t.Fatalf("turn 4, after an idle turn: the stopped line passes: %+v", r)
	}
}

// A stopped line fixed at a later Stop of the same turn is forgotten: the record goes.
func TestStoppedLineFixedInTheSameTurnRemovesTheStoppedFile(t *testing.T) {
	repo, root, readme := uncommittedLinesRepo(t)
	organizedRequest(t, repo, root, "f")
	writeFile(t, readme, "# project\n"+turnForbiddenLine+"\n")
	if r := wordStop(t, repo, root, "f", false); !stoppedForTheLine(r) {
		t.Fatalf("premise: %+v", r)
	}
	if _, err := os.Stat(stoppedRecordOf(root, repo)); err != nil {
		t.Fatalf("the stopped line is not kept: %v", err)
	}
	writeFile(t, readme, "# project\nthe turn wrote a clean line\n")
	if r := wordStop(t, repo, root, "f", true); !r.Pass {
		t.Fatalf("premise: the fixed turn passes: %+v", r)
	}
	if _, err := os.Stat(stoppedRecordOf(root, repo)); !os.IsNotExist(err) {
		t.Fatalf("the record is left after the same turn fixed the line: %v", err)
	}
}

// Once the stopped line is removed, the person writing the same word later on another line is not stopped for it.
func TestRemovedStoppedLineIsForgotten(t *testing.T) {
	repo, root, readme := uncommittedLinesRepo(t)
	stoppedOnce(t, repo, root, readme, "r")
	nextRequest(t, repo, root, "r")
	writeFile(t, readme, "# project\nthe turn removed its line\n")
	if r := wordStop(t, repo, root, "r", false); !r.Pass {
		t.Fatalf("premise: the turn that removed the line passes: %+v", r)
	}
	writeFile(t, readme, "# project\nthe turn removed its line\n"+draftLine+"\n") // the person, between the turns
	nextRequest(t, repo, root, "r")
	writeFile(t, readme, "# project\nthe turn removed its line\n"+draftLine+"\na clean line\n")
	if r := wordStop(t, repo, root, "r", false); !r.Pass {
		t.Fatalf("the person's later line with the same word stopped the turn: %+v", r)
	}
}

// Once the stopped line is committed, a turn changing README is not stopped for it, and its Stop forgets it.
func TestCommittedStoppedLineIsForgotten(t *testing.T) {
	repo, root, readme := uncommittedLinesRepo(t)
	stoppedOnce(t, repo, root, readme, "m")
	gitCommitAll(t, repo, "docs: the person commits it")
	nextRequest(t, repo, root, "m")
	writeFile(t, readme, "# project\n"+turnForbiddenLine+"\na clean line\n")
	if r := wordStop(t, repo, root, "m", false); !r.Pass {
		t.Fatalf("the committed line stopped the next turn: %+v", r)
	}
	if _, err := os.Stat(stoppedRecordOf(root, repo)); !os.IsNotExist(err) {
		t.Fatalf("the committed line is still kept as stopped: %v", err)
	}
}

// Only the copies a guard stopped are left out: the person's draft and the turn's stopped line are the same text; the turn's
// copy is stopped, and once the next turn removes that copy, the person's own copy is not an added line.
func TestStoppedLineCountsOnlyTheStoppedCopies(t *testing.T) {
	repo, root, readme := uncommittedLinesRepo(t)
	writeFile(t, readme, "# project\n"+turnForbiddenLine+"\n") // the person's draft
	organizedRequest(t, repo, root, "n")
	writeFile(t, readme, "# project\n"+turnForbiddenLine+"\n"+turnForbiddenLine+"\n")
	if r := wordStop(t, repo, root, "n", false); !stoppedForTheLine(r) {
		t.Fatalf("premise: the turn's second copy is refused: %+v", r)
	}
	wordStop(t, repo, root, "n", true)
	nextRequest(t, repo, root, "n")
	writeFile(t, readme, "# project\n"+turnForbiddenLine+"\na clean line\n")
	if r := wordStop(t, repo, root, "n", false); !r.Pass {
		t.Fatalf("the person's own copy of the line stopped the turn that removed the stopped copy: %+v", r)
	}
}

// The record keeps keyed hashes only, readable by the person alone.
func TestStoppedFileHoldsNoContent(t *testing.T) {
	repo, root, readme := uncommittedLinesRepo(t)
	stoppedOnce(t, repo, root, readme, "k")
	p := stoppedRecordOf(root, repo)
	b := mustRead(t, p)
	for _, word := range []string{"FORBIDDENWORD", "the turn wrote", "README", "# project"} {
		if strings.Contains(string(b), word) {
			t.Fatalf("the record holds the content %q: %q", word, b)
		}
	}
	if info, err := os.Stat(p); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("the record is readable by the person only: %v %v", info.Mode(), err)
	}
	st := readStoppedTable(p, feedbackScope(repo))
	if st == nil || len(st.files["README.md"]) != 1 || st.files["README.md"][newLineHasher(st.key).hash([]byte(turnForbiddenLine))] != 1 {
		t.Fatalf("the record does not hold the keyed hash of the stopped line once: %+v", st)
	}
	if strings.Contains(string(b), lineHash(nil, turnForbiddenLine)) {
		t.Fatalf("the record holds a hash made without its key: %q", b)
	}
}

// A record that names another repository (a hash of the path that met another one, a file copied in) is not used: the same
// line, the person's draft here, is a line from before the request, and that record is left as it is.
func TestStoppedFileOfAnotherRepositoryIsNotUsed(t *testing.T) {
	repo, root, readme := uncommittedLinesRepo(t)
	stoppedOnce(t, repo, root, readme, "o")
	p := stoppedRecordOf(root, repo)
	head, body, _ := strings.Cut(string(mustRead(t, p)), "\n")
	_, key, _ := strings.Cut(strings.TrimPrefix(head, stoppedMarker), " ")
	other := stoppedMarker + hex.EncodeToString([]byte("/elsewhere/another-repository")) + " " + key + "\n" + body
	if err := writeAtomic(p, []byte(other)); err != nil {
		t.Fatal(err)
	}
	nextRequest(t, repo, root, "o")
	writeFile(t, readme, "# project\n"+turnForbiddenLine+"\na clean line\n")
	if r := wordStop(t, repo, root, "o", false); !r.Pass {
		t.Fatalf("another repository's record counted the line as added here: %+v", r)
	}
	if got := string(mustRead(t, p)); got != other {
		t.Fatalf("another repository's record was changed: %q", got)
	}
}

// The per-session .stopped of the earlier 0.30.8 builds is no longer read, and is pruned like the listings: an old orphan
// goes, a session still in use keeps it.
func TestStoppedFileIsPrunedWithTheListings(t *testing.T) {
	root := t.TempDir()
	old := time.Now().Add(-hookSessionTTL - time.Hour)
	if err := saveSession(root, "live", &hookSession{}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"live", "orphan"} {
		p := keptLinesPath(root, id, ".stopped")
		if err := writeAtomic(p, []byte("#stopped1 ")); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(p, old, old); err != nil {
			t.Fatal(err)
		}
	}
	pruneSessions(root)
	if _, err := os.Stat(keptLinesPath(root, "orphan", ".stopped")); !os.IsNotExist(err) {
		t.Errorf("an old orphan .stopped is left behind: %v", err)
	}
	if _, err := os.Stat(keptLinesPath(root, "live", ".stopped")); err != nil {
		t.Errorf("the .stopped of a session still in use was removed: %v", err)
	}
}
