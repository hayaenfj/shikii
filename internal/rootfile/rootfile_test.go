//go:build unix

package rootfile

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// os.Root.OpenFile follows a link in the last name even with O_NOFOLLOW (checked on go1.27.3 darwin/arm64), so a file inside the
// root could be read, locked or written through a link placed under another name. These tests pin that OpenNoFollow refuses it.

func newRoot(t *testing.T) (*os.Root, string) {
	t.Helper()
	dir := t.TempDir()
	r, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r.Close() })
	return r, dir
}

func writeFile(t *testing.T, path, text string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func refusedAsALink(t *testing.T, f *os.File, err error) {
	t.Helper()
	if err == nil {
		f.Close()
		t.Fatal("opened the name although it is a link; it must be refused")
	}
	if !errors.Is(err, syscall.ELOOP) {
		t.Fatalf("the refusal must wrap ELOOP (as os.OpenFile with O_NOFOLLOW does), got %v", err)
	}
	if os.IsNotExist(err) {
		t.Fatalf("the refusal must not read as \"not there yet\": %v", err)
	}
}

func TestOpenNoFollowRefusesToReadThroughALink(t *testing.T) {
	r, dir := newRoot(t)
	writeFile(t, filepath.Join(dir, "other.json"), "secret")
	if err := os.Symlink("other.json", filepath.Join(dir, "record.json")); err != nil {
		t.Fatal(err)
	}
	f, err := OpenNoFollow(r, "record.json", os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	refusedAsALink(t, f, err)
}

func TestOpenNoFollowRefusesToCreateOrWriteThroughALink(t *testing.T) {
	r, dir := newRoot(t)
	writeFile(t, filepath.Join(dir, "other"), "kept")
	if err := os.Symlink("other", filepath.Join(dir, "x.lock")); err != nil {
		t.Fatal(err)
	}
	f, err := OpenNoFollow(r, "x.lock", os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0o600)
	refusedAsALink(t, f, err)
	if got := readFile(t, filepath.Join(dir, "other")); got != "kept" {
		t.Fatalf("the link's target changed: %q", got)
	}
	// A link that points at nothing yet: opening with create must not make the file it points at.
	if err := os.Symlink("made-through-the-link", filepath.Join(dir, "y.lock")); err != nil {
		t.Fatal(err)
	}
	f, err = OpenNoFollow(r, "y.lock", os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0o600)
	refusedAsALink(t, f, err)
	if _, err := os.Lstat(filepath.Join(dir, "made-through-the-link")); !os.IsNotExist(err) {
		t.Fatalf("the file the dangling link points at was created: %v", err)
	}
}

func TestOpenNoFollowOpensAnOrdinaryFile(t *testing.T) {
	r, dir := newRoot(t)
	writeFile(t, filepath.Join(dir, "record.json"), "{}")
	f, err := OpenNoFollow(r, "record.json", os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	b, err := io.ReadAll(f)
	if err != nil || string(b) != "{}" {
		t.Fatalf("read %q, %v", b, err)
	}
}

func TestOpenNoFollowCreatesAMissingFileAndKeepsNotThere(t *testing.T) {
	r, dir := newRoot(t)
	f, err := OpenNoFollow(r, "new.lock", os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	if info, err := os.Lstat(filepath.Join(dir, "new.lock")); err != nil || !info.Mode().IsRegular() {
		t.Fatalf("the missing file was not created as an ordinary file: %v", err)
	}
	// Without create, a missing name still reads as "not there" (callers use os.IsNotExist).
	_, err = OpenNoFollow(r, "missing.json", os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if !os.IsNotExist(err) {
		t.Fatalf("a missing name must read as not there, got %v", err)
	}
}

// The name is swapped for a link after it was checked and before it is opened (the seam runs there once; nothing is repeated to
// wait for a race). The check after opening must refuse what was opened.
func TestOpenNoFollowRefusesANameSwappedForALinkBeforeOpening(t *testing.T) {
	r, dir := newRoot(t)
	writeFile(t, filepath.Join(dir, "record.json"), "mine")
	writeFile(t, filepath.Join(dir, "other.json"), "secret")
	swap := func(name string) func() {
		return func() {
			if err := os.Remove(filepath.Join(dir, name)); err != nil && !os.IsNotExist(err) {
				t.Error(err)
			}
			if err := os.Symlink("other.json", filepath.Join(dir, name)); err != nil {
				t.Error(err)
			}
		}
	}
	t.Cleanup(func() { betweenCheckAndOpen = nil })

	betweenCheckAndOpen = swap("record.json")
	f, err := OpenNoFollow(r, "record.json", os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	refusedAsALink(t, f, err)

	// The name was not there when checked, so only the check after opening can see that create went through a link.
	betweenCheckAndOpen = swap("new.lock")
	f, err = OpenNoFollow(r, "new.lock", os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0o600)
	refusedAsALink(t, f, err)
	if got := readFile(t, filepath.Join(dir, "other.json")); got != "secret" {
		t.Fatalf("the link's target changed: %q", got)
	}
}

// A record another writer replaces by rename after this open and before the check after it is still the regular file that was at
// the name when it was checked: it is read, not refused (session records are rewritten by rename while other hooks read them).
func TestARecordReplacedByRenameAfterTheOpenIsStillRead(t *testing.T) {
	r, dir := newRoot(t)
	writeFile(t, filepath.Join(dir, "record.json"), "first\n")
	t.Cleanup(func() { betweenOpenAndCheck = nil })
	betweenOpenAndCheck = func() {
		betweenOpenAndCheck = nil
		writeFile(t, filepath.Join(dir, "record.json.new"), "second\n")
		if err := os.Rename(filepath.Join(dir, "record.json.new"), filepath.Join(dir, "record.json")); err != nil {
			t.Fatal(err)
		}
	}
	f, err := OpenNoFollow(r, "record.json", os.O_RDONLY, 0)
	if err != nil {
		t.Fatalf("a record replaced by rename after the open was refused: %v", err)
	}
	defer f.Close()
	if b, _ := io.ReadAll(f); string(b) != "first\n" {
		t.Fatalf("read %q, want the file that was at the name when it was checked", b)
	}
}
