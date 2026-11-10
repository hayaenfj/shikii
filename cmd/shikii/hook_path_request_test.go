package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A request that starts with a file path ("/Users/.../notes.txt ...", as a dragged file or a pasted path gives) is the
// person's request, not a slash command. It must be kept as the turn's request, and the Stop of that turn must still block
// on a failed guard (before the fix the previous turn's Judged made every guard a recheck that never blocks).
func TestRequestStartingWithAPathIsARequestAndItsStopStillBlocks(t *testing.T) {
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

	// turn 1: an ordinary request that ends clean and is judged
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "h2", "cwd": repo, "prompt": "explain what the README says about the storage layout"})
	first := loadSession(root, "h2").TurnID
	if _, err := planningCall(t, "submit", map[string]any{"session_id": "h2", "turn_id": first, "agreed": map[string]any{"unknown": true}, "open": map[string]any{"unknown": true}}); err != nil {
		t.Fatal(err)
	}
	if out := runHook(t, "stop", map[string]any{"session_id": "h2", "cwd": repo, "last_assistant_message": "explained"}); strings.Contains(out, "block") {
		t.Fatalf("premise: the first turn ends clean: %s", out)
	}
	if !loadSession(root, "h2").Judged {
		t.Fatal("premise: the first turn was judged")
	}

	// turn 2: the person's message starts with an absolute path
	prompt := "/Users/someone/Desktop/notes.txt has the wording to use; add its first paragraph to the README"
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "h2", "cwd": repo, "prompt": prompt})
	s := loadSession(root, "h2")
	if s.TurnID == first || s.Request != prompt {
		t.Errorf("a message that starts with a path is a new request: turn=%q (first %q) request=%q", s.TurnID, first, s.Request)
	}
	if err := os.WriteFile(readme, []byte("# project\n\nthis line has FORBIDDENWORD in it\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := runHook(t, "stop", map[string]any{"session_id": "h2", "cwd": repo, "last_assistant_message": "added"})
	if !strings.Contains(out, `"decision":"block"`) || !strings.Contains(out, "no-word") {
		t.Fatalf("the guard still stops the turn of a request that starts with a path: %q", out)
	}
}

// Real slash commands stay skipped (/model, /plugin:skill args, a lone "/").
func TestSlashCommandsAreStillSkipped(t *testing.T) {
	for prompt, want := range map[string]bool{
		"/model":                              true,
		"/codex:rescue look at the failure":   true,
		"/mcp__server__prompt":                true,
		"/":                                   true,
		"/tmp/build.log shows the error":      false,
		"/Users/someone/a b/notes.txt see it": false,
		"/etc/hosts":                          false,
		"explain /model":                      false,
	} {
		if got := isSlashCommand(prompt); got != want {
			t.Errorf("isSlashCommand(%q) = %v, want %v", prompt, got, want)
		}
	}
}
