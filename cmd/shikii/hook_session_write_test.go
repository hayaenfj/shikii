package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A write that fails (here at the rename: a folder with content in the place of the file) leaves no temporary file next to it:
// a temporary file left there would stay after the file it was for is pruned.
func TestFailedAtomicWriteLeavesNoTemporaryFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "record")
	if err := os.MkdirAll(filepath.Join(p, "x"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := writeAtomic(p, []byte("data\n")); err == nil {
		t.Fatal("premise: the write fails")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Fatalf("a failed write left its temporary file: %s", e.Name())
		}
	}
}

// A link at the name a temporary file could take is not followed: the file it points to outside is not written, and what is
// written is a regular file in the place of p.
func TestAtomicWriteDoesNotFollowALinkAtTheTemporaryName(t *testing.T) {
	dir := t.TempDir()
	outside := filepath.Join(t.TempDir(), "persons-file.txt")
	if err := os.WriteFile(outside, []byte("the person's text\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "record")
	if err := os.Symlink(outside, fmt.Sprintf("%s.%d.tmp", p, os.Getpid())); err != nil {
		t.Fatal(err)
	}
	if err := writeAtomic(p, []byte("data\n")); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(outside); string(b) != "the person's text\n" {
		t.Fatalf("the file outside was overwritten through the link: %q", b)
	}
	info, err := os.Lstat(p)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		t.Fatalf("what was written is not a regular file of mode 0600: %v %v", info, err)
	}
}
