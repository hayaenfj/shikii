package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func full1Copy(t *testing.T, from, to string) {
	t.Helper()
	if out, err := exec.Command("cp", "-R", from, to).CombinedOutput(); err != nil {
		t.Fatal(err, string(out))
	}
}

// C2/C4 in a folder that is not a Git repository: shikii --init in a copy makes it a project of its own (a new store id) as it does in a
// Git repository.
func TestFull1InitInNonGitCopyGetsItsOwnStoreID(t *testing.T) {
	for _, git := range []bool{true, false} {
		p := t.TempDir()
		if git {
			p = gitRepo(t)
		}
		hookEnv(t, nil, p)
		P := feedbackScope(p)
		idP := storeID(filepath.Join(P, ".shikii"), false)
		q := filepath.Join(t.TempDir(), "copy")
		full1Copy(t, P, q)
		Q := feedbackScope(q)
		t.Chdir(Q)
		out := capture(t, func() error { return cmdInit(nil) })
		if active, _ := projectActive(filepath.Join(Q, ".shikii"), Q); !active {
			t.Fatalf("git=%v: precondition: --init makes the copy active", git)
		}
		if storeID(filepath.Join(Q, ".shikii"), false) == idP {
			t.Errorf("git=%v: C4: the copy taken over with --init keeps the original's store id (two active projects share one store id)\n%s", git, out)
		}
	}
}

// C1 for --init in a folder that is not a Git repository: the person's --init on a moved folder forgets the old path's entry, as in Git.
func TestFull1InitInNonGitMovedFolderForgetsOldEntry(t *testing.T) {
	for _, git := range []bool{true, false} {
		p := t.TempDir()
		if git {
			p = gitRepo(t)
		}
		hookEnv(t, nil, p)
		common, _ := agentHome()
		P := feedbackScope(p)
		r := filepath.Join(t.TempDir(), "moved")
		if err := os.Rename(P, r); err != nil {
			t.Fatal(err)
		}
		R := feedbackScope(r)
		t.Chdir(R)
		out := capture(t, func() error { return cmdInit(nil) })
		if !strings.Contains(out, "Reconnected") {
			t.Errorf("git=%v: --init on a moved folder does not reconnect it:\n%s", git, out)
		}
		if _, err := os.Stat(filepath.Join(common, "projects", projectID(P)+".json")); err == nil {
			t.Errorf("git=%v: the old path's index entry is left", git)
		}
	}
}

// C1 "say it once": two hooks of the same moved folder at the same time (a prompt and a subagent, two agents).
func TestFull1ConcurrentRelinkTellsOnce(t *testing.T) {
	for round := 0; round < 10; round++ {
		p := gitRepo(t)
		hookEnv(t, nil, p)
		P := feedbackScope(p)
		r := filepath.Join(t.TempDir(), "moved")
		if err := os.Rename(P, r); err != nil {
			t.Fatal(err)
		}
		R := feedbackScope(r)
		var mu sync.Mutex
		told := 0
		var wg sync.WaitGroup
		for i := 0; i < 4; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if strings.HasPrefix(relinkMovedProject(R, false), "shikii:") {
					mu.Lock()
					told++
					mu.Unlock()
				}
			}()
		}
		wg.Wait()
		if told != 1 {
			t.Fatalf("round %d: C1: the move was told %d times", round, told)
		}
	}
}

// Docs: "The next request's hook reconnects the project at its new path by itself". mv to another disk is a copy and a removal; the
// .shikii folder gets a new inode.
func TestFull1MoveToAnotherDiskIsLeftToThePerson(t *testing.T) {
	p := gitRepo(t)
	hookEnv(t, nil, p)
	common, _ := agentHome()
	P := feedbackScope(p)
	r := filepath.Join(t.TempDir(), "moved")
	full1Copy(t, P, r) // what mv does across disks
	if err := os.RemoveAll(P); err != nil {
		t.Fatal(err)
	}
	R := feedbackScope(r)
	// the hook cannot tell this from a copy whose original moved elsewhere (another folder), so it does not take it over by itself
	if n := relinkMovedProject(R, false); n != "" {
		t.Fatalf("a folder whose store folder changed is not reconnected by the hook: %q", n)
	}
	if n := copiedNotice(R); !strings.Contains(n, "cannot confirm") {
		t.Fatalf("the hook says why shikii is not running: %q", n)
	}
	// the person's --init: shikii cannot tell this from a copy of a moved original, so it becomes a project of its own (new id), its wiki
	// and records kept, and the old entry stays for the original (decided in 0.9.7 after the property test found a takeover)
	before := storeID(filepath.Join(R, ".shikii"), false)
	t.Chdir(R)
	out := capture(t, func() error { return cmdInit(nil) })
	if !strings.Contains(out, "a project of its own") || storeID(filepath.Join(R, ".shikii"), false) == before {
		t.Fatalf("--init makes it a project of its own with a new id:\n%s", out)
	}
	if active, _ := projectActive(filepath.Join(R, ".shikii"), R); !active {
		t.Fatal("and it is active")
	}
	_ = common
}

func firstLine(s string) string { l, _, _ := strings.Cut(s, "\n"); return l }

// 0.9.7 full audit 1, C8: a reconnection that stopped after pointing project.json here but before forgetting the old path left the old
// entry with this store's id. Registering the project forgets such entries whose folder is gone, and never one whose folder exists.
func TestRegisteringForgetsStaleEntriesOfTheSameStore(t *testing.T) {
	p := gitRepo(t)
	hookEnv(t, nil, p)
	common, _ := agentHome()
	P := feedbackScope(p)
	id := storeID(filepath.Join(P, ".shikii"), false)
	gone := filepath.Join(t.TempDir(), "gone")
	alive := feedbackScope(t.TempDir())
	for _, path := range []string{gone, alive} {
		e := projectEntry{ID: projectID(path), Name: filepath.Base(path), Path: path, StoreID: id}
		b, _ := json.Marshal(e)
		if err := os.WriteFile(filepath.Join(common, "projects", e.ID+".json"), b, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := registerProject(P); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(common, "projects", projectID(gone)+".json")); !os.IsNotExist(err) {
		t.Fatal("C8: the entry of a gone folder with this store's id is forgotten")
	}
	if _, err := os.Stat(filepath.Join(common, "projects", projectID(alive)+".json")); err != nil {
		t.Fatal("an entry whose folder exists is kept")
	}
}
