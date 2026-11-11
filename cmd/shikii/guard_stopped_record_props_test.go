package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The properties of the record of stopped lines kept once per repository (guard_stopped.go): it is written under its lock,
// it is the repository's and not the session's, entries go only when removed or committed, a Stop that passes keeps them,
// past its size it drops only what does not fit, and every way of reading the content at the start applies it.

func stoppedRecordHas(t *testing.T, root, repo, file, line string) bool {
	t.Helper()
	st := readStoppedTable(stoppedRecordOf(root, repo), feedbackScope(repo))
	return st != nil && st.files[file][newLineHasher(st.key).hash([]byte(line))].n > 0
}

func blocked(out string) bool { return strings.Contains(out, `"decision":"block"`) }

// The record is read, changed and written under its lock. Another session's Stop of the same repository that writes between
// this Stop's comparison and its write is kept, in either order, and the lock is held while the record is read and written.
func TestStoppedLinesAreWrittenUnderTheRecordsLock(t *testing.T) {
	const otherLine = "the other session wrote FORBIDDENWORD"
	for _, outerIsA := range []bool{true, false} {
		name := map[bool]string{true: "b writes inside a's Stop", false: "a writes inside b's Stop"}[outerIsA]
		t.Run(name, func(t *testing.T) {
			repo, root, readme := uncommittedLinesRepo(t)
			notes := filepath.Join(repo, "notes.md")
			writeFile(t, notes, "notes\n")
			gitCommitAll(t, repo, "notes")
			writeA := func() { writeFile(t, readme, "# project\n"+turnForbiddenLine+"\n") }
			writeB := func() { writeFile(t, notes, "notes\n"+otherLine+"\n") }
			organizedRequest(t, repo, root, "a")
			organizedRequest(t, repo, root, "b")
			outer, inner, writeOuter, writeInner := "a", "b", writeA, writeB
			if !outerIsA {
				outer, inner, writeOuter, writeInner = "b", "a", writeB, writeA
			}
			writeOuter()
			p := stoppedRecordOf(root, repo)
			lockHeld, innerRan := true, false
			stoppedLinesSeam = func(step string) {
				switch step {
				case "before-lock":
					if innerRan {
						return
					}
					innerRan = true
					writeInner()
					if out := stopHook(t, repo, inner, false); !blocked(out) {
						t.Errorf("premise: the inner session's Stop is stopped for its line: %q", out)
					}
				case "locked":
					if unlock, ok := lockPathWithin(p+".lock", 0, nil); ok {
						lockHeld = false
						unlock()
					}
				}
			}
			defer func() { stoppedLinesSeam = nil }()
			if out := stopHook(t, repo, outer, false); !blocked(out) {
				t.Fatalf("premise: the outer session's Stop is stopped for its line: %q", out)
			}
			stoppedLinesSeam = nil
			if !innerRan {
				t.Fatal("premise: the inner Stop ran between the outer Stop's comparison and its write")
			}
			if !lockHeld {
				t.Error("the record was read and written without holding its lock")
			}
			if !stoppedRecordHas(t, root, repo, "README.md", turnForbiddenLine) || !stoppedRecordHas(t, root, repo, "notes.md", otherLine) {
				t.Fatalf("a Stop's write lost the other session's stopped line: %q", mustRead(t, p))
			}
		})
	}
}

// When the record's lock cannot be taken, the Stop writes nothing and still stops the turn (the hook never waits forever).
func TestStopWithoutTheRecordsLockWritesNothingAndGoesOn(t *testing.T) {
	repo, root, readme := uncommittedLinesRepo(t)
	p := stoppedRecordOf(root, repo)
	unlock, ok := lockPathWithin(p+".lock", 0, nil)
	if !ok {
		t.Fatal("premise: the test holds the record's lock")
	}
	defer unlock()
	old := hookLockWait
	hookLockWait = 50 * time.Millisecond
	defer func() { hookLockWait = old }()
	organizedRequest(t, repo, root, "l")
	writeFile(t, readme, "# project\n"+turnForbiddenLine+"\n")
	if out := stopHook(t, repo, "l", false); !blocked(out) {
		t.Fatalf("the Stop without the record's lock does not stop the turn: %q", out)
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Fatalf("the record was written without its lock: %v", err)
	}
}

// One store shared by two repositories (worktrees of one project): the record is the repository's, so a Stop in the second
// keeps the first one's stopped line, and the first one's comparison still counts it as added.
func TestStoppedLinesOfARepositoryStayWhenAnotherRepositoryIsUsed(t *testing.T) {
	root := t.TempDir()
	writeGuard(t, root, guard{ID: "no-word", Kind: guardKindForbiddenWords, Title: "do not use the word", Words: []string{"FORBIDDENWORD"}})
	guards := lineGuards(root)
	one, two := gitRepo(t), gitRepo(t)
	for _, repo := range []string{one, two} {
		writeFile(t, filepath.Join(repo, "README.md"), "# project\n")
		gitCommitAll(t, repo, "init")
		writeFile(t, filepath.Join(repo, "README.md"), "# project\n"+turnForbiddenLine+"\n")
		keepStoppedLines(root, guards, guardInput{repo: repo, Added: map[string][]string{"README.md": {turnForbiddenLine}}}, gitHead(repo))
	}
	for _, repo := range []string{one, two} {
		if !stoppedRecordHas(t, root, repo, "README.md", turnForbiddenLine) {
			t.Fatalf("the stopped line of %s is gone after the other repository's Stop", filepath.Base(repo))
		}
	}
	gitIn(t, one, "add", "README.md") // the content at the start is a blob the repository has
	_, listing, listed, _, _ := treeStateWithScope(one)
	if !listed {
		t.Fatal("premise: listed")
	}
	writeFile(t, filepath.Join(one, "README.md"), "# project\n"+turnForbiddenLine+"\na clean line\n")
	in := guardTurnInputStopped(root, one, listingHeader("r")+listing, nil, []string{"README.md"}, true, gitHead(one), time.Time{})
	if got := in.Added["README.md"]; len(got) != 2 {
		t.Fatalf("the first repository's comparison does not count its stopped line as added: %q", got)
	}
}

// The comparison with HEAD (no kept table and no blob, and the start's HEAD not known, as for a request before the first
// commit) applies the record too: the turn committing its stopped line does not make it a line from before the start.
func TestComparisonWithHeadAppliesTheRecord(t *testing.T) {
	root := t.TempDir()
	writeGuard(t, root, guard{ID: "no-word", Kind: guardKindForbiddenWords, Title: "do not use the word", Words: []string{"FORBIDDENWORD"}})
	repo := gitRepo(t)
	readme := filepath.Join(repo, "README.md")
	writeFile(t, readme, "# project\n"+turnForbiddenLine+"\n")
	keepStoppedLines(root, lineGuards(root), guardInput{repo: repo, Added: map[string][]string{"README.md": {turnForbiddenLine}}}, "")
	gitCommitAll(t, repo, "the turn commits its line")
	writeFile(t, readme, "# project\n"+turnForbiddenLine+"\na clean line\n")
	before := listingHeader("r") + "100644 " + strings.Repeat("0", 40) + " 0\tREADME.md"
	in := guardTurnInputStopped(root, repo, before, nil, []string{"README.md"}, true, "", time.Time{})
	if got := in.Added["README.md"]; len(got) != 2 || got[0] != turnForbiddenLine {
		t.Fatalf("the comparison with HEAD does not count the stopped line as added: %q", got)
	}
	if plain := guardTurnInputKept(repo, before, nil, []string{"README.md"}, true, "", time.Time{}); len(plain.Added["README.md"]) != 1 {
		t.Fatalf("premise: without the record only the clean line is added: %q", plain.Added["README.md"])
	}
}

// The check before a publication applies the record: the next turn commits the stopped line with a clean one and pushes; the
// push is refused.
func TestPublicationRefusesAStoppedLineOfAnEarlierTurn(t *testing.T) {
	repo, root, readme := uncommittedLinesRepo(t)
	stoppedOnce(t, repo, root, readme, "p")
	nextRequest(t, repo, root, "p")
	writeFile(t, readme, "# project\n"+turnForbiddenLine+"\na clean line\n")
	gitCommitAll(t, repo, "docs: the storage layout")
	out := runUnreviewedHook(t, "tool-lesson", map[string]any{"session_id": "p", "cwd": repo, "tool_name": "Bash", "tool_input": map[string]any{"command": "git push origin HEAD"}})
	if !strings.Contains(out, `"permissionDecision":"deny"`) || !strings.Contains(out, "no-word") {
		t.Fatalf("the push of a stopped line committed by the next turn is not refused: %q", out)
	}
}

// The turn that commits its own stopped line is still stopped for it: only a commit made before the start clears it.
func TestStoppedLineCommittedByTheTurnStillStops(t *testing.T) {
	repo, root, readme := uncommittedLinesRepo(t)
	stoppedOnce(t, repo, root, readme, "c")
	nextRequest(t, repo, root, "c")
	writeFile(t, readme, "# project\n"+turnForbiddenLine+"\na clean line\n")
	gitCommitAll(t, repo, "docs: the turn commits it")
	if r := wordStop(t, repo, root, "c", false); !stoppedForTheLine(r) {
		t.Fatalf("the turn that commits its stopped line passes: %+v", r)
	}
	if !stoppedRecordHas(t, root, repo, "README.md", turnForbiddenLine) {
		t.Fatal("the turn's own commit cleared the stopped line")
	}
}

// A Stop that passes keeps the stopped lines still in their file and not committed: the turn changes another file and passes;
// the line stays recorded and stops the next turn that changes README.
func TestPassingStopKeepsAStoppedLineStillThere(t *testing.T) {
	repo, root, readme := uncommittedLinesRepo(t)
	stoppedOnce(t, repo, root, readme, "k")
	nextRequest(t, repo, root, "k")
	writeFile(t, filepath.Join(repo, "notes.md"), "a clean note\n")
	if r := wordStop(t, repo, root, "k", false); !r.Pass {
		t.Fatalf("premise: the turn changing another file passes: %+v", r)
	}
	if !stoppedRecordHas(t, root, repo, "README.md", turnForbiddenLine) {
		t.Fatal("a passing Stop dropped a stopped line still in its file")
	}
	nextRequest(t, repo, root, "k")
	writeFile(t, readme, "# project\n"+turnForbiddenLine+"\na clean line\n")
	if r := wordStop(t, repo, root, "k", false); !stoppedForTheLine(r) {
		t.Fatalf("the stopped line kept by a passing Stop passes later: %+v", r)
	}
}

// Once the turn removes the stopped line, the same text the person writes back later is the person's line.
func TestSameTextWrittenBackAfterTheStoppedLineWasRemovedIsThePersons(t *testing.T) {
	repo, root, readme := uncommittedLinesRepo(t)
	stoppedOnce(t, repo, root, readme, "w")
	nextRequest(t, repo, root, "w")
	writeFile(t, readme, "# project\nthe turn removed its line\n")
	if r := wordStop(t, repo, root, "w", false); !r.Pass {
		t.Fatalf("premise: the turn that removed the line passes: %+v", r)
	}
	writeFile(t, readme, "# project\nthe turn removed its line\n"+turnForbiddenLine+"\n") // the person, between the turns
	nextRequest(t, repo, root, "w")
	writeFile(t, readme, "# project\nthe turn removed its line\n"+turnForbiddenLine+"\na clean line\n")
	if r := wordStop(t, repo, root, "w", false); !r.Pass {
		t.Fatalf("the person's line with the text of a removed stopped line stopped the turn: %+v", r)
	}
}

// Negative, comparing with a blob the repository has: the person's staged draft in the file of a stopped line is not added.
func TestStagedDraftNextToAStoppedLineIsNotAdded(t *testing.T) {
	repo, root, readme := uncommittedLinesRepo(t, "FORBIDDENWORD", "NEWWORD")
	const draft = "the person's staged draft with NEWWORD"
	stoppedOnce(t, repo, root, readme, "d")
	writeFile(t, readme, "# project\n"+draft+"\n"+turnForbiddenLine+"\n") // the person, between the turns
	gitIn(t, repo, "add", "README.md")
	nextRequest(t, repo, root, "d")
	writeFile(t, readme, "# project\n"+draft+"\n"+turnForbiddenLine+"\na clean line\n")
	r := wordStop(t, repo, root, "d", false)
	if !stoppedForTheLine(r) {
		t.Fatalf("premise: the stopped line stops: %+v", r)
	}
	if strings.Contains(r.Reason, "NEWWORD") {
		t.Fatalf("the person's staged draft was counted as added: %+v", r)
	}
}

// Past the size of a record only what does not fit is dropped: the entries kept before stay, and of the new ones the files
// with the fewest come first.
func TestStoppedRecordPastItsSizeDropsOnlyWhatDoesNotFit(t *testing.T) {
	st := newStoppedTable("/a/repository")
	h := newLineHasher(st.key)
	one := stoppedEntry{n: 1}
	st.add("old.md", h.hash([]byte("old line")), one)
	// the files this write looked at: old.md with its entry, and the two files that had none before
	old := map[string]map[[16]byte]stoppedEntry{"old.md": {h.hash([]byte("old line")): one}, "README.md": {}, "data/big.txt": {}}
	st.add("README.md", h.hash([]byte(turnForbiddenLine)), one)
	for i := 0; i < 1000; i++ {
		st.add("data/big.txt", h.hash([]byte(fmt.Sprint("FORBIDDENWORD ", i))), one)
	}
	b := st.bytes(old, 4096)
	if b == nil || len(b) > 4096 {
		t.Fatalf("the record past its size is %d bytes", len(b))
	}
	p := filepath.Join(t.TempDir(), "r.stopped")
	if err := os.WriteFile(p, b, 0o600); err != nil {
		t.Fatal(err)
	}
	got := readStoppedTable(p, "/a/repository")
	if got == nil || got.files["old.md"][h.hash([]byte("old line"))] != one || got.files["README.md"][h.hash([]byte(turnForbiddenLine))] != one {
		t.Fatalf("the old entry or the small file's new entry was dropped: %+v", got)
	}
	if n := len(got.files["data/big.txt"]); n == 0 || n >= 1000 {
		t.Fatalf("the big file's new entries are kept up to the size, got %d", n)
	}
}

// A record whose repository is gone (moved or deleted) goes once it is older than the sessions are kept; the record of a
// repository still there stays, however old.
func TestRecordOfARepositoryThatIsGoneIsPruned(t *testing.T) {
	root := t.TempDir()
	there := t.TempDir()
	gone := filepath.Join(t.TempDir(), "moved-away")
	old := time.Now().Add(-hookSessionTTL - time.Hour)
	for _, scope := range []string{there, gone} {
		st := newStoppedTable(scope)
		st.add("README.md", newLineHasher(st.key).hash([]byte(turnForbiddenLine)), stoppedEntry{n: 1})
		p := stoppedLinesPath(root, scope)
		if err := writeAtomic(p, st.bytes(nil, keptLinesMax)); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(p, old, old); err != nil {
			t.Fatal(err)
		}
	}
	pruneSessions(root)
	if _, err := os.Stat(stoppedLinesPath(root, gone)); !os.IsNotExist(err) {
		t.Errorf("the record of a repository that is gone is left behind: %v", err)
	}
	if _, err := os.Stat(stoppedLinesPath(root, there)); err != nil {
		t.Errorf("the record of a repository still there was removed: %v", err)
	}
}
