package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
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

// C1 "say it once": two hooks of the same moved folder at the same time (a prompt and a subagent, two agents, two sessions). Checked in
// one run instead of waiting for two hooks to race: right after this hook saw the old path and before it takes the lock, another hook
// reconnects the folder all the way; this hook then says nothing.
func TestFull1RelinkAfterAnotherHookReconnectedSaysNothing(t *testing.T) {
	R := full1Moved(t)
	other, ran := "", false
	old := beforeRelinkLock
	t.Cleanup(func() { beforeRelinkLock = old })
	beforeRelinkLock = func() {
		if !ran {
			ran = true
			other = relinkMovedProject(R, false)
		}
	}
	this := relinkMovedProject(R, false)
	if !strings.HasPrefix(other, "shikii:") {
		t.Fatalf("precondition: the other hook reconnects the moved folder: %q", other)
	}
	if this != "" {
		t.Fatalf("C1: the move was told twice; the second hook said %q", this)
	}
}

// The reconnection is written while its lock is held, so two hooks never both get past the second look at project.json: right after that
// look, the lock cannot be taken from outside.
func TestFull1RelinkWritesUnderTheLock(t *testing.T) {
	R := full1Moved(t)
	common, _ := agentHome()
	lock := filepath.Join(common, "relink-"+storeID(filepath.Join(R, ".shikii"), false)+".lock")
	tried := errors.New("whileRelinking did not run")
	old := whileRelinking
	t.Cleanup(func() { whileRelinking = old })
	whileRelinking = func() { tried = tryLockFromOutside(lock) }
	if n := relinkMovedProject(R, false); !strings.HasPrefix(n, "shikii:") {
		t.Fatalf("precondition: the moved folder is reconnected: %q", n)
	}
	if tried != syscall.EWOULDBLOCK {
		t.Fatalf("the reconnection lock is held while reconnecting: taking it from outside gave %v", tried)
	}
}

// full1Moved makes a project in a Git repository, moves its folder (mv) and returns the new path.
func full1Moved(t *testing.T) string {
	t.Helper()
	p := gitRepo(t)
	hookEnv(t, nil, p)
	r := filepath.Join(t.TempDir(), "moved")
	if err := os.Rename(feedbackScope(p), r); err != nil {
		t.Fatal(err)
	}
	return feedbackScope(r)
}

// tryLockFromOutside tries to take the flock of the file at path without waiting, and lets it go again when it got it: nil means nobody
// held it, syscall.EWOULDBLOCK means someone does.
func tryLockFromOutside(path string) error {
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer f.Close()
	err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if err == nil {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	}
	return err
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
