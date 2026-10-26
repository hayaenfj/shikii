package main

import (
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// 0.9.7 re-audit C-1: a page started by hand with --port 0 and a store not named .shikii was stopped by the minute check, although its record pointed at it.
func TestReauditCHandServeWithOtherStoreNameStops(t *testing.T) {
	hookEnv(t, nil)
	repo := gitRepo(t)
	initAt(t, repo)
	other := filepath.Join(t.TempDir(), "data")
	if out, err := exec.Command("cp", "-R", filepath.Join(repo, ".shikii"), other).CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	old := pageOwnerCheck
	pageOwnerCheck = 50 * time.Millisecond
	t.Cleanup(func() { pageOwnerCheck = old })
	done := make(chan error, 1)
	go func() {
		done <- cmdServe([]string{"--store", other, "--port", "0", "--local", "http://127.0.0.1:1"})
	}()
	select {
	case err := <-done:
		t.Fatalf("a page started by hand stopped: %v", err)
	case <-time.After(1 * time.Second):
	}
}
