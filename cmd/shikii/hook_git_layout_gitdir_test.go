package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// A kept answer of git names the git folder it was given for (keptEntryAgrees). Where only that folder is another, and the folder of
// the working tree, the shared git folder and the state of both configurations are what they were, nothing else tells the two apart,
// so the answer must not be used. Real git makes such a place: a linked worktree moved away (git worktree move) and another worktree
// made at its old path gets a git folder of its own under `<repository>/worktrees/` (the name taken is counted up), while it shares the
// repository, its configuration and its path with the worktree that was there. (Made again after a delete by hand, with add -f, git
// takes the registered git folder over, so that is not a case here.) TestAKeptAnswerNamesTheGitFolderItWasGivenFor turns a `.git`
// file to another git folder too, but there the shared git folder changes with it, so it holds with or without the comparison of the
// git folders.

// replacedWorktreeAt moves the linked worktree of main at top away and makes another worktree of main at top: the place of the working
// tree is the same afterwards, only its git folder is another. keep names a folder inside top (the project store of a hook) that is
// carried over to the new worktree, as the store of a project that stays at its path is.
func replacedWorktreeAt(t *testing.T, main, top, keep string) {
	t.Helper()
	moved := top + "-moved"
	gitAt(t, main, "worktree", "move", top, moved)
	gitAt(t, main, "worktree", "add", "-q", "--detach", top)
	if keep != "" {
		if err := os.Rename(filepath.Join(moved, keep), filepath.Join(top, keep)); err != nil {
			t.Fatal(err)
		}
	}
}

// onlyTheGitFolderChanged fails the test unless the two snapshots differ in the git folder alone: the premise of the cases below.
func onlyTheGitFolderChanged(t *testing.T, before, after placeSnapshot) {
	t.Helper()
	if before.layout.gitDir == after.layout.gitDir {
		t.Fatalf("premise: the worktree made again has another git folder, still %s", after.layout.gitDir)
	}
	if before.layout.commonDir != after.layout.commonDir {
		t.Fatalf("premise: the shared git folder is the same, %s then %s", before.layout.commonDir, after.layout.commonDir)
	}
	if before.config != after.config || before.wtConfig != after.wtConfig {
		t.Fatalf("premise: the configurations are in the state they were: %+v then %+v", before, after)
	}
}

func TestAKeptAnswerIsNotUsedForAnotherLinkedWorktreeMadeAtTheSamePath(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	main := filepath.Join(t.TempDir(), "main")
	newRepoAt(t, main)
	commitIn(t, main, "a")
	top := filepath.Join(filepath.Dir(main), "wt")
	gitAt(t, main, "worktree", "add", "-q", "--detach", top)
	var before, after placeSnapshot
	got, ok, n := keptThenChanged(t, top, func() {
		var seen bool
		if before, seen = snapshotAt(top); !seen {
			t.Fatal("premise: the files tell the place of the worktree")
		}
		replacedWorktreeAt(t, main, top, "")
		if after, seen = snapshotAt(top); !seen {
			t.Fatal("premise: the files tell the place of the worktree made again")
		}
		onlyTheGitFolderChanged(t, before, after)
	})
	if n == 0 {
		t.Error("the answer kept for the old git folder was used for the worktree made again, whose git folder is another")
	}
	if !ok || got.gitDir != after.layout.gitDir {
		t.Errorf("the place is %+v (%v), want the git folder of the worktree made again, %s", got, ok, after.layout.gitDir)
	}
}

// Through the hook: the request that follows the replacement of the worktree asks git where the repository is, as the first request in
// a repository does, and the one after that does not (the controls, so the case fails for the right reason).
func TestARequestAfterAWorktreeIsMadeAgainAtItsPathAsksGitWhereTheRepositoryIs(t *testing.T) {
	main := filepath.Join(t.TempDir(), "main")
	newRepoAt(t, main)
	commitIn(t, main, "a")
	top := filepath.Join(filepath.Dir(main), "wt")
	gitAt(t, main, "worktree", "add", "-q", "--detach", top)
	model := fakeAgentModel(t, func(string) float64 { return .9 })
	root := hookEnv(t, model, top)
	usePlanningSpec(t, root)
	t.Chdir(top)
	forgetKeptPlaces(t)
	calls := gitCallLog(t)
	// request runs a request of a session of its own and says whether it asked git where the repository is
	request := func(session string) (asked bool, got []string) {
		runManualHook(t, "user-prompt-submit", map[string]any{"session_id": session, "cwd": top, "prompt": planningTestRequest})
		got = calls()
		return slices.Contains(got, askedPlace), got
	}
	if asked, got := request("gitdir-a"); !asked {
		t.Fatalf("premise: the first request asks git where the repository is: %v", got)
	}
	if asked, got := request("gitdir-b"); asked {
		t.Fatalf("premise: the second request uses the answer kept by the first: %v", got)
	}
	before, seen := snapshotAt(top)
	if !seen {
		t.Fatal("premise: the files tell the place of the worktree")
	}
	replacedWorktreeAt(t, main, top, ".shikii")
	t.Chdir(top) // the folder the process stood in went away with the worktree that was moved
	after, seen := snapshotAt(top)
	if !seen {
		t.Fatal("premise: the files tell the place of the worktree made again")
	}
	onlyTheGitFolderChanged(t, before, after)
	calls() // what the replacement ran is not counted
	if asked, got := request("gitdir-c"); !asked {
		t.Errorf("the request after the worktree was made again used the answer kept for the old git folder; its git calls:\n%s", strings.Join(got, "\n"))
	}
	if asked, got := request("gitdir-d"); asked {
		t.Errorf("the answer given for the new git folder was not kept; the next request's git calls:\n%s", strings.Join(got, "\n"))
	}
}
