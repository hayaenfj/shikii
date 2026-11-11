package main

import (
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// When the record of stopped lines drops an entry (guard_stopped.go): a Stop looks again only at the files its turn changed,
// so a stopped line out of sight for a while (stashed) and the files nobody touched keep their entries; a commit made after
// the line was stopped clears it in every comparison, not only at the next write; and a record is pruned only 7 days after
// its repository was last seen.

// The stopped line is stashed by the person (git stash) while another turn changes another file and ends. After the person
// pops the stash, the next turn changing README is stopped for the line again.
func TestStashedStoppedLineStopsAfterPopAcrossATurnOnAnotherFile(t *testing.T) {
	repo, root, readme := uncommittedLinesRepo(t)
	stoppedOnce(t, repo, root, readme, "stash-pop-other-turn")
	gitIn(t, repo, "stash")
	nextRequest(t, repo, root, "stash-pop-other-turn")
	writeFile(t, filepath.Join(repo, "notes.md"), "a clean note\n")
	if r := wordStop(t, repo, root, "stash-pop-other-turn", false); !r.Pass {
		t.Fatalf("premise: the turn on another file passes: %+v", r)
	}
	gitIn(t, repo, "stash", "pop")
	nextRequest(t, repo, root, "stash-pop-other-turn")
	writeFile(t, readme, "# project\n"+turnForbiddenLine+"\na clean line\n")
	if r := wordStop(t, repo, root, "stash-pop-other-turn", false); !stoppedForTheLine(r) {
		t.Fatalf("the stopped line, stashed and popped, passes at the next turn: %+v", r)
	}
}

// Control: without a turn in between, the stashed and popped line stops the next turn.
func TestStashedStoppedLineStopsAfterPop(t *testing.T) {
	repo, root, readme := uncommittedLinesRepo(t)
	stoppedOnce(t, repo, root, readme, "stash-pop")
	gitIn(t, repo, "stash")
	gitIn(t, repo, "stash", "pop")
	nextRequest(t, repo, root, "stash-pop")
	writeFile(t, readme, "# project\n"+turnForbiddenLine+"\na clean line\n")
	if r := wordStop(t, repo, root, "stash-pop", false); !stoppedForTheLine(r) {
		t.Fatalf("the stashed and popped line passes: %+v", r)
	}
}

// The person commits the stopped line and, with no Stop in between, writes one more copy of the same text beside it. The
// commit at the next turn's start still holds the stopped copy, so that copy is no longer stopped, and the extra copy was there
// at the request: the next turn changing README is not stopped for it. (The same text written again after the stopped line
// was removed, from the file or by a later commit, is another case: it cannot be told from the stopped line and stops.)
func TestPersonsCopyAfterTheStoppedLineWasCommittedIsNotAdded(t *testing.T) {
	repo, root, readme := uncommittedLinesRepo(t)
	stoppedOnce(t, repo, root, readme, "commit-then-copy")
	gitCommitAll(t, repo, "docs: the person commits the turn's line")
	writeFile(t, readme, "# project\n"+turnForbiddenLine+"\n"+turnForbiddenLine+"\n") // the person's own copy, between the turns
	nextRequest(t, repo, root, "commit-then-copy")
	writeFile(t, readme, "# project\n"+turnForbiddenLine+"\n"+turnForbiddenLine+"\na clean line\n")
	if r := wordStop(t, repo, root, "commit-then-copy", false); !r.Pass {
		t.Fatalf("the person's copy, written after the stopped line was committed, stopped the next turn: %+v", r)
	}
}

// Control: with a turn on another file in between, the person's copy passes too.
func TestPersonsCopyAfterTheStoppedLineWasCommittedAndATurnIsNotAdded(t *testing.T) {
	repo, root, readme := uncommittedLinesRepo(t)
	stoppedOnce(t, repo, root, readme, "commit-other-turn")
	gitCommitAll(t, repo, "docs: the person commits the turn's line")
	nextRequest(t, repo, root, "commit-other-turn")
	writeFile(t, filepath.Join(repo, "notes.md"), "a clean note\n")
	if r := wordStop(t, repo, root, "commit-other-turn", false); !r.Pass {
		t.Fatalf("premise: %+v", r)
	}
	writeFile(t, readme, "# project\n"+turnForbiddenLine+"\n"+turnForbiddenLine+"\n")
	nextRequest(t, repo, root, "commit-other-turn")
	writeFile(t, readme, "# project\n"+turnForbiddenLine+"\n"+turnForbiddenLine+"\na clean line\n")
	if r := wordStop(t, repo, root, "commit-other-turn", false); !r.Pass {
		t.Fatalf("the person's copy stopped the turn: %+v", r)
	}
}

// The record of a repository still in use, whose stopped line stays and whose content no Stop changed for 8 days, is kept
// when the repository is away for a moment (renamed, a disk not mounted) at the sessions' pruning.
func TestStoppedRecordOfARepositoryAwayForAMomentIsKept(t *testing.T) {
	root := t.TempDir()
	repo := gitRepo(t)
	writeFile(t, filepath.Join(repo, "README.md"), "# project\n")
	gitCommitAll(t, repo, "init")
	writeFile(t, filepath.Join(repo, "README.md"), "# project\n"+turnForbiddenLine+"\n")
	g := guard{ID: "no-word", Kind: guardKindForbiddenWords, Words: []string{"FORBIDDENWORD"}}
	top := feedbackScope(repo)
	head := gitHead(repo)
	keepStoppedLines(root, []guard{g}, guardInput{repo: top, Added: map[string][]string{"README.md": {turnForbiddenLine}}}, head)
	p := stoppedRecordOf(root, repo)
	old := time.Now().Add(-8 * 24 * time.Hour)
	if err := os.Chtimes(p, old, old); err != nil {
		t.Fatal(err)
	}
	// a later Stop that passes, the line still there
	keepStoppedLines(root, nil, guardInput{repo: top, Added: map[string][]string{}}, head)
	away := repo + ".away"
	if err := os.Rename(repo, away); err != nil {
		t.Fatal(err)
	}
	pruneSessions(root)
	if err := os.Rename(away, repo); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("the record of a repository away for a moment was removed at once: %v", err)
	}
}

// How many copies of an entry are still stopped: the copies committed after the line was stopped are not, and never more than
// the file holds beyond the comparing turn's start HEAD.
func TestStoppedEntryLeftCountsTheCommitsSinceTheLineWasStopped(t *testing.T) {
	for _, c := range []struct {
		name            string
		e               stoppedEntry
		now, head, want int
	}{
		{"nothing committed", stoppedEntry{n: 1, head: 0}, 1, 0, 1},
		{"committed since", stoppedEntry{n: 1, head: 0}, 2, 1, 0},
		{"a copy in HEAD before it was stopped", stoppedEntry{n: 1, head: 1}, 2, 1, 1},
		{"one of two committed since", stoppedEntry{n: 2, head: 0}, 2, 1, 1},
		{"removed", stoppedEntry{n: 1, head: 0}, 0, 0, 0},
		{"HEAD moved back", stoppedEntry{n: 1, head: 2}, 3, 0, 1},
		{"no more than beyond HEAD", stoppedEntry{n: 3, head: 0}, 2, 0, 2},
	} {
		if got := c.e.left(c.now, c.head); got != c.want {
			t.Errorf("%s: %+v with %d now and %d in HEAD leaves %d, want %d", c.name, c.e, c.now, c.head, got, c.want)
		}
	}
}

// The person had committed a copy of the line before the turn wrote it once more and was stopped for it. That copy in HEAD
// is the person's: the turn's copy still stops the next turn changing README.
func TestStoppedCopyOfALineAlreadyCommittedStopsTheNextTurn(t *testing.T) {
	repo, root, readme := uncommittedLinesRepo(t)
	writeFile(t, readme, "# project\n"+turnForbiddenLine+"\n")
	gitCommitAll(t, repo, "docs: the person's own line")
	organizedRequest(t, repo, root, "h0")
	writeFile(t, readme, "# project\n"+turnForbiddenLine+"\n"+turnForbiddenLine+"\n")
	if r := wordStop(t, repo, root, "h0", false); !stoppedForTheLine(r) {
		t.Fatalf("premise: the turn's copy is refused: %+v", r)
	}
	wordStop(t, repo, root, "h0", true)
	nextRequest(t, repo, root, "h0")
	writeFile(t, readme, "# project\n"+turnForbiddenLine+"\n"+turnForbiddenLine+"\na clean line\n")
	if r := wordStop(t, repo, root, "h0", false); !stoppedForTheLine(r) {
		t.Fatalf("the turn's copy of a line the person had committed passes at the next turn: %+v", r)
	}
}

// After the person commits the stopped line, a later turn writing the same text once more is stopped for its own copy, that
// copy stays stopped at the turn after, and once the person commits it too, the next turn passes.
func TestLineStoppedAgainAfterTheFirstCopyWasCommitted(t *testing.T) {
	repo, root, readme := uncommittedLinesRepo(t)
	two := "# project\n" + turnForbiddenLine + "\n" + turnForbiddenLine + "\n"
	stoppedOnce(t, repo, root, readme, "again")
	gitCommitAll(t, repo, "docs: the person commits the first copy")
	nextRequest(t, repo, root, "again")
	writeFile(t, readme, two)
	if r := wordStop(t, repo, root, "again", false); !stoppedForTheLine(r) {
		t.Fatalf("premise: the turn's new copy is refused: %+v", r)
	}
	wordStop(t, repo, root, "again", true)
	nextRequest(t, repo, root, "again")
	writeFile(t, readme, two+"a clean line\n")
	if r := wordStop(t, repo, root, "again", false); !stoppedForTheLine(r) {
		t.Fatalf("the second stopped copy passes at the next turn: %+v", r)
	}
	wordStop(t, repo, root, "again", true)
	gitCommitAll(t, repo, "docs: the person commits the second copy")
	nextRequest(t, repo, root, "again")
	writeFile(t, readme, two+"a clean line\nanother clean line\n")
	if r := wordStop(t, repo, root, "again", false); !r.Pass {
		t.Fatalf("the committed second copy stopped the next turn: %+v", r)
	}
}

// A Stop looks again only at the files its turn changed: the stopped line the person removed from README between the turns
// stays in the record after a turn that changed another file, and goes at the next Stop that compares README.
func TestEntriesOfAFileTheStopDidNotCompareAreKept(t *testing.T) {
	repo, root, readme := uncommittedLinesRepo(t)
	stoppedOnce(t, repo, root, readme, "u")
	writeFile(t, readme, "# project\n") // the person removes it, between the turns
	nextRequest(t, repo, root, "u")
	writeFile(t, filepath.Join(repo, "notes.md"), "a clean note\n")
	if r := wordStop(t, repo, root, "u", false); !r.Pass {
		t.Fatalf("premise: %+v", r)
	}
	if !stoppedRecordHas(t, root, repo, "README.md", turnForbiddenLine) {
		t.Fatal("a Stop that did not compare README dropped its entry")
	}
	nextRequest(t, repo, root, "u")
	writeFile(t, readme, "# project\na clean line\n")
	if r := wordStop(t, repo, root, "u", false); !r.Pass {
		t.Fatalf("premise: %+v", r)
	}
	if _, err := os.Stat(stoppedRecordOf(root, repo)); !os.IsNotExist(err) {
		t.Fatalf("the Stop that compared README kept its removed line: %v", err)
	}
}

// The turn deletes the file of a stopped line: its entries go. The person writing the file again with that text later is not
// stopped for it.
func TestStoppedEntriesOfADeletedFileGo(t *testing.T) {
	repo, root, readme := uncommittedLinesRepo(t)
	stoppedOnce(t, repo, root, readme, "gone")
	nextRequest(t, repo, root, "gone")
	if err := os.Remove(readme); err != nil {
		t.Fatal(err)
	}
	if r := wordStop(t, repo, root, "gone", false); !r.Pass {
		t.Fatalf("premise: the turn that deleted README passes: %+v", r)
	}
	if stoppedRecordHas(t, root, repo, "README.md", turnForbiddenLine) {
		t.Fatal("the entries of a deleted file are kept")
	}
	writeFile(t, readme, "# project\n"+turnForbiddenLine+"\n") // the person, between the turns
	nextRequest(t, repo, root, "gone")
	writeFile(t, readme, "# project\n"+turnForbiddenLine+"\na clean line\n")
	if r := wordStop(t, repo, root, "gone", false); !r.Pass {
		t.Fatalf("the person's line in a file written again stopped the turn: %+v", r)
	}
}

// The sessions' pruning sets the time of the record of a repository that is there to now: once that repository is gone, its
// record stays until 7 days after it was last seen, and then goes.
func TestPruneCountsFromWhenTheRepositoryWasLastSeen(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.Mkdir(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	st := newStoppedTable(repo)
	st.add("README.md", newLineHasher(st.key).hash([]byte(turnForbiddenLine)), stoppedEntry{n: 1})
	p := stoppedLinesPath(root, repo)
	if err := writeAtomic(p, st.bytes(nil, keptLinesMax)); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-hookSessionTTL - time.Hour)
	if err := os.Chtimes(p, old, old); err != nil {
		t.Fatal(err)
	}
	pruneSessions(root)
	if info, err := os.Stat(p); err != nil || time.Since(info.ModTime()) > time.Hour {
		t.Fatalf("the pruning did not set the time of the record of a repository that is there: %v", err)
	}
	if err := os.Rename(repo, repo+".away"); err != nil {
		t.Fatal(err)
	}
	pruneSessions(root)
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("the record went though its repository was seen less than 7 days ago: %v", err)
	}
	if err := os.Chtimes(p, old, old); err != nil { // 7 days pass
		t.Fatal(err)
	}
	pruneSessions(root)
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Fatalf("the record of a repository not seen for 7 days is left: %v", err)
	}
}

// A record in the form of the earlier builds (no count in HEAD) is not used: the person's line stays theirs, and a Stop that
// stops a line writes the record in the current form over it.
func TestStoppedRecordInAnOlderFormIsNotUsed(t *testing.T) {
	repo, root, readme := uncommittedLinesRepo(t)
	p := stoppedRecordOf(root, repo)
	key := make([]byte, 32)
	d := newLineHasher(key).hash([]byte(turnForbiddenLine))
	older := "#stopped2 " + hex.EncodeToString([]byte(feedbackScope(repo))) + " " + hex.EncodeToString(key) + "\n" +
		hex.EncodeToString([]byte("README.md")) + " " + hex.EncodeToString(d[:]) + " 1\n"
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writeAtomic(p, []byte(older)); err != nil {
		t.Fatal(err)
	}
	writeFile(t, readme, "# project\n"+turnForbiddenLine+"\n") // the person's draft
	organizedRequest(t, repo, root, "older")
	writeFile(t, readme, "# project\n"+turnForbiddenLine+"\na clean line\n")
	if r := wordStop(t, repo, root, "older", false); !r.Pass {
		t.Fatalf("a record in an older form counted the person's line as added: %+v", r)
	}
	nextRequest(t, repo, root, "older")
	writeFile(t, readme, "# project\n"+turnForbiddenLine+"\na clean line\n"+turnForbiddenLine+"\n")
	if r := wordStop(t, repo, root, "older", false); !stoppedForTheLine(r) {
		t.Fatalf("premise: the turn's copy is refused: %+v", r)
	}
	if !strings.HasPrefix(string(mustRead(t, p)), stoppedMarker) || !stoppedRecordHas(t, root, repo, "README.md", turnForbiddenLine) {
		t.Fatalf("the record in an older form was not written over: %q", mustRead(t, p))
	}
}

// The same text written again after the stopped line was committed and then removed by a later commit (or a revert), with no
// Stop comparing README in between. The record knows a line only by the hash of its text, so the copy written again cannot be
// told from the stopped line: it is stopped, as in 0.30.7. Only the copy written again is stopped this way; the commit alone,
// while the commit at the turn's start holds the copy, clears it (TestCommittedStoppedLineIsForgotten).
func TestSameTextWrittenAgainAfterTheStoppedLineWasCommittedAndRemovedStops(t *testing.T) {
	repo, root, readme := uncommittedLinesRepo(t)
	stoppedOnce(t, repo, root, readme, "commit-remove-rewrite")
	gitCommitAll(t, repo, "docs: the person commits the stopped line")
	writeFile(t, readme, "# project\n")
	gitCommitAll(t, repo, "docs: the person removes it again")
	writeFile(t, readme, "# project\n"+turnForbiddenLine+"\n") // the same text written again, after the commits
	nextRequest(t, repo, root, "commit-remove-rewrite")
	writeFile(t, readme, "# project\n"+turnForbiddenLine+"\na clean line\n")
	if r := wordStop(t, repo, root, "commit-remove-rewrite", false); !stoppedForTheLine(r) {
		t.Fatalf("the same text written again after the stopped line was committed and removed passes: %+v", r)
	}
}

// The same text written again after the person removed the stopped line while no Stop compared README (a turn on another
// file ended in between). It cannot be told from the stopped line by the hash of its text: it is stopped, as in 0.30.7. Once a
// Stop compares README with the line removed, the text written again is the person's
// (TestSameTextWrittenBackAfterTheStoppedLineWasRemovedIsThePersons).
func TestSameTextWrittenAgainAfterARemovalNoStopComparedStops(t *testing.T) {
	repo, root, readme := uncommittedLinesRepo(t)
	stoppedOnce(t, repo, root, readme, "remove-unseen-rewrite")
	writeFile(t, readme, "# project\n") // the person removes it
	nextRequest(t, repo, root, "remove-unseen-rewrite")
	writeFile(t, filepath.Join(repo, "notes.md"), "a clean note\n")
	if r := wordStop(t, repo, root, "remove-unseen-rewrite", false); !r.Pass {
		t.Fatalf("premise: %+v", r)
	}
	writeFile(t, readme, "# project\n"+turnForbiddenLine+"\n") // the same text written again, later
	nextRequest(t, repo, root, "remove-unseen-rewrite")
	writeFile(t, readme, "# project\n"+turnForbiddenLine+"\na clean line\n")
	if r := wordStop(t, repo, root, "remove-unseen-rewrite", false); !stoppedForTheLine(r) {
		t.Fatalf("the same text written again after a removal no Stop compared passes: %+v", r)
	}
}

// The person had committed a copy of the line; the turn wrote one more and was stopped (at a HEAD holding one copy). The
// person moves HEAD back (git reset --soft HEAD~1), a turn changing README ends (its Stop writes the record again, at a start
// HEAD holding no copy), and the person commits one copy again (their own). The turn's copy is still stopped at the next turn:
// the entry is counted from the HEAD it was stopped at, never from an older one.
func TestStoppedCopyStaysAfterHeadMovedBackAndThePersonsCopyIsCommittedAgain(t *testing.T) {
	repo, root, readme := uncommittedLinesRepo(t)
	one := "# project\n" + turnForbiddenLine + "\n"
	writeFile(t, readme, one)
	gitCommitAll(t, repo, "docs: the person's own line")
	organizedRequest(t, repo, root, "reset-soft")
	writeFile(t, readme, one+turnForbiddenLine+"\n")
	if r := wordStop(t, repo, root, "reset-soft", false); !stoppedForTheLine(r) {
		t.Fatalf("premise: %+v", r)
	}
	wordStop(t, repo, root, "reset-soft", true)
	gitIn(t, repo, "reset", "--soft", "HEAD~1")
	nextRequest(t, repo, root, "reset-soft")
	writeFile(t, readme, one+turnForbiddenLine+"\na clean line\n")
	if r := wordStop(t, repo, root, "reset-soft", false); !stoppedForTheLine(r) {
		t.Fatalf("premise: after HEAD moved back the turn's copy still stops: %+v", r)
	}
	wordStop(t, repo, root, "reset-soft", true)
	// the person commits one copy (their own) and keeps the rest uncommitted
	writeFile(t, readme, one)
	gitCommitAll(t, repo, "docs: the person's own line again")
	writeFile(t, readme, one+turnForbiddenLine+"\na clean line\n")
	nextRequest(t, repo, root, "reset-soft")
	writeFile(t, readme, one+turnForbiddenLine+"\na clean line\nanother clean line\n")
	if r := wordStop(t, repo, root, "reset-soft", false); !stoppedForTheLine(r) {
		t.Fatalf("the turn's stopped copy passes once the person committed their own copy again: %+v", r)
	}
}
