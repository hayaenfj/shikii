package main

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

// stopWritingTheRecord runs the Stop of a turn that wrote body into README, through the comparison and the write of the record.
func stopWritingTheRecord(t *testing.T, repo, root, readme string, body string) {
	t.Helper()
	head := gitHead(repo)
	_, listing, _, _, _ := treeStateWithScope(repo)
	before := listingHeader("req") + listing
	writeFile(t, readme, body)
	_, now, _, _, _ := treeStateWithScope(repo)
	in := guardTurnInputStopped(root, repo, before, nil, changedFiles(before, now), true, head, time.Time{})
	keepStoppedLines(root, []guard{{ID: "no-word", Kind: guardKindForbiddenWords, Words: []string{"FORBIDDENWORD"}}}, in, head)
}

// A write of the record that fails (a full disk) leaves the record as it was:
// the lines stopped before still stop.
func TestFailedWriteOfTheStoppedRecordKeepsTheEntriesKeptBefore(t *testing.T) {
	repo, root, readme := uncommittedLinesRepo(t)
	top := feedbackScope(repo)
	st := newStoppedTable(top)
	h := newLineHasher(st.key)
	kept := "an earlier stopped line FORBIDDENWORD"
	st.add("a.md", h.hash([]byte(kept)), stoppedEntry{n: 1})
	p := stoppedLinesPath(root, top)
	if err := writeAtomic(p, st.bytes(stoppedLinesMax)); err != nil {
		t.Fatal(err)
	}
	failed := false
	writeStoppedRecord = func(string, []byte) error { failed = true; return errors.New("no space left on device") }
	t.Cleanup(func() { writeStoppedRecord = writeAtomic })
	stopWritingTheRecord(t, repo, root, readme, "# project\n"+turnForbiddenLine+"\n")
	writeStoppedRecord = writeAtomic
	if !failed {
		t.Fatal("premise: the Stop writes the record")
	}
	if got := readStoppedTable(p, top); got != nil && len(got.files["README.md"]) > 0 {
		t.Fatal("premise: the write of the newly stopped line fails")
	}
	got := readStoppedTable(p, top)
	if got == nil || got.files["a.md"][h.hash([]byte(kept))].n != 1 {
		_, err := os.Stat(p)
		t.Fatalf("a write that failed lost the entries kept before (record: %v)", err)
	}
}

// A record of exactly stoppedLinesMax bytes whose last line has no newline (cut off, or edited by hand): the line without its
// newline is not read, so writing the record back never makes it larger than it was, and it stays readable.
func TestRecordAtTheLimitWithoutTheLastNewlineStaysReadable(t *testing.T) {
	repo, root, readme := uncommittedLinesRepo(t)
	top := feedbackScope(repo)
	st := newStoppedTable(top)
	h := newLineHasher(st.key)
	header := len(stoppedMarker) + 2*len(top) + 1 + 2*len(st.key) + 1 + 2*stoppedGenBytes + 1
	rem := stoppedLinesMax + 1 - header // the entry lines, each ending in a newline
	if rem%2 != 0 {
		t.Fatalf("premise: an even room for the entries: %d", rem)
	}
	k, r := rem/60, rem%60
	for i := 0; i < k; i++ {
		p := "data"
		if i == 0 {
			p += strings.Repeat("x", r/2) // one line longer by r
		}
		st.add(p, h.hash([]byte(fmt.Sprint("FORBIDDENWORD ", i))), stoppedEntry{n: 1})
	}
	b := st.bytes(1 << 30)
	if len(b) != stoppedLinesMax+1 {
		t.Fatalf("premise: %d bytes, want %d", len(b), stoppedLinesMax+1)
	}
	b = b[:len(b)-1] // the last line without its newline: stoppedLinesMax bytes
	p := stoppedLinesPath(root, top)
	if err := writeAtomic(p, b); err != nil {
		t.Fatal(err)
	}
	if readStoppedTable(p, top) == nil {
		t.Fatal("premise: the record is read")
	}
	stopWritingTheRecord(t, repo, root, readme, "# project\n"+turnForbiddenLine+"\n")
	after := mustRead(t, p)
	got := readStoppedTable(p, top)
	if got == nil || len(got.files["data"]) == 0 {
		t.Fatalf("after the Stop wrote it, the record (%d bytes, limit %d) is no longer read: its entries kept before stop nothing", len(after), stoppedLinesMax)
	}
}

// The same entry twice in a record (edited by hand) adds up and is written back once: written twice, the next reading would add
// it up again and count the person's copies as stopped.
func TestStoppedRecordWritesADuplicatedEntryBackOnce(t *testing.T) {
	repo, root, readme := uncommittedLinesRepo(t)
	top := feedbackScope(repo)
	st := newStoppedTable(top)
	h := newLineHasher(st.key)
	kept := "an earlier stopped line FORBIDDENWORD"
	st.add("a.md", h.hash([]byte(kept)), stoppedEntry{n: 1})
	b := st.bytes(stoppedLinesMax)
	header := bytes.IndexByte(b, '\n') + 1
	b = append(b, b[header:]...) // the entry line twice
	p := stoppedLinesPath(root, top)
	if err := writeAtomic(p, b); err != nil {
		t.Fatal(err)
	}
	if got := readStoppedTable(p, top); got == nil || got.files["a.md"][h.hash([]byte(kept))].n != 2 {
		t.Fatalf("premise: the duplicated entry adds up to 2 copies: %+v", got)
	}
	stopWritingTheRecord(t, repo, root, readme, "# project\n"+turnForbiddenLine+"\n")
	after := mustRead(t, p)
	d := h.hash([]byte(kept))
	if n := bytes.Count(after, []byte(" "+hex.EncodeToString(d[:])+" ")); n != 1 {
		t.Fatalf("the duplicated entry was written back %d times, want once", n)
	}
	if got := readStoppedTable(p, top); got == nil || got.files["a.md"][h.hash([]byte(kept))].n != 2 {
		t.Fatalf("the duplicated entry does not read back as 2 copies: %+v", got)
	}
}
