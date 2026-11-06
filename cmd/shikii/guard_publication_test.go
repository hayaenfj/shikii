package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPublicationGuardChecksCommitBodyBeforePush(t *testing.T) {
	repo, root, _ := planningFixture(t)
	if err := os.WriteFile(filepath.Join(repo, "publication.txt"), []byte("synthetic base\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitCommitAll(t, repo, "base before this request")
	runUnreviewedHook(t, "user-prompt-submit", map[string]any{"session_id": "reasoning", "cwd": repo, "prompt": "Publish the checked synthetic change."})
	writeGuard(t, root, guard{ID: "publication-body", Kind: guardKindCommitBody, Title: "Check the configured commit body", Required: []string{"Problem", "Changed", "Checks"}})
	if err := os.WriteFile(filepath.Join(repo, "publication.txt"), []byte("synthetic change\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitCommitAll(t, repo, "feat: synthetic change without its required body")
	for _, command := range []string{"git push origin HEAD", "rtk git push origin HEAD", "gh release create v-test --draft", "rtk gh release create v-test --draft", "git push origin HEAD 2>&1", "git push origin HEAD &> publication.log"} {
		out := runUnreviewedHook(t, "tool-lesson", map[string]any{"session_id": "reasoning", "cwd": repo, "tool_name": "Bash", "tool_input": map[string]any{"command": command}})
		if !strings.Contains(out, `"permissionDecision":"deny"`) || !strings.Contains(out, "publication-body") {
			t.Fatalf("an enabled commit rule must be checked before publishing (%s): %s", command, out)
		}
	}
	for _, command := range []string{"git log -1", "gh release view v-test", "echo 'git push origin HEAD'"} {
		out := runUnreviewedHook(t, "tool-lesson", map[string]any{"session_id": "reasoning", "cwd": repo, "tool_name": "Bash", "tool_input": map[string]any{"command": command}})
		if strings.Contains(out, `"permissionDecision":"deny"`) {
			t.Fatalf("a read-only or quoted command is not a publication: %s %s", command, out)
		}
	}
}
