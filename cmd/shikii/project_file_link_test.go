package main

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// project.json is read without following a link in its name and without waiting on a FIFO, by every reader of it: a link there
// names no version and no scope.
func TestProjectFileReadersDoNotFollowALink(t *testing.T) {
	project := t.TempDir()
	store := filepath.Join(project, ".shikii")
	if err := os.MkdirAll(store, 0o700); err != nil {
		t.Fatal(err)
	}
	link := func(scope string) {
		t.Helper()
		_ = os.Remove(filepath.Join(store, "project.json"))
		if err := os.WriteFile(filepath.Join(store, "other.json"), []byte(`{"version":1,"scope":"`+scope+`"}`), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink("other.json", filepath.Join(store, "project.json")); err != nil {
			t.Fatal(err)
		}
	}
	link(project)
	if v := projectInitVersion(store); v != 0 {
		t.Errorf("projectInitVersion read the file behind a link: version %d", v)
	}
	if err := os.Remove(filepath.Join(store, "project.json")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(store, "project.json"), []byte(`{"version":2,"scope":"`+project+`"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if v := projectInitVersion(store); v != 2 {
		t.Errorf("a regular project.json was not read: version %d", v)
	}
	// canonicalScope answers only for the same folder by another spelling, which needs a disk that does not tell case apart
	other := strings.Replace(project, "Test", "TEST", 1)
	if !sameFolder(other, project) {
		t.Log("the disk tells the spellings apart: canonicalScope is not checked here")
		return
	}
	link(other)
	if got := canonicalScope(project); got != project {
		t.Errorf("canonicalScope read the file behind a link: %q", got)
	}
}

// A FIFO named project.json is not waited on.
func TestProjectFileReadersDoNotWaitOnAFIFO(t *testing.T) {
	project := t.TempDir()
	store := filepath.Join(project, ".shikii")
	if err := os.MkdirAll(store, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(filepath.Join(store, "project.json"), 0o600); err != nil {
		t.Skip("no FIFO here:", err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = projectInitVersion(store)
		_ = canonicalScope(project)
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("a reader of project.json waits on a FIFO")
	}
}
