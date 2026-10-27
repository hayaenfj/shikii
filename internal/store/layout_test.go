package store

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// Final audit BC-3 (B1): records/store-name was created with O_EXCL and then written. A stop between the two (kill -9, power loss) or a
// failed write (a full disk) left an empty store-name, which was never read as a name nor written again, so the name followed the
// current path and changed with a move. The name is now written whole or not at all, and a file without a valid name is replaced.
func TestFinalBC3EmptyStoreNameLetsTheNameChangeAfterAMove(t *testing.T) {
	for _, empty := range []bool{false, true} { // false is the control: the name is written and kept
		t.Run(map[bool]string{false: "control", true: "empty-store-name"}[empty], func(t *testing.T) { finalBC3(t, empty) })
	}
}

func finalBC3(t *testing.T, empty bool) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "records"), 0o700); err != nil {
		t.Fatal(err)
	}
	// the state after O_EXCL created the file and the write did not happen
	if empty {
		if err := os.WriteFile(filepath.Join(dir, "records", "store-name"), nil, 0o600); err != nil {
			t.Fatal(err)
		}
		// left by a stop some time ago: a file younger than 10 seconds may be another process's, being written (audit of 0.9.9, finding 4)
		old := time.Now().Add(-time.Minute)
		if err := os.Chtimes(filepath.Join(dir, "records", "store-name"), old, old); err != nil {
			t.Fatal(err)
		}
	}
	s := Store{Dir: dir}
	if err := s.AppendFeedback("/work/old", Feedback{ID: "0000000000000abc", Request: "fix the readme", Summary: "fixed", Reply: "thanks"}); err != nil {
		t.Fatal(err)
	}
	// the folder is moved; the hook records the old path's name first, then the store is opened at the new path
	RecordStoreNameFrom(dir, "/work/old")
	got, err := s.RecentFeedback("/work/new")
	sum := sha256.Sum256([]byte("/work/old"))
	recorded, _ := os.ReadFile(filepath.Join(dir, "records", "store-name"))
	if err != nil || len(got) != 1 || strings.TrimSpace(string(recorded)) != hex.EncodeToString(sum[:]) {
		t.Fatalf("B1: the name is written once and kept after a move: %d conversations after the move (%v), store-name=%q", len(got), err, recorded)
	}
}

func withoutHardLinks(t *testing.T) {
	t.Helper()
	old := Link
	Link = func(string, string) error { return &os.LinkError{Op: "link", Err: syscall.ENOTSUP} }
	t.Cleanup(func() { Link = old })
}

// On a disk without hard links PlaceOnce wrote the file in place, so a stop in the middle left a part of it at the path, which every later
// init kept (0.9.9 audit). The whole file is now written to a temp file first and renamed into place: a stopped write leaves nothing there.
func TestPatch0910StoppedWriteWithoutHardLinksLeavesNothing(t *testing.T) {
	withoutHardLinks(t)
	old := WriteTemp
	t.Cleanup(func() { WriteTemp = old })
	WriteTemp = func(f *os.File, b []byte) (int, error) {
		n, _ := f.Write(b[:len(b)/2])
		return n, syscall.EINTR
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "needs-plan.yaml")
	if _, err := PlaceOnce(path, []byte("name: needs-plan\n"), 0o644); err == nil {
		t.Fatal("a stopped write is an error")
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("a stopped write leaves nothing in the folder: %v", entries)
	}
	WriteTemp = old
	if placed, err := PlaceOnce(path, []byte("name: needs-plan\n"), 0o644); err != nil || !placed {
		t.Fatalf("the next write places it: %v %v", placed, err)
	}
	if b, _ := os.ReadFile(path); string(b) != "name: needs-plan\n" {
		t.Fatalf("got %q", b)
	}
}

// Two writers at once on a disk without hard links (two inits making the store id) must not both place a file: the check that nothing is
// there and the rename happen while the folder is locked. Checked in one run, without waiting on a race: right before the rename the
// folder's lock cannot be taken from outside.
func TestPatch0910RenameHappensUnderTheFolderLock(t *testing.T) {
	withoutHardLinks(t)
	dir := t.TempDir()
	var tried error
	old := BeforeRename
	t.Cleanup(func() { BeforeRename = old })
	BeforeRename = func() {
		f, err := os.Open(dir)
		if err != nil {
			tried = err
			return
		}
		defer f.Close()
		tried = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if tried == nil {
			syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		}
	}
	if placed, err := PlaceOnce(filepath.Join(dir, "store-id"), []byte("a\n"), 0o600); err != nil || !placed {
		t.Fatal(placed, err)
	}
	if tried != syscall.EWOULDBLOCK {
		t.Fatalf("the folder is locked right before the rename: taking the lock from outside gave %v", tried)
	}
}

// A file already there when the lock is taken is kept, and the writer says it did not place one.
func TestPatch0910FileAlreadyThereWithoutHardLinksStays(t *testing.T) {
	withoutHardLinks(t)
	path := filepath.Join(t.TempDir(), "store-id")
	if err := os.WriteFile(path, []byte("b\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if placed, err := PlaceOnce(path, []byte("a\n"), 0o600); err != nil || placed {
		t.Fatalf("nothing is placed over it: %v %v", placed, err)
	}
	if b, _ := os.ReadFile(path); string(b) != "b\n" {
		t.Fatalf("got %q", b)
	}
}

// 0.9.10 confirmation audit a-2: a file named <path>.placing that shikii did not make is never removed.
func TestPatch0910OthersPlacingFileStays(t *testing.T) {
	withoutHardLinks(t)
	path := filepath.Join(t.TempDir(), "store-id")
	if err := os.WriteFile(path+".placing", []byte("notes of another tool\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if placed, err := PlaceOnce(path, []byte("id\n"), 0o600); err != nil || !placed {
		t.Fatal(placed, err)
	}
	if b, err := os.ReadFile(path + ".placing"); err != nil || string(b) != "notes of another tool\n" {
		t.Fatalf("a .placing file shikii did not make stays: %q %v", b, err)
	}
}

// 0.9.10 confirmation audit s-1: Linux's exFAT and FAT drivers refuse chmod (EPERM), and PlaceOnce stopped on its temp file's mode there,
// so init never worked on such a disk. Modes mean nothing there: a refused chmod is let pass.
func TestPatch0910RefusedChmodDoesNotStop(t *testing.T) {
	withoutHardLinks(t)
	old := ChmodTemp
	t.Cleanup(func() { ChmodTemp = old })
	ChmodTemp = func(*os.File, os.FileMode) error { return &os.PathError{Op: "chmod", Err: syscall.EPERM} }
	path := filepath.Join(t.TempDir(), "store-id")
	if placed, err := PlaceOnce(path, []byte("id\n"), 0o600); err != nil || !placed {
		t.Fatalf("a refused chmod does not stop it: %v %v", placed, err)
	}
}

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

// The name "project" was used only by 0.9.7 builds in development, never released; it is not read (backward compatibility is kept for
// released versions only, 2026-10-27). A folder of that name does not change the store's name.
func TestTheDevelopmentNameProjectIsNotUsed(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "wiki", "lessons", "project"), 0o700); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte("/work/p"))
	if got := StoreName(dir, "/work/p"); got != hex.EncodeToString(sum[:]) {
		t.Fatalf("the name is the hash of the path, not %q", got)
	}
}

// Only EPERM (Linux's exFAT and FAT refusing modes) is let pass: any other failure to set the temp file's mode stops PlaceOnce, and nothing
// is placed (0.9.10 final audit u-2).
func TestPatch0910OtherChmodFailureStops(t *testing.T) {
	withoutHardLinks(t)
	old := ChmodTemp
	t.Cleanup(func() { ChmodTemp = old })
	ChmodTemp = func(*os.File, os.FileMode) error { return &os.PathError{Op: "chmod", Err: syscall.EIO} }
	dir := t.TempDir()
	if placed, err := PlaceOnce(filepath.Join(dir, "store-id"), []byte("id\n"), 0o600); err == nil || placed {
		t.Fatalf("a chmod failure other than EPERM stops it: %v %v", placed, err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("nothing is left in the folder: %v", entries)
	}
}

// A network disk without hard links may not support locks (flock gives ENOTSUP or ENOLCK on some SMB mounts): PlaceOnce places the file
// without the lock instead of stopping init there (0.9.10 final audit, suspicion 1). The check that nothing is there still comes first.
func TestPatch0910FolderWithoutLocksStillPlaces(t *testing.T) {
	withoutHardLinks(t)
	old := FlockDir
	t.Cleanup(func() { FlockDir = old })
	for _, refusal := range []error{syscall.ENOTSUP, syscall.EOPNOTSUPP, syscall.ENOLCK} {
		FlockDir = func(int, int) error { return refusal }
		path := filepath.Join(t.TempDir(), "store-id")
		if placed, err := PlaceOnce(path, []byte("id\n"), 0o600); err != nil || !placed {
			t.Fatalf("%v: a folder without locks still gets the file: %v %v", refusal, placed, err)
		}
		if placed, err := PlaceOnce(path, []byte("other\n"), 0o600); err != nil || placed {
			t.Fatalf("%v: a file already there is kept: %v %v", refusal, placed, err)
		}
	}
	FlockDir = func(int, int) error { return syscall.EIO }
	if _, err := PlaceOnce(filepath.Join(t.TempDir(), "store-id"), []byte("id\n"), 0o600); err == nil {
		t.Fatal("any other lock failure stops it")
	}
}
