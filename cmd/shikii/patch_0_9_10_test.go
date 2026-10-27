package main

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/store"
)

var builtInSpec = []byte("name: needs-plan\nquestion: Should the plan be agreed first?\n")

// seedCase writes text at path with the given mode and age, and the in-place write mark beside it when mark is not zero.
func seedCase(t *testing.T, path, text string, mode os.FileMode, age, mark time.Time) {
	t.Helper()
	if err := os.WriteFile(path, []byte(text), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, age, age); err != nil {
		t.Fatal(err)
	}
	if !mark.IsZero() {
		if err := os.WriteFile(store.PlacingMark(path), nil, 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(store.PlacingMark(path), mark, mark); err != nil {
			t.Fatal(err)
		}
	}
}

// A suspicion the 0.9.9 audit left: on a disk without hard links a built-in file is written in place, so a stop in the middle left a part
// of it, and every later init kept that part. Only a write that left its mark (PlaceOnce's in-place write) is finished again; a file the
// person shortened or emptied has no mark and stays (0.9.10 audit a-1).
func TestPatch0910OnlyAStoppedInPlaceWriteIsFinished(t *testing.T) {
	old, now := time.Now().Add(-time.Hour), time.Now()
	for _, c := range []struct {
		name, text string
		mark       time.Time
		want       string
		created    bool
	}{
		{"a part left by a stopped write", "name: needs-pl", old, string(builtInSpec), true},
		{"an empty file left by a stopped write", "", old, string(builtInSpec), true},
		{"a part another init is writing now", "name: needs-pl", now, "name: needs-pl", false},
		{"a file the person shortened", "name: needs-plan\n", time.Time{}, "name: needs-plan\n", false},
		{"a file the person emptied", "", time.Time{}, "", false},
		{"a whole file with a mark left after the write", string(builtInSpec), old, string(builtInSpec), false},
		{"the person's text after a stopped write", "name: mine\n", old, "name: mine\n", false},
	} {
		t.Run(c.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "needs-plan.yaml")
			seedCase(t, path, c.text, 0o644, old, c.mark)
			created, err := seedFile(path, builtInSpec)
			if err != nil {
				t.Fatal(err)
			}
			if b, _ := os.ReadFile(path); string(b) != c.want || created != c.created {
				t.Fatalf("got %q (created %v), want %q (created %v)", b, created, c.want, c.created)
			}
			if _, err := os.Lstat(store.PlacingMark(path)); c.mark == old && !os.IsNotExist(err) {
				t.Fatalf("an old mark is removed once the file is checked: %v", err)
			}
		})
	}
}

// 0.9.10 audit u-1: the file written again keeps the mode it had (the person may have narrowed it).
func TestPatch0910FinishedFileKeepsItsMode(t *testing.T) {
	old := time.Now().Add(-time.Hour)
	path := filepath.Join(t.TempDir(), "needs-plan.yaml")
	seedCase(t, path, "name: needs-pl", 0o600, old, old)
	if _, err := seedFile(path, builtInSpec); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("the mode stays 0600: %v %v", info.Mode(), err)
	}
}

// 0.9.10 audit u-2: a folder that cannot be written (a read-only disk) does not stop init or the hook; the part stays as it was.
func TestPatch0910UnwritableFolderDoesNotStop(t *testing.T) {
	old := time.Now().Add(-time.Hour)
	dir := t.TempDir()
	path := filepath.Join(dir, "needs-plan.yaml")
	seedCase(t, path, "", 0o644, old, old)
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0o755) })
	if _, err := seedFile(path, builtInSpec); err != nil {
		t.Fatalf("an unwritable folder does not stop it: %v", err)
	}
}

// The whole path on a disk without hard links: a write stopped in the middle leaves its part and its mark, and the next seed after the
// mark is old finishes it.
func TestPatch0910StoppedWriteOnADiskWithoutHardLinksIsFinished(t *testing.T) {
	store.Link = func(string, string) error { return &os.LinkError{Op: "link", Err: syscall.ENOTSUP} }
	store.WriteInPlace = func(f *os.File, b []byte) (int, error) {
		n, _ := f.Write(b[:len(b)/2])
		return n, syscall.EINTR
	}
	t.Cleanup(func() {
		store.Link, store.WriteInPlace = os.Link, func(f *os.File, b []byte) (int, error) { return f.Write(b) }
	})
	path := filepath.Join(t.TempDir(), "needs-plan.yaml")
	if _, err := seedFile(path, builtInSpec); err == nil {
		t.Fatal("the stopped write is an error")
	}
	if _, err := os.Lstat(store.PlacingMark(path)); err != nil {
		t.Fatalf("a stopped write leaves its mark: %v", err)
	}
	store.WriteInPlace = func(f *os.File, b []byte) (int, error) { return f.Write(b) }
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(store.PlacingMark(path), old, old); err != nil {
		t.Fatal(err)
	}
	if created, err := seedFile(path, builtInSpec); err != nil || !created {
		t.Fatalf("the part is finished: %v %v", created, err)
	}
	if b, _ := os.ReadFile(path); string(b) != string(builtInSpec) {
		t.Fatalf("got %q", b)
	}
	if created, err := seedFile(filepath.Join(filepath.Dir(path), "other.yaml"), builtInSpec); err != nil || !created {
		t.Fatalf("a whole write works: %v %v", created, err)
	}
	if _, err := os.Lstat(store.PlacingMark(filepath.Join(filepath.Dir(path), "other.yaml"))); !os.IsNotExist(err) {
		t.Fatalf("a whole write leaves no mark: %v", err)
	}
}
