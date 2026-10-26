package main

import (
	"net"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"
)

// takePageLock holds the project's page lock the way a running page does (from another open file, as another process would).
func takePageLock(t *testing.T, common, scope string) {
	t.Helper()
	path := filepath.Join(common, "pages", projectID(scope)+".lock")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.Close() })
}

// 0.9.7: a second page of the same project exits while the first holds the page lock, even when the first answered /v1/status too slowly
// for the hook's probe (two pages per project were found running after the 0.9.6 swap). It neither listens nor writes the page record.
func TestSecondPageOfTheSameProjectExitsWhileTheFirstHoldsTheLock(t *testing.T) {
	hookEnv(t, nil)
	common, _ := agentHome()
	repo := gitRepo(t)
	initAt(t, repo)
	takePageLock(t, common, repo)
	done := make(chan error, 1)
	go func() {
		done <- cmdServe([]string{"--store", filepath.Join(repo, ".shikii"), "--port", "0", "--local", "http://127.0.0.1:1"})
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("a second page exits quietly: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("a second page of the same project kept running while the first held the page lock")
	}
	if rec, ok := readPageRecord(common, repo); ok {
		t.Fatalf("the second page must not overwrite the first page's record: %+v", rec)
	}
	if len(opened) != 0 {
		t.Fatalf("the second page must not open the browser: %v", opened)
	}
}

// A recorded page whose pid is alive and which holds the page lock is up even when /v1/status does not answer within the hook's probe
// (a busy page); the hook then does not start another one.
func TestPageHoldingTheLockIsUpEvenWhenItAnswersSlowly(t *testing.T) {
	hookEnv(t, nil)
	common, _ := agentHome()
	repo := gitRepo(t)
	initAt(t, repo)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close() // accepts (the kernel backlog) but never answers
	port := ln.Addr().(*net.TCPAddr).Port
	if err := writePageRecord(common, repo, port, os.Getpid()); err != nil {
		t.Fatal(err)
	}
	if livePage(repo) != "" {
		t.Fatal("without the page lock, a page that does not answer is not up")
	}
	takePageLock(t, common, repo)
	if url := livePage(repo); url != "http://127.0.0.1:"+strconv.Itoa(port) {
		t.Fatalf("a slow page holding the lock is up: %q", url)
	}
	spawned = nil
	if _, started := ensureProjectPage(repo); started || len(spawned) != 0 {
		t.Fatalf("the hook does not start a second page: %v", spawned)
	}
}

// The page that holds the lock starts even when the old record's pid is alive (now another process): its own lock must not make that record look up.
func TestPageStartsWhenTheOldRecordsPidWasReused(t *testing.T) {
	hookEnv(t, nil)
	common, _ := agentHome()
	repo := gitRepo(t)
	initAt(t, repo)
	if err := writePageRecord(common, repo, 1, os.Getpid()); err != nil { // a live pid, a port that does not answer
		t.Fatal(err)
	}
	go cmdServe([]string{"--store", filepath.Join(repo, ".shikii"), "--port", "0", "--local", "http://127.0.0.1:1"})
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if rec, ok := readPageRecord(common, repo); ok && rec.Port != 1 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("the page did not start: the old record with a reused pid looked up through the page's own lock")
}
