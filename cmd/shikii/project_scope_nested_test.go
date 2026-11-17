package main

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// nestedWorktree prepares an initialized git project with one commit and a linked worktree kept under it, the way chiten
// (.chiten/wt/<task>) and Claude Code (.claude/worktrees/<agent>) keep the trees their agents work in.
func nestedWorktree(t *testing.T) (repo, tree string) {
	t.Helper()
	repo = gitRepo(t)
	t.Chdir(repo)
	capture(t, func() error { return cmdInit(nil) })
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("# project\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitCommitAll(t, repo, "init")
	tree = filepath.Join(repo, ".chiten", "wt", "task")
	if out, err := exec.Command("git", "-C", repo, "worktree", "add", "-q", "-b", "task", tree).CombinedOutput(); err != nil {
		t.Fatalf("git worktree add: %v %s", err, out)
	}
	return repo, tree
}

// The project of a folder is the nearest initialized folder at or above it, with or without git: a worktree kept under a project
// uses that project's store. Without an initialized folder above, it stays the top of the git working tree, and init still prepares
// the folder it runs in.
func TestProjectIsTheNearestInitializedFolderAbove(t *testing.T) {
	hookEnv(t, nil)
	repo, tree := nestedWorktree(t)
	project := projectScopeAt(repo)
	inner := filepath.Join(tree, "docs")
	if err := os.MkdirAll(inner, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, cwd := range []string{tree, inner} {
		if got := projectScopeAt(cwd); got != project {
			t.Fatalf("a worktree under the project belongs to the project: from %s got %q, want %q", cwd, got, project)
		}
		root, scope, err := projectStoreAt(cwd)
		if err != nil || root != filepath.Join(project, ".shikii") || scope != project {
			t.Fatalf("its store is the project's: from %s got %q %q %v", cwd, root, scope, err)
		}
		if _, _, err := prepareHookProject(cwd); err != nil {
			t.Fatalf("the hooks run there with the project's store: %v", err)
		}
	}
	if _, err := os.Lstat(filepath.Join(tree, ".shikii")); !os.IsNotExist(err) {
		t.Fatalf("the worktree gets no store of its own: %v", err)
	}

	// outside git too
	plain := t.TempDir()
	t.Chdir(plain)
	capture(t, func() error { return cmdInit(nil) })
	sub := filepath.Join(plain, "a", "b")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if got, want := projectScopeAt(sub), projectScopeAt(plain); got != want || want != feedbackScope(plain) {
		t.Fatalf("a folder under an initialized folder outside git belongs to it: got %q, want %q", got, want)
	}

	// negative: no initialized folder above. The project is the top of the git working tree as before, and the hooks stay off
	other := gitRepo(t)
	below := filepath.Join(other, "x")
	if err := os.MkdirAll(below, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := projectScopeAt(below); got != feedbackScope(other) {
		t.Fatalf("without an initialized folder above, the top of the git working tree: got %q, want %q", got, feedbackScope(other))
	}
	if _, _, err := prepareHookProject(below); !errors.Is(err, errProjectInactive) {
		t.Fatalf("a folder that was not initialized stays inactive: %v", err)
	}

	// init prepares the folder it runs in, also under a project; from then on the nearer one is the project
	t.Chdir(tree)
	capture(t, func() error { return cmdInit(nil) })
	if got := projectScopeAt(inner); got != feedbackScope(tree) {
		t.Fatalf("init in a tree under a project makes it a project of its own: got %q, want %q", got, feedbackScope(tree))
	}
	if got := projectScopeAt(repo); got != project {
		t.Fatalf("the outer project is unchanged: got %q, want %q", got, project)
	}
}

// shikii guard try in a worktree kept under the project checks that worktree's changes with the project's guards. From the
// project's own tree, the worktree's change is not seen.
func TestGuardTryInAWorktreeUnderTheProjectUsesTheProjectsGuards(t *testing.T) {
	hookEnv(t, nil)
	repo, tree := nestedWorktree(t)
	writeGuard(t, filepath.Join(repo, ".shikii"), guard{ID: "no-token", Kind: guardKindForbiddenWords, Title: "no forbidden-token", Words: []string{"forbidden-token"}})
	if err := os.WriteFile(filepath.Join(tree, "README.md"), []byte("# project\n\nforbidden-token\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(tree)
	if out := capture(t, func() error { return cmdGuard([]string{"try"}) }); !strings.Contains(out, "STOP no-token") || !strings.Contains(out, "README.md") {
		t.Fatalf("try in the worktree stops on its change with the project's guard:\n%s", out)
	}
	t.Chdir(repo)
	if out := capture(t, func() error { return cmdGuard([]string{"try"}) }); !strings.Contains(out, "ok   no-token") {
		t.Fatalf("try in the project's own tree does not see the worktree's change:\n%s", out)
	}
}

// A session working in a worktree kept under the project is stopped by the project's guard, and its records go to the
// project's store.
func TestStopInAWorktreeUnderTheProjectIsBlockedByTheProjectsGuard(t *testing.T) {
	model := fakeAgentModel(t, func(string) float64 { return .1 })
	hookEnv(t, model)
	repo, tree := nestedWorktree(t)
	root := filepath.Join(repo, ".shikii")
	writeGuard(t, root, guard{ID: "no-token", Kind: guardKindForbiddenWords, Title: "no forbidden-token", Words: []string{"forbidden-token"}})

	runHook(t, "user-prompt-submit", map[string]any{"session_id": "w", "cwd": tree, "prompt": "add a note to the README"})
	if err := os.WriteFile(filepath.Join(tree, "README.md"), []byte("# project\n\nforbidden-token\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := runHook(t, "stop", map[string]any{"session_id": "w", "cwd": tree, "last_assistant_message": "added"})
	var parsed map[string]any
	if err := json.Unmarshal([]byte(out), &parsed); err != nil || parsed["decision"] != "block" {
		t.Fatalf("the Stop in the worktree is blocked by the project's guard: %s", out)
	}
	if reason, _ := parsed["reason"].(string); !strings.Contains(reason, "no-token") || !strings.Contains(reason, "README.md") {
		t.Fatalf("the reason names the guard and the file: %s", reason)
	}
	if recs := guardRecordsFor(root, "no-token"); len(recs) != 1 || recs[0].Pass {
		t.Fatalf("the stop is recorded in the project's store: %+v", recs)
	}
	if s, err := readSession(root, "w"); err != nil || s.Scope != projectScopeAt(repo) {
		t.Fatalf("the session is the project's: %+v %v", s, err)
	}
	if _, err := os.Lstat(filepath.Join(tree, ".shikii")); !os.IsNotExist(err) {
		t.Fatalf("the worktree gets no store of its own: %v", err)
	}
}

// In a worktree kept apart from the project's own tree, the work is committed on the worktree's branch. guard try there checks what the
// worktree changed since it left the project's branch, with its commits. The project's own tree keeps comparing with HEAD.
func TestGuardTryInAWorktreeChecksItsCommittedWork(t *testing.T) {
	hookEnv(t, nil)
	repo, tree := nestedWorktree(t)
	root := filepath.Join(repo, ".shikii")
	writeGuard(t, root, guard{ID: "no-token", Kind: guardKindForbiddenWords, Title: "no forbidden-token", Words: []string{"forbidden-token"}})
	writeGuard(t, root, guard{ID: "body", Kind: guardKindCommitBody, Title: "commit bodies name their verification", Required: []string{"Verification:"}})
	note := []byte("# project\n\nforbidden-token\n")
	if err := os.WriteFile(filepath.Join(tree, "README.md"), note, 0o644); err != nil {
		t.Fatal(err)
	}
	gitCommitAll(t, tree, "add a note")
	if err := os.WriteFile(filepath.Join(tree, "new.txt"), []byte("forbidden-token\n"), 0o644); err != nil { // not tracked yet
		t.Fatal(err)
	}
	t.Chdir(tree)
	out := capture(t, func() error { return cmdGuard([]string{"try"}) })
	if !strings.Contains(out, "since it left the project's branch") {
		t.Fatalf("try says what it compares in a worktree:\n%s", out)
	}
	if !strings.Contains(out, "STOP no-token") || !strings.Contains(out, "README.md") || !strings.Contains(out, "new.txt") {
		t.Fatalf("try in the worktree sees the committed change and the file not tracked yet:\n%s", out)
	}
	if !strings.Contains(out, "STOP body") {
		t.Fatalf("try in the worktree checks the bodies of the worktree's commits:\n%s", out)
	}

	// negative: the project's own tree is compared with HEAD as before, so the same word committed there is not reported
	if err := os.WriteFile(filepath.Join(repo, "README.md"), note, 0o644); err != nil {
		t.Fatal(err)
	}
	gitCommitAll(t, repo, "add a note")
	t.Chdir(repo)
	out = capture(t, func() error { return cmdGuard([]string{"try"}) })
	if strings.Contains(out, "since it left") || !strings.Contains(out, "ok   no-token") || strings.Contains(out, "STOP body") {
		t.Fatalf("try in the project's own tree compares with HEAD and does not look at commits:\n%s", out)
	}
}

// A repository of its own kept under the project (not a worktree of the project's repository) shares no commit with it: guard try
// there compares that repository's working tree with its own HEAD, with the project's guards.
func TestGuardTryInAnotherRepositoryUnderTheProjectComparesWithItsHead(t *testing.T) {
	hookEnv(t, nil)
	repo, _ := nestedWorktree(t)
	writeGuard(t, filepath.Join(repo, ".shikii"), guard{ID: "no-token", Kind: guardKindForbiddenWords, Title: "no forbidden-token", Words: []string{"forbidden-token"}})
	inner := filepath.Join(repo, "vendor", "lib")
	if err := os.MkdirAll(inner, 0o755); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("git", "-C", inner, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	if err := os.WriteFile(filepath.Join(inner, "a.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitCommitAll(t, inner, "init")
	if err := os.WriteFile(filepath.Join(inner, "a.txt"), []byte("x\nforbidden-token\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(inner)
	out := capture(t, func() error { return cmdGuard([]string{"try"}) })
	if !strings.Contains(out, "STOP no-token") || !strings.Contains(out, "a.txt") || strings.Contains(out, "since it left") {
		t.Fatalf("try in a repository of its own under the project checks its working tree against its HEAD:\n%s", out)
	}
}
