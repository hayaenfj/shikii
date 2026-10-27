package store

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

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
