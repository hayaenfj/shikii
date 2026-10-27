package store

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// 0.9.9 (.shikii/handoff/plan-0.9.9/triage.md, C): the store's files are written and read in a way that works on every disk and with what a
// .shikii brought in from elsewhere holds.

// confirm1-1: on a disk without hard links (exFAT, FAT: link fails with "operation not supported") records/store-name was never written,
// so the name kept following the path, and after a move the conversations written at the old path were no longer found.
func TestPatch099StoreNameOnADiskWithoutHardLinks(t *testing.T) {
	Link = func(string, string) error { return &os.LinkError{Op: "link", Err: syscall.ENOTSUP} }
	t.Cleanup(func() { Link = os.Link })
	dir := t.TempDir()
	s := Store{Dir: dir}
	if err := s.AppendFeedback("/work/old", Feedback{ID: "0000000000000abc", Request: "fix the readme", Summary: "fixed", Reply: "thanks"}); err != nil {
		t.Fatal(err)
	}
	recorded, _ := os.ReadFile(filepath.Join(dir, "records", "store-name"))
	RecordStoreNameFrom(dir, "/work/old")
	got, err := s.RecentFeedback("/work/new")
	if err != nil || len(got) != 1 {
		t.Fatalf("the name is written the first time even without hard links, and kept after a move: store-name=%q, %d conversations (%v)", recorded, len(got), err)
	}
}

// BC-3 = confirm1-3: readStoreName read records/store-name with os.ReadFile, so a FIFO there (a .shikii brought in from elsewhere) kept
// `shikii wiki query` waiting forever before --init. It is read like store-id: no links, no blocking, a size limit, a regular file only.
func TestPatch099StoreNameThatIsAFIFODoesNotBlock(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "records"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(filepath.Join(dir, "records", "store-name"), 0o600); err != nil {
		t.Skip("no FIFO here:", err)
	}
	done := make(chan string, 1)
	go func() { done <- StoreName(dir, "/work/p") }()
	select {
	case name := <-done:
		if name != scopeKey("/work/p") {
			t.Fatalf("a FIFO is not a name; the path's name is used: %q", name)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("reading a FIFO at records/store-name blocks")
	}
}

// Confirmation u-0: nothing checked that a name file another process is writing just now is not replaced (on a disk without hard links
// PlaceOnce makes it first and writes it then).
func TestPatch099StoreNameBeingWrittenIsNotReplaced(t *testing.T) {
	Link = func(string, string) error { return &os.LinkError{Op: "link", Err: syscall.ENOTSUP} }
	t.Cleanup(func() { Link = os.Link })
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "records"), 0o700)
	path := filepath.Join(dir, "records", "store-name")
	a, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600) // A: made, not yet written
	if err != nil {
		t.Fatal(err)
	}
	first := scopeKey("/work/first")
	StoreName(dir, "/work/second") // B, in between
	a.WriteString(first + "\n")
	a.Close()
	if got := readStoreName(dir); got != first {
		t.Fatalf("the first writer's name stays: %q", got)
	}
}
