package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// h4BrokenGuard is a guard file a person edited by hand and left invalid (paired-files without pairs). guard add tells the
// person to edit the file to change a check, so this state is one typo away.
const h4BrokenGuard = "id: half-edited\nkind: paired-files\ntitle: half edited by hand\nenabled: false\n"

// One guard file that does not load must not switch off the other, enabled guards at Stop.
func TestOneInvalidGuardFileDoesNotDisableTheOthersAtStop(t *testing.T) {
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
	if err := os.WriteFile(guardPath(root, "half-edited"), []byte(h4BrokenGuard), 0o600); err != nil {
		t.Fatal(err)
	}

	runHook(t, "user-prompt-submit", map[string]any{"session_id": "h4", "cwd": repo, "prompt": "add a line to the README about the storage layout"})
	if err := os.WriteFile(readme, []byte("# project\n\nthis line has FORBIDDENWORD in it\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := runHook(t, "stop", map[string]any{"session_id": "h4", "cwd": repo, "last_assistant_message": "added"})
	if !strings.Contains(out, `"decision":"block"`) || !strings.Contains(out, "no-word") {
		t.Fatalf("the valid, enabled guard still stops the turn next to an invalid guard file: %q", out)
	}
	// the invalid file is still reported where the person looks for it
	if _, err := loadGuards(root); err == nil || !strings.Contains(err.Error(), "half-edited.yaml") {
		t.Fatalf("guard list and status keep reporting the invalid file: %v", err)
	}
}

// The same for a command about to run (forbidden-commands, PreToolUse Bash).
func TestOneInvalidGuardFileDoesNotDisableTheOthersBeforeACommand(t *testing.T) {
	st, scope, _, _ := toolLessonFixture(t)
	writeGuard(t, st.Dir, guard{ID: "no-force-push", Kind: guardKindForbiddenCommands, Title: "never force push", Patterns: []string{`git\s+push\s+.*--force`}, Example: "git push origin main --force"})
	if err := os.WriteFile(guardPath(st.Dir, "half-edited"), []byte(h4BrokenGuard), 0o600); err != nil {
		t.Fatal(err)
	}
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "h4c", "cwd": scope, "prompt": "publish the branch"})
	out := runHook(t, "tool-lesson", map[string]any{"session_id": "h4c", "cwd": scope, "hook_event_name": "PreToolUse", "tool_name": "Bash", "tool_input": map[string]any{"command": "git push origin main --force"}})
	if !strings.Contains(out, `"permissionDecision":"deny"`) || !strings.Contains(out, "no-force-push") {
		t.Fatalf("the valid, enabled guard still refuses the command next to an invalid guard file: %q", out)
	}
}
