package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/notebook"
	"github.com/hayaenfj/shikii/internal/store"
)

// Two `shikii nap` that touch .shikii/nap-first at the same time (nap_first.go). The other run is played by the test, at the try it chooses
// (beforeNapFirstLockTry): no test here waits for a race, and none runs a loop to meet one. The findings are those of the second
// confirmation audit of 0.43.0: a conversation printed and read, and counted 0 times, because the other run held the lock when the
// first print had to add its line, or was rewriting the file when the run that counts read it.

// holdNapFirstLock takes the lock of .shikii/nap-first the way a run that changes the file does, as the other run.
func holdNapFirstLock(t *testing.T, dir string) (release func()) {
	t.Helper()
	f, err := os.OpenFile(filepath.Join(dir, napFirstLockName), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	return func() {
		syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		f.Close()
	}
}

// napFirstLockIsHeld reports whether the lock is held now, by trying to take it the way another run would.
func napFirstLockIsHeld(t *testing.T, dir string) bool {
	t.Helper()
	f, err := os.OpenFile(filepath.Join(dir, napFirstLockName), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) != nil {
		return true
	}
	syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	return false
}

// useNapFirstWait sets how long a run waits for the lock, and what the test does before each try; both are put back when the test ends.
func useNapFirstWait(t *testing.T, wait time.Duration, before func(attempt int)) {
	t.Helper()
	oldWait, oldBefore := napFirstLockWait, beforeNapFirstLockTry
	t.Cleanup(func() { napFirstLockWait, beforeNapFirstLockTry = oldWait, oldBefore })
	napFirstLockWait = wait
	if before != nil {
		beforeNapFirstLockTry = before
	}
}

func napLogShown(t *testing.T, st store.Store) int {
	t.Helper()
	shown := 0
	for _, row := range napLogRows(t, st) {
		shown += int(row["shown"].(float64))
	}
	return shown
}

// napTwoPrintedAndRead is the state in which the agent has been given conversations 1 (cited by the fixture's article) and 2 (cited by
// nobody), an article from 2 was saved since, and the agent has sent both ids: the next run moves the window and counts them.
func napTwoPrintedAndRead(t *testing.T) (store.Store, string) {
	t.Helper()
	st, scope, _, draft, _ := memoryEffectFixture(t)
	book := notebook.Book{Root: st.Dir, Scope: scope}
	saveConversation(t, st, scope, "0000000000000002", time.Now().Add(-time.Hour), "second")
	decodeNap(t)
	saveNapArticle(t, book, napDraft(draft, "Second article", "0000000000000002"))
	napSendSources(t, st, scope, hexIDList(1, 2)...)
	return st, scope
}

// The run that prints conversations for the first time adds their lines to .shikii/nap-first. When another run holds the lock at that
// moment, it waits for it, so the lines are written and the conversations are counted once the agent has read them (the first of the
// auditor's two tests: before, the run left the file as it was and nothing was ever counted).
func TestNapWaitsForTheLockOfNapFirstToAddTheLinesOfAFirstPrint(t *testing.T) {
	st, scope, _, draft, _ := memoryEffectFixture(t)
	book := notebook.Book{Root: st.Dir, Scope: scope}
	saveConversation(t, st, scope, "0000000000000002", time.Now().Add(-time.Hour), "second")
	r, err := runNapReport(st.Dir, scope)
	if err != nil {
		t.Fatal(err)
	}
	release := holdNapFirstLock(t, st.Dir)
	useNapFirstWait(t, time.Hour, func(attempt int) {
		if attempt == 1 {
			release() // the other run is done after the first try
		}
	})
	capture(t, func() error { return finishNap(st.Dir, r) })
	if got, want := readNapFirstFile(t, st), "0000000000000001 1\n0000000000000002 0\n"; got != want {
		t.Fatalf("the lines were added once the lock was free:\n got %q\nwant %q", got, want)
	}
	saveNapArticle(t, book, napDraft(draft, "Second article", "0000000000000002"))
	napSendSources(t, st, scope, hexIDList(1, 2)...)
	_, out := decodeNapOutput(t)
	requireLastNap(t, out, 2, 1, 1)
	if got := napLogShown(t, st); got != 2 {
		t.Fatalf("the log counts the 2 conversations once: %d %v", got, napLogRows(t, st))
	}
}

// The run that counts reads .shikii/nap-first only once it has the lock, and changes what it reads: a line another run added while it
// waited is still there afterwards (a read before the lock would write back a file without it).
func TestNapReadsNapFirstOnlyOnceItHasTheLockAndKeepsWhatAnotherRunAddedMeanwhile(t *testing.T) {
	st, _ := napTwoPrintedAndRead(t)
	before := readNapFirstFile(t, st)
	release := holdNapFirstLock(t, st.Dir)
	useNapFirstWait(t, time.Hour, func(attempt int) {
		if attempt == 1 { // the other run adds the line of a conversation it printed, and is done
			if err := os.WriteFile(napFirstPath(st.Dir), []byte(before+"0000000000000003 0\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			release()
		}
	})
	_, out := decodeNapOutput(t)
	requireLastNap(t, out, 2, 1, 1)
	if got := readNapFirstFile(t, st); got != "0000000000000003 0\n" {
		t.Fatalf("the counted lines are out and the line the other run added stays: %q", got)
	}
}

// The lock is held from the read to the swap, and a reader that takes no lock sees the old file whole until the swap: the new content is
// in the temp file and comes in by one rename (never an empty or half written .shikii/nap-first, the second of the auditor's tests).
func TestNapHoldsTheLockOfNapFirstUntilTheSwapAndNeverShowsAHalfWrittenFile(t *testing.T) {
	type seen struct {
		held     bool
		inPlace  string
		tmp      string
		happened int // how many swaps were made
	}
	watch := func(t *testing.T, st store.Store, s *seen) {
		old := beforeNapFirstSwap
		t.Cleanup(func() { beforeNapFirstSwap = old })
		beforeNapFirstSwap = func() {
			s.happened++
			s.held = napFirstLockIsHeld(t, st.Dir)
			s.inPlace = readNapFirstFile(t, st)
			b, _ := os.ReadFile(filepath.Join(st.Dir, napFirstTmpName))
			s.tmp = string(b)
		}
	}
	t.Run("a run that counts", func(t *testing.T) {
		st, _ := napTwoPrintedAndRead(t)
		var s seen
		watch(t, st, &s)
		decodeNapOutput(t)
		if s.happened != 1 || !s.held {
			t.Fatalf("the swap is made with the lock held: %+v", s)
		}
		if s.inPlace != "0000000000000001 1\n0000000000000002 0\n" || s.tmp != "" {
			t.Fatalf("until the swap the file is whole as it was, and the new content (nothing is left) is in the temp file: %+v", s)
		}
	})
	t.Run("a run that adds lines", func(t *testing.T) {
		st, scope, _, _, _ := memoryEffectFixture(t)
		decodeNap(t) // the file holds the line of conversation 1
		saveConversation(t, st, scope, "0000000000000003", time.Now().Add(-time.Minute), "third")
		var s seen
		watch(t, st, &s)
		decodeNap(t) // prints 1 again and 3 for the first time
		if s.happened != 1 || !s.held {
			t.Fatalf("the swap is made with the lock held: %+v", s)
		}
		if s.inPlace != "0000000000000001 1\n" || s.tmp != "0000000000000001 1\n0000000000000003 0\n" {
			t.Fatalf("until the swap the file is whole as it was, and the new content is in the temp file: %+v", s)
		}
	})
}

// A lock that stays held past the time to wait: the run that counts does not go on without it. The window stays where it is, the
// conversations print again, the error says so, nothing is counted; once the agent has sent their ids again a later run moves the
// window and counts them once.
func TestNapDoesNotMoveTheWindowNorCountWhenItCannotGetTheLockOfNapFirst(t *testing.T) {
	st, scope := napTwoPrintedAndRead(t)
	window := readNapWindowEndFile(t, st)
	first := readNapFirstFile(t, st)
	release := holdNapFirstLock(t, st.Dir)
	useNapFirstWait(t, 0, nil)
	r, _, err := napPrintedRun(t, st)
	if err == nil || !strings.Contains(err.Error(), "not moved past and not counted yet") || !strings.Contains(err.Error(), napFirstLockName) {
		t.Fatalf("the error says the conversations were not moved past and not counted yet: %v", err)
	}
	if got := readNapWindowEndFile(t, st); got != window {
		t.Fatalf("the window stays: %q, was %q", got, window)
	}
	if got := conversationIDs(r); fmt.Sprint(got) != hexIDs(1, 2) || r.ShownAgain != 2 || r.LastNap != nil {
		t.Fatalf("the 2 conversations are printed again, shown again, and nothing is counted: %v %d %v", got, r.ShownAgain, r.LastNap)
	}
	if rows := napLogRows(t, st); len(rows) != 0 || readNapFirstFile(t, st) != first {
		t.Fatalf("nothing is logged and the lines stay: %v %q", rows, readNapFirstFile(t, st))
	}
	release()
	napSendSources(t, st, scope, hexIDList(1, 2)...)
	_, out := decodeNapOutput(t)
	requireLastNap(t, out, 2, 1, 1)
	if got := napLogShown(t, st); got != 2 || readNapFirstFile(t, st) != "" {
		t.Fatalf("counted once, and the lines are out: %d %q", got, readNapFirstFile(t, st))
	}
}

// The run that prints for the first time and cannot get the lock to add the lines records nothing of this print (as if it had not
// printed): the next run prints the conversations again, adds their lines, and they are counted once read.
func TestNapRecordsNothingOfAPrintWhoseLinesCouldNotBeAddedAndPrintsItAgain(t *testing.T) {
	st, scope, _, draft, _ := memoryEffectFixture(t)
	book := notebook.Book{Root: st.Dir, Scope: scope}
	saveConversation(t, st, scope, "0000000000000002", time.Now().Add(-time.Hour), "second")
	release := holdNapFirstLock(t, st.Dir)
	useNapFirstWait(t, 0, nil)
	r, _, err := napPrintedRun(t, st)
	if len(r.Conversations) != 2 || err == nil || !strings.Contains(err.Error(), "nothing of this print is recorded") {
		t.Fatalf("the conversations are printed and the error says nothing of the print is recorded: %v %v", conversationIDs(r), err)
	}
	for _, name := range []string{napShownName, napFirstName} {
		if _, err := os.Lstat(filepath.Join(st.Dir, name)); !os.IsNotExist(err) {
			t.Fatalf("%s is not written: %v", name, err)
		}
	}
	release()
	useNapFirstWait(t, 2*time.Second, nil)
	again := decodeNap(t)
	if fmt.Sprint(conversationIDs(again)) != hexIDs(1, 2) {
		t.Fatalf("the next run prints them again: %v", conversationIDs(again))
	}
	saveNapArticle(t, book, napDraft(draft, "Second article", "0000000000000002"))
	napSendSources(t, st, scope, hexIDList(1, 2)...)
	_, out := decodeNapOutput(t)
	requireLastNap(t, out, 2, 1, 1)
}

// The temp file is written before the window moves: when it cannot be written (here a folder is where it goes) nothing has moved, the
// error says so, and a later run counts the conversations once and leaves no line of them behind.
func TestNapLeavesTheWindowWhereItIsWhenTheNewNapFirstCannotBeWritten(t *testing.T) {
	st, scope := napTwoPrintedAndRead(t)
	window, first := readNapWindowEndFile(t, st), readNapFirstFile(t, st)
	obstacle := filepath.Join(st.Dir, napFirstTmpName)
	if err := os.Mkdir(obstacle, 0o700); err != nil {
		t.Fatal(err)
	}
	_, _, err := napPrintedRun(t, st)
	if err == nil || !strings.Contains(err.Error(), "could not be moved past or counted") || !strings.Contains(err.Error(), napFirstTmpName) {
		t.Fatalf("the error says the conversations could not be moved past or counted: %v", err)
	}
	if got := readNapWindowEndFile(t, st); got != window || readNapFirstFile(t, st) != first || len(napLogRows(t, st)) != 0 {
		t.Fatalf("nothing moved: window %q (was %q), file %q (was %q), log %v", got, window, readNapFirstFile(t, st), first, napLogRows(t, st))
	}
	if err := os.Remove(obstacle); err != nil {
		t.Fatal(err)
	}
	napSendSources(t, st, scope, hexIDList(1, 2)...)
	_, out := decodeNapOutput(t)
	requireLastNap(t, out, 2, 1, 1)
	if got := readNapFirstFile(t, st); got != "" {
		t.Fatalf("the counted lines are out: %q", got)
	}
}

// A conversation that was counted leaves no line: when it is printed again (the window was set back) it is a first print with the state
// of now, and an article that cites it now does not count as caught a second time.
func TestNapDoesNotCountAConversationTwiceWhenItIsPrintedAgainAfterTheWindowWasSetBack(t *testing.T) {
	st, scope := napTwoPrintedAndRead(t)
	_, out := decodeNapOutput(t)
	requireLastNap(t, out, 2, 1, 1)
	if err := os.Remove(filepath.Join(st.Dir, napWindowEndName)); err != nil {
		t.Fatal(err)
	}
	again := decodeNap(t)
	if fmt.Sprint(conversationIDs(again)) != hexIDs(1, 2) {
		t.Fatalf("premise: the conversations are printed again: %v", conversationIDs(again))
	}
	napSendSources(t, st, scope, hexIDList(1, 2)...)
	_, out = decodeNapOutput(t)
	requireLastNap(t, out, 2, 0, 0) // both were cited when printed this time
	picked := 0
	for _, row := range napLogRows(t, st) {
		picked += int(row["picked_up"].(float64))
	}
	if picked != 1 {
		t.Fatalf("the article from conversation 2 is counted as caught once: %d %v", picked, napLogRows(t, st))
	}
}

// A rename over a link would replace the link itself: the destination is checked again right before the rename, and a symbolic link put
// there meanwhile is left as it is, with what it points at, and the temp file is not left behind.
func TestNapNeverSwapsNapFirstOverALinkThatCameInMeanwhile(t *testing.T) {
	st, _, _, _, _ := memoryEffectFixture(t)
	other := filepath.Join(t.TempDir(), "persons-file")
	if err := os.WriteFile(other, []byte(napPersonsFile), 0o600); err != nil {
		t.Fatal(err)
	}
	old := beforeNapFirstSwap
	t.Cleanup(func() { beforeNapFirstSwap = old })
	beforeNapFirstSwap = func() {
		if err := os.Symlink(other, napFirstPath(st.Dir)); err != nil {
			t.Fatal(err)
		}
	}
	err := addNapFirst(st.Dir, []napFirstEntry{{"0000000000000009", napWasUncited}})
	if err == nil || !strings.Contains(err.Error(), "remove") || !strings.Contains(err.Error(), napFirstName) {
		t.Fatalf("the error tells the person to remove .shikii/nap-first: %v", err)
	}
	requireFileHolds(t, other, napPersonsFile)
	if fi, err := os.Lstat(napFirstPath(st.Dir)); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("the link is still a link: %v %v", fi, err)
	}
	if _, err := os.Lstat(filepath.Join(st.Dir, napFirstTmpName)); !os.IsNotExist(err) {
		t.Fatalf("the temp file is not left behind: %v", err)
	}
}

// A lock file that is a link (symbolic or hard) is a file of the person's: it is not locked through, and the run that counts leaves the
// window where it is.
func TestNapRefusesALockFileThatIsALink(t *testing.T) {
	for _, kind := range []string{"a symbolic link", "a hard link"} {
		t.Run(kind, func(t *testing.T) {
			st, _ := napTwoPrintedAndRead(t)
			window := readNapWindowEndFile(t, st)
			path := filepath.Join(st.Dir, napFirstLockName)
			os.Remove(path) // made by the first print
			var other string
			if kind == "a hard link" {
				other = linkToAPersonsFile(t, path, napPersonsFile)
			} else {
				other = filepath.Join(t.TempDir(), "persons-file")
				if err := os.WriteFile(other, []byte(napPersonsFile), 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(other, path); err != nil {
					t.Fatal(err)
				}
			}
			_, _, err := napPrintedRun(t, st)
			if err == nil || !strings.Contains(err.Error(), "remove") || !strings.Contains(err.Error(), napFirstLockName) {
				t.Fatalf("the error tells the person to remove the lock file: %v", err)
			}
			requireFileHolds(t, other, napPersonsFile)
			if got := readNapWindowEndFile(t, st); got != window {
				t.Fatalf("the window stays: %q, was %q", got, window)
			}
		})
	}
}
