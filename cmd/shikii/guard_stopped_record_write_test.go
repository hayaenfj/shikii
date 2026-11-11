package main

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// How a Stop reads and writes the record of stopped lines (guard_stopped.go): it reads the record once, uses that reading under
// the lock only while the record is the same file, unchanged, and past the limit it keeps the entries already kept; the
// pruning looks at a record's time again under its lock.

// countStoppedReads counts the records read back (readStoppedTable) until the returned function is called.
func countStoppedReads(t *testing.T) (reads *int, done func()) {
	t.Helper()
	n := 0
	stoppedLinesSeam = func(step string) {
		if step == "read" {
			n++
		}
	}
	return &n, func() { stoppedLinesSeam = nil }
}

// A record near its limit (many stopped lines in 200 files that nobody fixed yet). A later Stop whose turn changed one other
// file reads the record once and stays close to the comparison with the request's HEAD of the same file, in the same run, and
// leaves the record as it was.
func TestStopWithARecordNearItsLimitReadsItOnceAndStaysNearTheComparisonWithHead(t *testing.T) {
	repo, root, readme := uncommittedLinesRepo(t)
	top := feedbackScope(repo)
	st := newStoppedTable(top)
	h := newLineHasher(st.key)
	for i, size := 0, 0; size < keptLinesMax-200000; i++ {
		p := fmt.Sprintf("data/f%03d.txt", i%200)
		st.add(p, h.hash([]byte(fmt.Sprint("FORBIDDENWORD ", i))), stoppedEntry{n: 1})
		size += 2*len(p) + len(" ") + 32 + len(" 1 0\n")
	}
	b := st.bytes(nil, keptLinesMax)
	if len(b) < keptLinesMax-300000 || len(b) > keptLinesMax {
		t.Fatalf("premise: the record is near its limit: %d bytes", len(b))
	}
	p := stoppedLinesPath(root, top)
	if err := writeAtomic(p, b); err != nil {
		t.Fatal(err)
	}
	head := gitHead(repo)
	_, listing, listed, _, _ := treeStateWithScope(repo)
	if !listed {
		t.Fatal("premise: listed")
	}
	before := listingHeader("req") + listing
	writeFile(t, readme, "# project\na clean line\n")
	_, now, _, _, _ := treeStateWithScope(repo)
	changed := changedFiles(before, now)
	if len(changed) != 1 {
		t.Fatalf("premise: one changed file, got %v", changed)
	}
	// the faster of three runs of each, so a pause of the machine in one run does not decide
	var withRecord, withHead time.Duration
	for i := 0; i < 3; i++ {
		reads, done := countStoppedReads(t)
		start := time.Now()
		in := guardTurnInputStopped(root, repo, before, nil, changed, true, head, time.Time{})
		keepStoppedLines(root, nil, in, head)
		d := time.Since(start)
		done()
		if *reads != 1 && i == 0 {
			t.Errorf("the Stop read the record %d times, want once", *reads)
		}
		start = time.Now()
		guardTurnInputKept(repo, before, nil, changed, true, head, time.Time{})
		e := time.Since(start)
		if i == 0 || d < withRecord {
			withRecord = d
		}
		if i == 0 || e < withHead {
			withHead = e
		}
	}
	t.Logf("record %d bytes: with the record %v, compared with HEAD %v (ratio %.2f)", len(b), withRecord, withHead, float64(withRecord)/float64(withHead))
	if withRecord > 3*withHead {
		t.Fatalf("the Stop with a record near its limit takes %v, more than 3 times the comparison with HEAD (%v)", withRecord, withHead)
	}
	if got := mustRead(t, p); string(got) != string(b) {
		t.Fatal("the Stop changed the entries of files it did not compare")
	}
}

// A Stop through the hook that stops a line of a file with a stopped line already kept reads the record once, for its
// comparison and its write together.
func TestStopThroughTheHookReadsTheRecordOnce(t *testing.T) {
	repo, root, readme := uncommittedLinesRepo(t)
	stoppedOnce(t, repo, root, readme, "read-once")
	nextRequest(t, repo, root, "read-once")
	writeFile(t, readme, "# project\n"+turnForbiddenLine+"\na clean line\nthe second line with FORBIDDENWORD\n")
	reads, done := countStoppedReads(t)
	r := wordStop(t, repo, root, "read-once", false)
	done()
	if !stoppedForTheLine(r) {
		t.Fatalf("premise: the stopped line stops the turn: %+v", r)
	}
	if !stoppedRecordHas(t, root, repo, "README.md", "the second line with FORBIDDENWORD") {
		t.Fatal("premise: the Stop keeps the line it stopped now")
	}
	if *reads != 1 {
		t.Fatalf("the Stop read the record %d times, want once", *reads)
	}
}

// The record exists when this Stop compares; another session's Stop writes it after that comparison and before this Stop
// takes the lock. This Stop does not use what it read before: it reads the record again and keeps the other session's line.
func TestStopReadsTheRecordAgainWhenAnotherStopWroteItAfterTheComparison(t *testing.T) {
	const outerLine, innerLine = "the outer session wrote FORBIDDENWORD", "the inner session wrote FORBIDDENWORD"
	repo, root, readme := uncommittedLinesRepo(t)
	notes, other := filepath.Join(repo, "notes.md"), filepath.Join(repo, "other.md")
	writeFile(t, notes, "notes\n")
	writeFile(t, other, "other\n")
	gitCommitAll(t, repo, "notes")
	stoppedOnce(t, repo, root, readme, "earlier")
	organizedRequest(t, repo, root, "outer")
	organizedRequest(t, repo, root, "inner")
	writeFile(t, notes, "notes\n"+outerLine+"\n")
	p := stoppedRecordOf(root, repo)
	innerRan := false
	stoppedLinesSeam = func(step string) {
		if step != "before-lock" || innerRan {
			return
		}
		innerRan = true
		writeFile(t, other, "other\n"+innerLine+"\n")
		if out := stopHook(t, repo, "inner", false); !blocked(out) {
			t.Errorf("premise: the inner session's Stop is stopped for its line: %q", out)
		}
	}
	defer func() { stoppedLinesSeam = nil }()
	if out := stopHook(t, repo, "outer", false); !blocked(out) {
		t.Fatalf("premise: the outer session's Stop is stopped for its line: %q", out)
	}
	stoppedLinesSeam = nil
	if !innerRan {
		t.Fatal("premise: the inner Stop ran between the outer Stop's comparison and its write")
	}
	for file, line := range map[string]string{"README.md": turnForbiddenLine, "notes.md": outerLine, "other.md": innerLine} {
		if !stoppedRecordHas(t, root, repo, file, line) {
			t.Errorf("the record lost the stopped line of %s: %q", file, mustRead(t, p))
		}
	}
}

// A record at its upper limit; an entry already kept grows by one digit (9 copies -> 10). The entries already kept stay:
// another file's kept entry is not dropped for it, and the grown entry keeps the line it had while the new form does not fit.
func TestKeptEntryGrowingADigitAtTheLimitDropsNoOtherKeptEntry(t *testing.T) {
	st := newStoppedTable("/a/repository")
	h := newLineHasher(st.key)
	a, z := h.hash([]byte("a line")), h.hash([]byte("z line"))
	st.add("a.md", a, stoppedEntry{n: 9})
	st.add("z.md", z, stoppedEntry{n: 1})
	// the Stop looked at a.md only; z.md's entry was kept before
	old := map[string]map[[16]byte]stoppedEntry{"a.md": {a: {n: 9}}}
	limit := len(st.bytes(old, keptLinesMax)) // the record fills its limit exactly
	st.files["a.md"][a] = stoppedEntry{n: 10} // a later Stop stops one more copy of a kept line
	p := filepath.Join(t.TempDir(), "r.stopped")
	for _, c := range []struct {
		name  string
		limit int
		wantA int
	}{
		{"one byte short", limit, 9},
		{"room for the grown entry", limit + 1, 10},
	} {
		b := st.bytes(old, c.limit)
		if len(b) > c.limit {
			t.Fatalf("%s: the record is %d bytes, past %d", c.name, len(b), c.limit)
		}
		if err := writeAtomic(p, b); err != nil {
			t.Fatal(err)
		}
		got := readStoppedTable(p, "/a/repository")
		if got == nil || got.files["z.md"][z].n != 1 {
			t.Fatalf("%s: another file's kept entry was dropped when a kept entry grew by one byte: %q", c.name, b)
		}
		if n := got.files["a.md"][a].n; n != c.wantA {
			t.Fatalf("%s: the grown kept entry holds %d copies, want %d: %q", c.name, n, c.wantA, b)
		}
	}
}

// At the limit, a new entry that does not fit is dropped and no kept entry is: the kept entries come first, whatever their
// paths, and the new ones only fill what is left.
func TestNewEntryPastTheLimitIsDroppedBeforeAnyKeptEntry(t *testing.T) {
	st := newStoppedTable("/a/repository")
	h := newLineHasher(st.key)
	kept, fresh := h.hash([]byte("z line")), h.hash([]byte("a line"))
	st.add("z.md", kept, stoppedEntry{n: 1})
	old := map[string]map[[16]byte]stoppedEntry{"z.md": {kept: {n: 1}}, "a.md": {}} // a.md had no entry before
	limit := len(st.bytes(old, keptLinesMax))
	st.add("a.md", fresh, stoppedEntry{n: 1})
	b := st.bytes(old, limit)
	p := filepath.Join(t.TempDir(), "r.stopped")
	if err := writeAtomic(p, b); err != nil {
		t.Fatal(err)
	}
	got := readStoppedTable(p, "/a/repository")
	if got == nil || got.files["z.md"][kept].n != 1 || len(got.files["a.md"]) != 0 {
		t.Fatalf("past the limit a kept entry was dropped or a new one written over the limit: %q", b)
	}
}

// The sessions' pruning finds the repository of a record gone and the record older than the sessions are kept; before the
// pruning takes the record's lock, the repository is back and a Stop reads the record (setting its time to now). The pruning
// looks at the time again under the lock and keeps the record.
func TestPruneKeepsARecordAStopReadAfterThePruningLookedAtIt(t *testing.T) {
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
	old := time.Now().Add(-hookSessionTTL - time.Hour)
	if err := os.Chtimes(p, old, old); err != nil {
		t.Fatal(err)
	}
	away := repo + ".away"
	if err := os.Rename(repo, away); err != nil {
		t.Fatal(err)
	}
	back := false
	stoppedLinesSeam = func(step string) {
		if step != "prune-before-lock" || back {
			return
		}
		back = true
		if err := os.Rename(away, repo); err != nil {
			t.Error(err)
			return
		}
		keepStoppedLines(root, nil, guardInput{repo: top, Added: map[string][]string{}}, head) // a Stop that passes reads it
	}
	defer func() { stoppedLinesSeam = nil }()
	pruneSessions(root)
	stoppedLinesSeam = nil
	if !back {
		t.Fatal("premise: the pruning looked at the record of a repository that is gone, older than the sessions are kept")
	}
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("the pruning removed a record a Stop read after the pruning looked at its time: %v", err)
	}
}
