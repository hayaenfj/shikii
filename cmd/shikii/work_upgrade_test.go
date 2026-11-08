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
	loopPage(t, b, s.ID, map[string]any{"title": "Second upgrade rule"})
	loopPage(t, b, s.ID, map[string]any{"title": "Third upgrade rule"})
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
	if len(pages) != 2 || !strings.Contains(u["next_action"].(string), "one batch per task") {
		t.Fatal("routine work must review one bounded batch and resume later", u["next_action"])
	}
	page := pages[0].(map[string]any)
	if page["article"] == nil || page["ref"] == "" || page["maintenance_ref"] == "" {
		t.Fatal(page)
	}
	maintenance := []any{}
	for _, raw := range pages {
		p := raw.(map[string]any)
		maintenance = append(maintenance, map[string]any{"ref": p["maintenance_ref"], "reason": "Checked original evidence; the rule remains applicable."})
		if p["task"].(map[string]any)["next_action"] == u["policy"] {
			t.Fatal("the shared policy must not be repeated in each task")
		}
	}
	r, err := workCall(t, b, "record", map[string]any{"receipt": v["receipt"], "maintenance": maintenance, "result": map[string]any{"changed": "Reviewed the existing rules.", "evidence": []string{page["ref"].(string)}}})
	if err != nil || r["complete"] != true {
		t.Fatal(r, err)
	}
	if progress, err := b.MemoryUpgrade(); err != nil || progress.Reviewed != 2 || progress.Status != "upgrading" {
		t.Fatal("a completed batch must retain the unfinished upgrade", progress, err)
	}
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "upgrade", "cwd": repo, "prompt": "Check the next routing condition."})
	next, err := workCall(t, b, "prepare", map[string]any{"session_id": "upgrade", "turn_id": loadSession(root, "upgrade").TurnID, "query": "memory-upgrade"})
	if err != nil {
		t.Fatal(err)
	}
	u = next["memory_upgrade"].(map[string]any)
	if u["reviewed"] != float64(2) || len(u["pages"].([]any)) != 1 {
		t.Fatal("the next task must resume only the remaining page", u)
	}
	page = u["pages"].([]any)[0].(map[string]any)
	_, err = workCall(t, b, "record", map[string]any{"receipt": next["receipt"], "maintenance": []any{map[string]any{"ref": page["maintenance_ref"], "reason": "Checked the remaining original evidence."}}})
	if err != nil {
		t.Fatal(err)
	}
	next, err = workCall(t, b, "prepare", map[string]any{"receipt": next["receipt"], "query": "memory-upgrade"})
	if err != nil || next["memory_upgrade"].(map[string]any)["status"] != "complete" {
		t.Fatal("resume and finish without another human form", next, err)
	}
}
