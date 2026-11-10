package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// trackedIgnoredRepo is a repository where Git tracks dist/app.js although .gitignore names dist/ (added with -f, or the rule
// came after the commit), next to dist/cache.tmp, which is ignored and not tracked.
func trackedIgnoredRepo(t *testing.T) string {
	t.Helper()
	repo := gitRepo(t)
	for name, body := range map[string]string{".gitignore": "dist/\n", "README.md": "# project\n", "dist/app.js": "console.log(1)\n", "dist/cache.tmp": "scratch\n"} {
		p := filepath.Join(repo, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if out, err := exec.Command("git", "-C", repo, "add", "-f", "dist/app.js").CombinedOutput(); err != nil {
		t.Fatalf("git add -f: %v %s", err, out)
	}
	gitCommitAll(t, repo, "init")
	if out, err := exec.Command("git", "-C", repo, "ls-files").CombinedOutput(); err != nil || !strings.Contains(string(out), "dist/app.js") || strings.Contains(string(out), "cache.tmp") {
		t.Fatalf("premise: git tracks dist/app.js and not dist/cache.tmp: %v %s", err, out)
	}
	return repo
}

// The listing the hooks compare is every file of the repository as Git sees it. A tracked file stays tracked when .gitignore
// names it, so it belongs to the listing; an ignored file Git does not track stays out.
func TestTreeListingKeepsTrackedFilesThatGitignoreNames(t *testing.T) {
	repo := trackedIgnoredRepo(t)
	_, listing, listed, _ := treeState(repo)
	if !listed {
		t.Fatal("premise: the listing is taken")
	}
	files := parseListing(listing)
	if _, ok := files["dist/app.js"]; !ok {
		t.Errorf("a tracked file that .gitignore names is in the listing: %q", listing)
	}
	if _, ok := files["dist/cache.tmp"]; ok {
		t.Errorf("an ignored file Git does not track stays out of the listing: %q", listing)
	}
	// a change to it is a changed file, and removing it is one too
	if err := os.WriteFile(filepath.Join(repo, "dist", "app.js"), []byte("console.log(2)\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, edited, _, _ := treeState(repo)
	if got := changedFiles(listing, edited); len(got) != 1 || got[0] != "dist/app.js" {
		t.Errorf("an edit to the tracked file is the one changed file, got %q", got)
	}
	if err := os.Remove(filepath.Join(repo, "dist", "app.js")); err != nil {
		t.Fatal(err)
	}
	_, removed, listedAfter, _ := treeState(repo)
	if got := changedFiles(listing, removed); !listedAfter || len(got) != 1 || got[0] != "dist/app.js" {
		t.Errorf("removing the tracked file is the one changed file, got %q (listed %v)", got, listedAfter)
	}
}

// A guard the person enabled reads the lines the turn added to such a file, like any other tracked file.
func TestStopGuardReadsTrackedFilesThatGitignoreNames(t *testing.T) {
	model := fakeAgentModel(t, func(string) float64 { return .1 })
	hookEnv(t, model)
	repo := trackedIgnoredRepo(t)
	t.Chdir(repo)
	capture(t, func() error { return cmdInit(nil) })
	root := filepath.Join(repo, ".shikii")
	writeGuard(t, root, guard{ID: "no-word", Kind: guardKindForbiddenWords, Title: "do not use the word", Words: []string{"FORBIDDENWORD"}})

	runHook(t, "user-prompt-submit", map[string]any{"session_id": "t", "cwd": repo, "prompt": "update the built script in dist"})
	if err := os.WriteFile(filepath.Join(repo, "dist", "app.js"), []byte("console.log(1)\n// FORBIDDENWORD\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// the untracked ignored file changes too: it is not the repository's, so it must not be read
	if err := os.WriteFile(filepath.Join(repo, "dist", "cache.tmp"), []byte("FORBIDDENWORD in scratch\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := runHook(t, "stop", map[string]any{"session_id": "t", "cwd": repo, "last_assistant_message": "updated"})
	if !strings.Contains(out, `"decision":"block"`) || !strings.Contains(out, "no-word") || !strings.Contains(out, "dist/app.js") {
		t.Errorf("a forbidden word added to a tracked file that .gitignore names must stop the turn: %q", out)
	}
	if strings.Contains(out, "cache.tmp") {
		t.Errorf("an ignored file Git does not track is not checked: %q", out)
	}
}
