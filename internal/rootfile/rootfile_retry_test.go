//go:build unix

package rootfile

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// These tests change the name in the seams on the first attempt only (once), so what they pin is how OpenNoFollow answers a name
// that changed under it, not how often a race happens.

func renameOver(t *testing.T, dir, name, text string) {
	t.Helper()
	tmp := filepath.Join(dir, name+".tmp")
	writeFile(t, tmp, text)
	if err := os.Rename(tmp, filepath.Join(dir, name)); err != nil {
		t.Fatal(err)
	}
}

// once runs f the first time it is called only, so a seam changes the name on the first attempt and the attempts after it see the
// name as it is.
func once(f func()) func() {
	done := false
	return func() {
		if !done {
			done = true
			f()
		}
	}
}

func clearSeams(t *testing.T) {
	t.Cleanup(func() { betweenCheckAndOpen, betweenOpenAndCheck = nil, nil })
}

// Two writers each rewrite a record by rename, one before the open and one after it, and no link is anywhere: the opened file is
// neither the one checked before nor the one at the name after, so the open is tried again and the record now at the name is read.
func TestARecordRewrittenByRenameBeforeAndAfterTheOpenIsRead(t *testing.T) {
	r, dir := newRoot(t)
	writeFile(t, filepath.Join(dir, "record.json"), "first\n")
	clearSeams(t)
	betweenCheckAndOpen = once(func() { renameOver(t, dir, "record.json", "second\n") })
	betweenOpenAndCheck = once(func() { renameOver(t, dir, "record.json", "third\n") })
	f, err := OpenNoFollow(r, "record.json", os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		t.Fatalf("a regular file rewritten twice by rename (no link) was refused: %v", err)
	}
	defer f.Close()
	if b, _ := io.ReadAll(f); string(b) != "third\n" {
		t.Fatalf("read %q, want the record at the name when it was tried again", b)
	}
}

// One writer rewrites a record by rename before the open and another removes it after: no link is there, so the open must not be
// refused as one; tried again, the name is gone, which reads as "not there" as before.
func TestARecordRewrittenThenRemovedReadsAsNotThere(t *testing.T) {
	r, dir := newRoot(t)
	writeFile(t, filepath.Join(dir, "record.json"), "first\n")
	clearSeams(t)
	betweenCheckAndOpen = once(func() { renameOver(t, dir, "record.json", "second\n") })
	betweenOpenAndCheck = once(func() {
		if err := os.Remove(filepath.Join(dir, "record.json")); err != nil {
			t.Fatal(err)
		}
	})
	f, err := OpenNoFollow(r, "record.json", os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err == nil {
		f.Close()
		t.Fatal("opened a record that was removed before the open was tried again")
	}
	if errors.Is(err, syscall.ELOOP) || !os.IsNotExist(err) {
		t.Fatalf("a record rewritten then removed (no link) must read as not there, got %v", err)
	}
}

// A name that is a different regular file on every attempt is refused after a fixed number of attempts, not tried forever.
func TestANameThatChangesOnEveryAttemptIsRefusedAfterAFixedNumberOfAttempts(t *testing.T) {
	r, dir := newRoot(t)
	writeFile(t, filepath.Join(dir, "record.json"), "0\n")
	clearSeams(t)
	tries := 0
	betweenCheckAndOpen = func() { tries++; renameOver(t, dir, "record.json", "before\n") }
	betweenOpenAndCheck = func() { renameOver(t, dir, "record.json", "after\n") }
	f, err := OpenNoFollow(r, "record.json", os.O_RDONLY, 0)
	refusedAsALink(t, f, err)
	if tries != 8 {
		t.Fatalf("tried %d times, want exactly 8", tries)
	}
}

// Creating a name that is swapped for a dangling link after the check must not make the file the link points at, whether the name
// was missing (create) or a regular file (open) when it was checked.
func TestCreateThroughADanglingLinkSwappedInMakesNoFile(t *testing.T) {
	for _, existed := range []bool{false, true} {
		t.Run(map[bool]string{false: "the name was missing", true: "the name was a file"}[existed], func(t *testing.T) {
			r, dir := newRoot(t)
			if existed {
				writeFile(t, filepath.Join(dir, "new.lock"), "")
			}
			clearSeams(t)
			betweenCheckAndOpen = once(func() {
				if err := os.Remove(filepath.Join(dir, "new.lock")); err != nil && !os.IsNotExist(err) {
					t.Fatal(err)
				}
				if err := os.Symlink("made.json", filepath.Join(dir, "new.lock")); err != nil {
					t.Fatal(err)
				}
			})
			f, err := OpenNoFollow(r, "new.lock", os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0o600)
			refusedAsALink(t, f, err)
			if _, err := os.Lstat(filepath.Join(dir, "made.json")); !os.IsNotExist(err) {
				t.Fatalf("refused, but the file the link points at was created: %v", err)
			}
		})
	}
}

// Another writer makes the missing name between the check and the open: create opens the file it made (as O_CREATE without O_EXCL
// does), and does not fail because the name now exists.
func TestCreateOpensTheFileAnotherWriterMadeAfterTheCheck(t *testing.T) {
	r, dir := newRoot(t)
	clearSeams(t)
	betweenCheckAndOpen = once(func() { writeFile(t, filepath.Join(dir, "x.lock"), "theirs") })
	f, err := OpenNoFollow(r, "x.lock", os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0o600)
	if err != nil {
		t.Fatalf("create failed on a name another writer made: %v", err)
	}
	defer f.Close()
	if b, _ := io.ReadAll(f); string(b) != "theirs" {
		t.Fatalf("read %q, want the file the other writer made", b)
	}
}

// The name is removed between the check and the open: create still makes it (as O_CREATE does), not "not there".
func TestCreateMakesANameRemovedAfterTheCheck(t *testing.T) {
	r, dir := newRoot(t)
	writeFile(t, filepath.Join(dir, "x.lock"), "old")
	clearSeams(t)
	betweenCheckAndOpen = once(func() {
		if err := os.Remove(filepath.Join(dir, "x.lock")); err != nil {
			t.Fatal(err)
		}
	})
	f, err := OpenNoFollow(r, "x.lock", os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0o600)
	if err != nil {
		t.Fatalf("create failed on a name removed after the check: %v", err)
	}
	defer f.Close()
	opened, _ := f.Stat()
	there, err := os.Lstat(filepath.Join(dir, "x.lock"))
	if err != nil || !there.Mode().IsRegular() || !os.SameFile(opened, there) || there.Size() != 0 {
		t.Fatalf("the opened file is not a new regular file at the name: %v", err)
	}
}

// O_CREATE|O_EXCL keeps its meaning: an existing name fails as existing and is left as it was.
func TestExclusiveCreateStillFailsOnAnExistingName(t *testing.T) {
	r, dir := newRoot(t)
	writeFile(t, filepath.Join(dir, "x.lock"), "kept")
	f, err := OpenNoFollow(r, "x.lock", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err == nil {
		f.Close()
		t.Fatal("create-exclusive opened an existing name")
	}
	if !errors.Is(err, fs.ErrExist) {
		t.Fatalf("create-exclusive on an existing name must fail as existing, got %v", err)
	}
	if got := readFile(t, filepath.Join(dir, "x.lock")); got != "kept" {
		t.Fatalf("the existing file changed: %q", got)
	}
}
