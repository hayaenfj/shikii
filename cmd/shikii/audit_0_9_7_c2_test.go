package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// 0.9.7 re-audit C-6: a page whose model answered first (the usual case), stopped by the check, returned "use of closed network connection".
func TestReauditCOwnerStopAfterModelReadyReturnsError(t *testing.T) {
	hookEnv(t, nil)
	common, _ := agentHome()
	repo := gitRepo(t)
	initAt(t, repo)
	old := pageOwnerCheck
	pageOwnerCheck = 50 * time.Millisecond
	t.Cleanup(func() { pageOwnerCheck = old })
	done := make(chan error, 1)
	go func() {
		done <- cmdServe([]string{"--store", filepath.Join(repo, ".shikii"), "--port", "0", "--local", ""})
	}()
	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, ok := readPageRecord(common, repo); ok {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("precondition: the page started")
		}
		time.Sleep(20 * time.Millisecond)
	}
	time.Sleep(20 * time.Millisecond)
	if err := os.Remove(pagePath(common, repo)); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("a page stopped by the check ends with an error: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("it does not stop")
	}
}
