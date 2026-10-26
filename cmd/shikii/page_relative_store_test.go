package main

import (
	"path/filepath"
	"strings"
	"testing"
)

// 0.9.5 check: Claude Code and OpenCode start `shikii mcp --store .shikii` inside the project, so a decision made through MCP (reflect
// place) looked up the page under the scope "." and started a second `shikii serve --store .shikii` next to the hook's page, whose
// process list did not say which project it belonged to. The page is looked up and started under the project's absolute path.
func TestDecisionFromRelativeStoreUsesTheProjectsPage(t *testing.T) {
	model := fakeAgentModel(t, func(string) float64 { return .1 })
	repo := gitRepo(t)
	hookEnv(t, model, repo)
	t.Chdir(repo)
	spawned = nil
	if _, _, err := hookDecide(".shikii", "needs-plan", map[string]string{"request": "rename a variable"}, repo); err != nil {
		t.Fatal(err)
	}
	want := "--store " + filepath.Join(feedbackScope(repo), ".shikii") + " "
	if len(spawned) != 1 || !strings.Contains(spawned[0], want) {
		t.Fatalf("the page is started for the project's absolute path (%q): %q", want, spawned)
	}
}
