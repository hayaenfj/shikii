package main

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/notebook"
)

// 0.9.2: after the folder was renamed, a wiki store left under the old path's name made the wiki silently empty.
// The hook before a request tells the agent about the leftover store once per session.
func TestHookTellsWikiLeftUnderOldPath(t *testing.T) {
	repo := gitRepo(t)
	agent := hookEnv(t, nil, repo)
	t.Chdir(repo)
	// a store written before 0.9.7 under the old path's name, next to the current one (half copied by hand): a single old store is adopted
	// by itself, so only a leftover beside the current store is told
	if _, err := (notebook.Book{Root: agent, Scope: feedbackScope(repo)}).Capture(notebook.Observation{Kind: "test", Title: "now", Content: "PASS", Reference: "go test", Version: "v1", ObservedAt: time.Unix(2, 0)}); err != nil {
		t.Fatal(err)
	}
	elsewhere := t.TempDir()
	if _, err := (notebook.Book{Root: elsewhere, Scope: "/work/old-name"}).Capture(notebook.Observation{Kind: "test", Title: "確かめた", Content: "PASS", Reference: "go test", Version: "v1", ObservedAt: time.Unix(1, 0)}); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte("/work/old-name"))
	place := "records/wiki-sources/" + hex.EncodeToString(sum[:])
	if err := os.Rename(filepath.Join(elsewhere, "records", "wiki-sources", "project"), filepath.Join(agent, place)); err != nil {
		t.Fatal(err)
	}
	prompt := map[string]any{"session_id": "s1", "cwd": repo, "prompt": "README の誤字を直してください。"}
	first := feedbackContext(t, runHook(t, "user-prompt-submit", prompt))
	if !strings.Contains(first, "wiki store under another name") || !strings.Contains(first, place) {
		t.Fatalf("tells about the leftover store: %q", first)
	}
	prompt["prompt"] = "次は CHANGELOG の誤字を直してください。"
	if again := feedbackContext(t, runHook(t, "user-prompt-submit", prompt)); strings.Contains(again, "under the previous path's name") {
		t.Fatalf("only once in the same session: %q", again)
	}
}
