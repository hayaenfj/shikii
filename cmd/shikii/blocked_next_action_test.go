package main

import (
	"strings"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/notebook"
)

// Probe: a page declared blocked is then used by an outcome. prepareProcedureReviews skips the blocked
// task but still sets complete = len(tasks) == 0, so review_complete stays false with nothing to review.

// The only pages left are blocked: the upgrade stops, and its next action must not send the agent back to review them.
func TestOnlyBlockedPagesRemainingDoesNotSendReviewOn(t *testing.T) {
	repo, root, _ := planningFixture(t)
	b := notebook.Book{Root: root, Scope: feedbackScope(repo)}
	s, err := b.CaptureObserved(notebook.Observation{Kind: "test", Title: "Original rule", Content: "Keep existing evidence and current preferences.", Reference: "synthetic", Version: "1", ObservedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	loopPage(t, b, s.ID, nil)
	loopPage(t, b, s.ID, map[string]any{"title": "Second upgrade rule"})
	loopPage(t, b, s.ID, map[string]any{"title": "Third upgrade rule"})
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "upgrade", "cwd": repo, "prompt": "Check the routing implementation."})
	v, err := workCall(t, b, "prepare", map[string]any{"session_id": "upgrade", "turn_id": loadSession(root, "upgrade").TurnID, "query": "memory-upgrade"})
	if err != nil {
		t.Fatal(err)
	}
	pages := v["memory_upgrade"].(map[string]any)["pages"].([]any)
	if len(pages) != 3 {
		t.Fatal("setup: the three pages fit one batch, and all of them are declared blocked", len(pages))
	}
	maint := []any{}
	for _, p := range pages {
		maint = append(maint, map[string]any{"ref": p.(map[string]any)["maintenance_ref"], "reason": "The cited source file does not exist.", "blocked": true})
	}
	if _, err := workCall(t, b, "record", map[string]any{"receipt": v["receipt"], "maintenance": maint, "result": map[string]any{"changed": "Declared pages blocked.", "checked": "Sources missing."}}); err != nil {
		t.Fatal("setup: first block", err)
	}
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "upgrade", "cwd": repo, "prompt": "Check the routing condition again."})
	v3, err := workCall(t, b, "prepare", map[string]any{"session_id": "upgrade", "turn_id": loadSession(root, "upgrade").TurnID, "query": "memory-upgrade"})
	if err != nil {
		t.Fatal(err)
	}
	u := v3["memory_upgrade"].(map[string]any)
	t.Log("status =", u["status"], "pages =", len(u["pages"].([]any)), "next_action =", v3["next_action"], "memory next_action =", u["next_action"])
	if u["status"] != "blocked" {
		t.Fatal("only blocked pages remain: status must be blocked", u["status"])
	}
	if u["next_action"] == nil && v3["next_action"] == nil {
		t.Fatal("no stop instruction")
	}
	if s, _ := u["next_action"].(string); strings.Contains(s, "Resume remaining articles") || strings.Contains(s, "Review one batch") {
		t.Fatal("only blocked pages remain: the next action must not send the agent to review or resume a batch", s)
	}
}
