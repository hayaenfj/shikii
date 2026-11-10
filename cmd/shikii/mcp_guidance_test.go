package main

import (
	"strings"
	"testing"
)

func TestMCPSharedWorkflowIsSuppliedOnce(t *testing.T) {
	send, _ := mcpSession(t, "true", map[string]float64{"change_request": 1})
	initial := result(t, send(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize"}))
	bootstrap := initial["instructions"].(string)
	for _, required := range []string{"shikii_wiki", "description", "before", "never permission", "never call shikii_label with your own judgment", "absolute", "original project", "worktree", "project"} {
		if !strings.Contains(bootstrap, required) {
			t.Errorf("the initial instructions must retain the entry point and safeguards: %q", required)
		}
	}
	listed := result(t, send(map[string]any{"jsonrpc": "2.0", "id": 2, "method": "tools/list"}))
	tools := listed["tools"].([]any)
	copies := 0
	for _, raw := range tools {
		tool := raw.(map[string]any)
		description := tool["description"].(string)
		count := strings.Count(description, mcpInstructions)
		copies += count
		if count != 0 && tool["name"] != "shikii_wiki" {
			t.Fatal("the shared workflow belongs to the named entry point")
		}
	}
	if copies != 1 {
		t.Fatalf("supply the complete original workflow once in the wiki description: copies=%d", copies)
	}
	// Some clients append initialize.instructions to each tool description.
	// Count that transport explicitly, without treating bytes as model quality.
	before := len(tools) * len(mcpInstructions)
	after := len(tools)*len(bootstrap) + len(mcpInstructions)
	if after >= before {
		t.Fatalf("sharing must reduce repeated guidance including its entry point: %d -> %d", before, after)
	}
	t.Logf("shared guidance with %d tool attachments, UTF-8 bytes: %d -> %d", len(tools), before, after)
}

func TestMCPWorktreeGuidanceKeepsOriginalProject(t *testing.T) {
	for name, text := range map[string]string{"workflow": mcpInstructions, "bootstrap": mcpBootstrap, "schema": mcpProjectSchema["description"].(string)} {
		for _, want := range []string{"original project", "worktree"} {
			if !strings.Contains(text, want) {
				t.Errorf("%s lacks %q", name, want)
			}
		}
	}
	_, _, err := mcpProjectStore(t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "original project") {
		t.Fatalf("unregistered worktree guidance: %v", err)
	}
}
