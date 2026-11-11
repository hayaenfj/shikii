package main

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// The record of stopped lines near its limit and between a Stop's comparison and its write (guard_stopped.go): a kept entry
// that changes is written with what this Stop worked out, a Stop that writes a record near its limit stays near the comparison
// with HEAD, the record read for the comparison is used again only while it is the same record, and the entries kept before are
// written in the order they were read.

// stoppedRecordKeys gives the path and the hash of each entry line of a record, in the order they are written.
func stoppedRecordKeys(b []byte) []string {
	_, body, _ := strings.Cut(string(b), "\n")
	var keys []string
	for _, line := range strings.Split(strings.TrimSuffix(body, "\n"), "\n") {
		if f := strings.Fields(line); len(f) == 4 {
			keys = append(keys, f[0]+" "+f[1])
		}
	}
	return keys
}

// stoppedReadBack writes st within max bytes and reads it back: its entries are then entries kept before. size is the bytes
// of the record written.
func stoppedReadBack(t *testing.T, st *stoppedTable, max int) (read *stoppedTable, size int) {
	t.Helper()
	b := st.bytes(max)
	p := filepath.Join(t.TempDir(), "read-back.stopped")
	if err := writeAtomic(p, b); err != nil {
		t.Fatal(err)
	}
	if read = readStoppedTable(p, st.scope); read == nil {
		t.Fatalf("premise: the record written is read back: %q", b)
	}
	return read, len(b)
}

// stoppedRecordNearItsLimit is a record of the repository scope near stoppedLinesMax: many stopped lines in 200 files that
// nobody fixed yet.
func stoppedRecordNearItsLimit(t *testing.T, scope string) []byte {
	t.Helper()
	st := newStoppedTable(scope)
	h := newLineHasher(st.key)
	for i, size := 0, 0; size < stoppedLinesMax-64<<10; i++ {
		p := fmt.Sprintf("data/f%03d.txt", i%200)
		st.add(p, h.hash([]byte(fmt.Sprint("FORBIDDENWORD ", i))), stoppedEntry{n: 1})
		size += 2*len(p) + len(" ") + 32 + 2*len(" ") + 2*stoppedCountDigits + len("\n")
	}
	b := st.bytes(stoppedLinesMax)
	if len(b) < stoppedLinesMax-128<<10 || len(b) > stoppedLinesMax {
		t.Fatalf("premise: the record is near its limit: %d bytes", len(b))
	}
	return b
}

// A record at its limit; the person commits one copy of a stopped line and removes most of the others, and the next Stop
// comparing the file counts one copy stopped against the new HEAD ({1 10}). That entry is written as worked out, never as the
// line it had before ({9 9}): the person's copies written later stay theirs.
func TestKeptEntryChangedAtTheLimitDoesNotStopThePersonsLaterCopies(t *testing.T) {
	const scope = "/a/repository"
	st := newStoppedTable(scope)
	h := newLineHasher(st.key)
	line := "a line with FORBIDDENWORD"
	a := h.hash([]byte(line))
	pad := h.hash([]byte("pad"))
	// an earlier Stop: the start's HEAD held 9 copies, and 9 copies were stopped
	st.add("a.md", a, stoppedEntry{n: 9, head: 9})
	st.add("z.md", pad, stoppedEntry{n: 1})
	st, limit := stoppedReadBack(t, st, stoppedLinesMax) // the record read fills its limit exactly
	// the person committed one copy (HEAD holds 10) and removed seven (the file holds 11); this Stop compares a.md
	now := []byte(strings.Repeat(line+"\n", 11))
	head := []byte(strings.Repeat(line+"\n", 10))
	st.update("a.md", now, nil, func() []byte { return head })
	if e := st.files["a.md"][a]; e != (stoppedEntry{1, 10}) {
		t.Fatalf("premise: the update works out {1 10}, got %+v", e)
	}
	b := st.bytes(limit)
	if len(b) != limit {
		t.Fatalf("the record is %d bytes, not the %d of the record read", len(b), limit)
	}
	p := filepath.Join(t.TempDir(), "r.stopped")
	if err := writeAtomic(p, b); err != nil {
		t.Fatal(err)
	}
	got := readStoppedTable(p, scope)
	if got == nil || got.files["z.md"][pad].n != 1 {
		t.Fatalf("another file's kept entry was dropped: %q", b)
	}
	// a later turn: HEAD still holds 10 copies, and the person wrote 8 more copies of the text (the file holds 19)
	later := []byte(strings.Repeat(line+"\n", 19))
	if n := got.stoppedIn("a.md", later, func() []byte { return head })[line]; n != 1 {
		t.Fatalf("one copy is still stopped, got %d (the entry written is %+v: %d of the person's copies counted as stopped)",
			n, got.files["a.md"][a], n-1)
	}
}

// A record near its limit; the Stop stops a line and writes the record. It stays close to the comparison with the request's
// HEAD of the same file, in the same run (the faster of three runs of each, so a pause of the machine in one run does not
// decide).
func TestStopThatWritesARecordNearItsLimitStaysNearTheComparisonWithHead(t *testing.T) {
	repo, root, readme := uncommittedLinesRepo(t)
	top := feedbackScope(repo)
	b := stoppedRecordNearItsLimit(t, top)
	p := stoppedLinesPath(root, top)
	head := gitHead(repo)
	_, listing, _, _, _ := treeStateWithScope(repo)
	before := listingHeader("req") + listing
	g := guard{ID: "no-word", Kind: guardKindForbiddenWords, Words: []string{"FORBIDDENWORD"}}
	var withRecord, withHead time.Duration
	for i := 0; i < 3; i++ {
		if err := writeAtomic(p, b); err != nil {
			t.Fatal(err)
		}
		writeFile(t, readme, fmt.Sprintf("# project\n%s %d\n", turnForbiddenLine, i))
		_, now, _, _, _ := treeStateWithScope(repo)
		changed := changedFiles(before, now)
		reads, done := countStoppedReads(t)
		start := time.Now()
		in := guardTurnInputStopped(root, repo, before, nil, changed, true, head, time.Time{})
		keepStoppedLines(root, []guard{g}, in, head)
		d := time.Since(start)
		done()
		if *reads != 1 {
			t.Fatalf("the Stop read the record %d times, want once", *reads)
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
		if string(mustRead(t, p)) == string(b) {
			t.Fatal("premise: the Stop wrote the record")
		}
	}
	t.Logf("record %d bytes, a Stop that writes it: with the record %v, compared with HEAD %v (ratio %.2f)",
		len(b), withRecord, withHead, float64(withRecord)/float64(withHead))
	if !withinRecordBudget(withRecord, withHead) {
		t.Fatalf("the Stop that writes a record near its limit takes %v, more than %v (the comparison with HEAD takes %v)", withRecord, recordBudget(withHead), withHead)
	}
}

// Between this Stop's comparison and its lock, the record is written again in place with other entries of the same size and
// its time is set back (as a file system with times in seconds and a reused file number can leave it): the same file, size and
// time. The Stop does not use what its comparison read: it reads the record again and keeps the entry written since.
func TestStopReadsTheRecordAgainWhenItsContentChangedWithTheSameSizeAndTime(t *testing.T) {
	repo, root, readme := uncommittedLinesRepo(t)
	top := feedbackScope(repo)
	st := newStoppedTable(top)
	h := newLineHasher(st.key)
	first, second := "the first line FORBIDDENWORD", "the other line FORBIDDENWORD"
	st.add("a.md", h.hash([]byte(first)), stoppedEntry{n: 1})
	p := stoppedLinesPath(root, top)
	if err := writeAtomic(p, st.bytes(stoppedLinesMax)); err != nil {
		t.Fatal(err)
	}
	since := newStoppedTable(top)
	since.key = st.key
	since.add("b.md", h.hash([]byte(second)), stoppedEntry{n: 1})
	replacement := since.bytes(stoppedLinesMax)
	head := gitHead(repo)
	_, listing, _, _, _ := treeStateWithScope(repo)
	before := listingHeader("req") + listing
	writeFile(t, readme, "# project\n"+turnForbiddenLine+"\n")
	_, now, _, _, _ := treeStateWithScope(repo)
	in := guardTurnInputStopped(root, repo, before, nil, changedFiles(before, now), true, head, time.Time{})
	replaced := false
	stoppedLinesSeam = func(step string) {
		if step != "before-lock" || replaced {
			return
		}
		replaced = true
		info, err := os.Stat(p)
		if err != nil || info.Size() != int64(len(replacement)) {
			t.Errorf("premise: the replacement has the size of the record: %v", err)
			return
		}
		if err := os.WriteFile(p, replacement, 0o600); err != nil {
			t.Error(err)
		}
		if err := os.Chtimes(p, info.ModTime(), info.ModTime()); err != nil {
			t.Error(err)
		}
	}
	defer func() { stoppedLinesSeam = nil }()
	g := guard{ID: "no-word", Kind: guardKindForbiddenWords, Words: []string{"FORBIDDENWORD"}}
	keepStoppedLines(root, []guard{g}, in, head)
	stoppedLinesSeam = nil
	if !replaced {
		t.Fatal("premise: the record was replaced between the comparison and the lock")
	}
	got := readStoppedTable(p, top)
	if got == nil || got.files["b.md"][h.hash([]byte(second))].n != 1 || got.files["README.md"][h.hash([]byte(turnForbiddenLine))].n != 1 {
		t.Fatalf("the Stop wrote what its comparison read over the record written since: %q", mustRead(t, p))
	}
	if len(got.files["a.md"]) != 0 {
		t.Fatalf("the entry of the record as the comparison read it came back: %q", mustRead(t, p))
	}
}

// The entries kept before are written in the order they were read, whatever their paths, and the new ones after them.
func TestStopWritesTheEntriesKeptBeforeInTheOrderTheyWereRead(t *testing.T) {
	repo, root, readme := uncommittedLinesRepo(t)
	top := feedbackScope(repo)
	st := newStoppedTable(top)
	h := newLineHasher(st.key)
	for i, f := range []string{"a.md", "m.md", "z.md", "docs/b.md"} {
		for j := 0; j < 3; j++ {
			st.add(f, h.hash([]byte(fmt.Sprint("FORBIDDENWORD ", i, j))), stoppedEntry{n: 1 + j})
		}
	}
	b := st.bytes(stoppedLinesMax)
	// the same record with its entry lines in the reverse order
	header, body, _ := strings.Cut(string(b), "\n")
	lines := strings.Split(strings.TrimSuffix(body, "\n"), "\n")
	slices.Reverse(lines)
	reversed := header + "\n" + strings.Join(lines, "\n") + "\n"
	p := stoppedLinesPath(root, top)
	if err := writeAtomic(p, []byte(reversed)); err != nil {
		t.Fatal(err)
	}
	want := stoppedRecordKeys([]byte(reversed))
	if len(want) != 12 || slices.IsSorted(want) {
		t.Fatalf("premise: twelve entries, not in the order of their paths: %q", want)
	}
	head := gitHead(repo)
	_, listing, _, _, _ := treeStateWithScope(repo)
	before := listingHeader("req") + listing
	writeFile(t, readme, "# project\n"+turnForbiddenLine+"\n")
	_, now, _, _, _ := treeStateWithScope(repo)
	in := guardTurnInputStopped(root, repo, before, nil, changedFiles(before, now), true, head, time.Time{})
	keepStoppedLines(root, []guard{{ID: "no-word", Kind: guardKindForbiddenWords, Words: []string{"FORBIDDENWORD"}}}, in, head)
	got := stoppedRecordKeys(mustRead(t, p))
	if len(got) != 13 || !slices.Equal(got[:12], want) {
		t.Fatalf("the entries kept before are not in the order they were read:\n got %q\nwant %q", got, want)
	}
	if d := h.hash([]byte(turnForbiddenLine)); !strings.HasSuffix(got[12], fmt.Sprintf("%x", d[:])) {
		t.Fatalf("the new entry is not after the entries kept before: %q", got)
	}
}

// A count past the width of the record (stoppedCountDigits) is written as the width's maximum and read back as unbounded: the
// copies counted as stopped after reading it back are never fewer than with the true counts. Every count takes the same width,
// and a count within it is read back as it was.
func TestStoppedCountPastItsWidthLeansToStopping(t *testing.T) {
	st := newStoppedTable("/a/repository")
	h := newLineHasher(st.key)
	cases := map[string]stoppedEntry{
		"n":     {n: stoppedCountMax + 5, head: 3},
		"head":  {n: 2, head: stoppedCountMax + 100},
		"both":  {n: stoppedCountMax + 5, head: stoppedCountMax + 100},
		"exact": {n: stoppedCountMax, head: stoppedCountMax},
		"small": {n: 7, head: 12},
	}
	for name, e := range cases {
		st.add(name+".md", h.hash([]byte(name)), e)
	}
	b := st.bytes(stoppedLinesMax)
	_, body, _ := strings.Cut(string(b), "\n")
	for _, line := range strings.Split(strings.TrimSuffix(body, "\n"), "\n") {
		if f := strings.Fields(line); len(f) != 4 || len(f[2]) != stoppedCountDigits || len(f[3]) != stoppedCountDigits {
			t.Fatalf("a count is not written in %d digits: %q", stoppedCountDigits, line)
		}
	}
	p := filepath.Join(t.TempDir(), "r.stopped")
	if err := writeAtomic(p, b); err != nil {
		t.Fatal(err)
	}
	got := readStoppedTable(p, "/a/repository")
	if got == nil {
		t.Fatalf("the record is not read back: %q", b)
	}
	if r := got.files["small.md"][h.hash([]byte("small"))]; r != cases["small"] {
		t.Fatalf("a count within the width is read back as %+v, want %+v", r, cases["small"])
	}
	const m = stoppedCountMax
	for name, e := range cases {
		r := got.files[name+".md"][h.hash([]byte(name))]
		for _, head := range []int{0, 3, 12, m, m + 50, m + 1000, 2 * m} {
			for _, extra := range []int{0, 1, 7, m + 10} {
				now := head + extra
				if r.left(now, head) < e.left(now, head) {
					t.Fatalf("%s: read back as %+v, it counts %d copies stopped for %d in the file and %d in HEAD, fewer than the %d of the true counts %+v",
						name, r, r.left(now, head), now, head, e.left(now, head), e)
				}
			}
		}
	}
}

// recordStopExtra is what a record near its limit may add to a Stop when that is more than 3 times the comparison with HEAD: where
// git is fast (Linux, a few milliseconds for a small repository), reading and writing 4 MB once takes several times as long as the
// comparison itself, yet only some milliseconds.
const recordStopExtra = 50 * time.Millisecond

// recordBudget is how long a Stop with a record may take: 3 times the comparison with HEAD, or that comparison and recordStopExtra,
// whichever is longer.
func recordBudget(withHead time.Duration) time.Duration {
	return max(3*withHead, withHead+recordStopExtra)
}

func withinRecordBudget(withRecord, withHead time.Duration) bool {
	return withRecord <= recordBudget(withHead)
}
