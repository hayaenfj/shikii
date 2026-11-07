package main

import (
	"strings"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/notebook"
)

func TestWorkUpgradeUsesExistingReceiptAndMaintenance(t *testing.T) {
	repo, root, _ := planningFixture(t)
	b := notebook.Book{Root: root, Scope: feedbackScope(repo)}
	s, err := b.Capture(notebook.Observation{Kind: "test", Title: "Original rule", Content: "Keep existing evidence and current preferences.", Reference: "synthetic", Version: "1", ObservedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	loopPage(t, b, s.ID, nil)
	out := runHook(t, "user-prompt-submit", map[string]any{"session_id": "upgrade", "cwd": repo, "prompt": "Check the routing implementation."})
	if !strings.Contains(out, "Upgrading memory") || !strings.Contains(feedbackContext(t, out), "memory-upgrade") {
		t.Fatal("an installed agent must automatically receive the upgrade work", out)
	}
	v, err := workCall(t, b, "prepare", map[string]any{"session_id": "upgrade", "turn_id": loadSession(root, "upgrade").TurnID, "query": "memory-upgrade"})
	if err != nil {
		t.Fatal(err)
	}
	u, ok := v["memory_upgrade"].(map[string]any)
	if !ok || u["status"] != "upgrading" {
		t.Fatal("deliver the actual pinned upgrade articles", v)
	}
	pages := u["pages"].([]any)
	page := pages[0].(map[string]any)
	if page["article"] == nil || page["ref"] == "" || page["maintenance_ref"] == "" {
		t.Fatal(page)
	}
	r, err := workCall(t, b, "record", map[string]any{"receipt": v["receipt"], "maintenance": []any{map[string]any{"ref": page["maintenance_ref"], "reason": "Checked original evidence; the rule remains applicable."}}, "result": map[string]any{"changed": "Reviewed the existing rule.", "evidence": []string{page["ref"].(string)}}})
	if err != nil || r["complete"] != true {
		t.Fatal(r, err)
	}
	next, err := workCall(t, b, "prepare", map[string]any{"receipt": v["receipt"], "query": "memory-upgrade"})
	if err != nil || next["memory_upgrade"].(map[string]any)["status"] != "complete" {
		t.Fatal("resume and finish without another human form", next, err)
	}
}
