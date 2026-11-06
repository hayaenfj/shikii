package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func publicationBoundaryFixture(t *testing.T, committed bool) (string, string) {
	t.Helper()
	repo, root, _ := planningFixture(t)
	path := filepath.Join(repo, "publication.txt")
	if err := os.WriteFile(path, []byte("synthetic base\n"), 0600); err != nil {
		t.Fatal(err)
	}
	gitCommitAll(t, repo, "base before request")
	runUnreviewedHook(t, "user-prompt-submit", map[string]any{"session_id": "audit-c", "cwd": repo, "prompt": "Publish the synthetic change."})
	writeGuard(t, root, guard{ID: "audit-c-body", Kind: guardKindCommitBody, Title: "Required body", Required: []string{"Problem", "Changed", "Checks"}})
	if err := os.WriteFile(path, []byte("synthetic update\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if committed {
		gitCommitAll(t, repo, "feat: missing required body")
	}
	return repo, root
}

func TestPublicationGuardDryRunIsReadOnly(t *testing.T) {
	repo, _ := publicationBoundaryFixture(t, true)
	for _, command := range []string{"git push --dry-run origin HEAD", "git push -n origin HEAD", "git push --help", "rtk git push --dry-run origin HEAD", "gh release create --help", "gh --repo owner/project release --help", "gh release view v-test"} {
		out := runUnreviewedHook(t, "tool-lesson", map[string]any{"session_id": "audit-c", "cwd": repo, "tool_name": "Bash", "tool_input": map[string]any{"command": command}})
		if strings.Contains(out, `"permissionDecision":"deny"`) {
			t.Fatal("a read-only dry run was refused by publication checks: " + command + out)
		}
	}
}

func TestPublicationGuardCompoundPublicationRechecksFutureCommit(t *testing.T) {
	repo, _ := publicationBoundaryFixture(t, false)
	out := runUnreviewedHook(t, "tool-lesson", map[string]any{"session_id": "audit-c", "cwd": repo, "tool_name": "Bash", "tool_input": map[string]any{"command": "git commit -am 'feat: missing required body' && git push origin HEAD"}})
	if !strings.Contains(out, `"permissionDecision":"deny"`) {
		t.Fatal("a supported compound publication can add a missing-body commit after the sole pre-tool check: " + out)
	}
}

func TestPublicationGuardPublicationForms(t *testing.T) {
	for _, command := range []string{"gh --repo owner/project release create v-test", "git --git-dir .git push origin HEAD", "git -C . push origin HEAD", "rtk git --git-dir=.git push origin HEAD", "gh release --repo owner/project create v-test", "gh release new v-test", "gh release create v-test --notes --help", "git push --receive-pack --dry-run origin HEAD", "git push -- --dry-run"} {
		t.Run(command, func(t *testing.T) {
			if !isPublicationCommand(command) {
				t.Fatal("a literal publication command with an ordinary global option was not recognized")
			}
		})
	}
}

func TestPublicationGuardSinglePublicationWithFDRedirection(t *testing.T) {
	repo, _ := publicationBoundaryFixture(t, false)
	gitCommitAll(t, repo, "feat: checked synthetic change\n\nProblem: a synthetic condition.\nChanged: retain the condition.\nChecks: checked the fixture.")
	out := runUnreviewedHook(t, "tool-lesson", map[string]any{"session_id": "audit-c", "cwd": repo, "tool_name": "Bash", "tool_input": map[string]any{"command": "git push origin HEAD 2>&1"}})
	if strings.Contains(out, `"permissionDecision":"deny"`) {
		t.Fatal("a single publication with FD redirection was treated as a compound command: " + out)
	}
}
