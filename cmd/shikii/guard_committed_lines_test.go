package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// H6: added lines are the lines that were not in the file when the request arrived. A turn that adds a forbidden word and
// commits it (the usual way to work: one commit per feature) must still be stopped: the content to compare with is the
// blob kept in the request's listing, not the HEAD the turn itself just moved.
func TestForbiddenWordCommittedInTheTurnStillStops(t *testing.T) {
	model := fakeAgentModel(t, func(string) float64 { return .1 })
	hookEnv(t, model)
	repo := gitRepo(t)
	t.Chdir(repo)
	capture(t, func() error { return cmdInit(nil) })
	root := filepath.Join(repo, ".shikii")
	readme := filepath.Join(repo, "README.md")
	if err := os.WriteFile(readme, []byte("# project\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitCommitAll(t, repo, "init")
	writeGuard(t, root, guard{ID: "no-word", Kind: guardKindForbiddenWords, Title: "do not use the word", Words: []string{"FORBIDDENWORD"}})

	runHook(t, "user-prompt-submit", map[string]any{"session_id": "h6", "cwd": repo, "prompt": "add a line to the README about the storage layout and commit it"})
	if err := os.WriteFile(readme, []byte("# project\n\nthis line has FORBIDDENWORD in it\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// a file the turn creates and commits is checked too (it was not in the request's listing: every line is new)
	if err := os.WriteFile(filepath.Join(repo, "NOTES.md"), []byte("a new file with FORBIDDENWORD\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitCommitAll(t, repo, "docs: describe the storage layout")
	out := runHook(t, "stop", map[string]any{"session_id": "h6", "cwd": repo, "last_assistant_message": "added and committed"})
	if !strings.Contains(out, `"decision":"block"`) || !strings.Contains(out, "no-word") || !strings.Contains(out, `README.md: \"FORBIDDENWORD\"`) || !strings.Contains(out, `NOTES.md: \"FORBIDDENWORD\"`) {
		t.Fatalf("a forbidden word the turn added and committed must stop the turn: %q", out)
	}
}

// H6 (the other direction): a line that was already in the file when the request arrived is not an added line, also when
// the turn commits: old text with the word, a clean line added and committed in the turn, must not stop the turn.
func TestLinesFromBeforeTheRequestAreNotAddedLinesAfterACommit(t *testing.T) {
	repo := gitRepo(t)
	readme := filepath.Join(repo, "README.md")
	if err := os.WriteFile(readme, []byte("# project\n\nan old line with FORBIDDENWORD\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitCommitAll(t, repo, "init")
	_, before, listed, _, _ := treeStateWithScope(repo)
	if !listed {
		t.Fatal("premise: the listing at the request is taken")
	}
	if err := os.WriteFile(readme, []byte("# project\n\nan old line with FORBIDDENWORD\n\na new clean line\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitCommitAll(t, repo, "docs: add a line")
	_, after, _, _, _ := treeStateWithScope(repo)
	in := guardTurnInput(repo, listingHeader("r")+before, changedFiles(before, after), true, "", time.Time{})
	if got := strings.Join(in.Added["README.md"], "|"); got != "a new clean line" {
		t.Fatalf("only the line the turn added is an added line, got %q", got)
	}
}

// H6 (a file that was already modified, uncommitted, when the request arrived): its blob lived only in the snapshot's
// temporary store, so the comparison falls back to a commit. That commit must be the HEAD kept at the request, not the
// HEAD the turn's own commit moved to.
func TestForbiddenWordCommittedInTheTurnStillStopsForAFileDirtyAtTheRequest(t *testing.T) {
	repo := gitRepo(t)
	readme := filepath.Join(repo, "README.md")
	if err := os.WriteFile(readme, []byte("# project\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitCommitAll(t, repo, "init")
	if err := os.WriteFile(readme, []byte("# project\n\nthe person's own uncommitted line\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	head := gitHead(repo)
	_, before, listed, _, _ := treeStateWithScope(repo)
	if !listed || head == "" {
		t.Fatal("premise: the listing and HEAD at the request are taken")
	}
	if err := os.WriteFile(readme, []byte("# project\n\nthe person's own uncommitted line\n\nthis line has FORBIDDENWORD in it\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitCommitAll(t, repo, "docs: add a line")
	_, after, _, _, _ := treeStateWithScope(repo)
	in := guardTurnInput(repo, listingHeader("r")+before, changedFiles(before, after), true, head, time.Time{})
	if got := strings.Join(in.Added["README.md"], "|"); !strings.Contains(got, "FORBIDDENWORD") {
		t.Fatalf("the line the turn added and committed is an added line, got %q", got)
	}
}
