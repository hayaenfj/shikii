package main

import (
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/notebook"
)

// Probe: a page declared blocked is then used by an outcome. prepareProcedureReviews skips the blocked
// task but still sets complete = len(tasks) == 0, so review_complete stays false with nothing to review.
func TestBlockedPageDoesNotKeepReviewIncomplete(t *testing.T) {
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
	blocked := pages[0].(map[string]any)
	r, err := workCall(t, b, "record", map[string]any{"receipt": v["receipt"], "maintenance": []any{map[string]any{"ref": blocked["maintenance_ref"], "reason": "The cited source file does not exist.", "blocked": true}}, "result": map[string]any{"changed": "Declared one page blocked.", "checked": "The cited source file is missing from the store."}})
	if err != nil || r["complete"] != true {
		t.Fatal("setup: block must complete", r, err)
	}
	r2, err := workCall(t, b, "record", map[string]any{"receipt": v["receipt"], "outcomes": []any{map[string]any{"ref": blocked["ref"], "conditions": "The routing condition held.", "action": "Used the rule.", "result": "The routing was checked."}}, "result": map[string]any{"changed": "Recorded an outcome.", "checked": "Outcome recorded."}})
	t.Log("review_complete =", r2["review_complete"], "procedure_reviews =", r2["procedure_reviews"], "next_action =", r2["next_action"], "err =", err)
	if r2["review_complete"] != true {
		t.Fatal("only a blocked page is pending, so review_complete must not stay false", r2["review_complete"], r2["next_action"])
	}
}

// Control: the same outcome without a block keeps the review incomplete, so the fix does not hide a page that still needs review.
func TestUnblockedPageKeepsReviewIncomplete(t *testing.T) {
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
	blocked := pages[0].(map[string]any)
	r2, err := workCall(t, b, "record", map[string]any{"receipt": v["receipt"], "outcomes": []any{map[string]any{"ref": blocked["ref"], "conditions": "The routing condition held.", "action": "Used the rule.", "result": "The routing was checked."}}, "result": map[string]any{"changed": "Recorded an outcome.", "checked": "Outcome recorded."}})
	t.Log("control review_complete =", r2["review_complete"], "procedure_reviews =", len(r2["procedure_reviews"].([]any)), "err =", err)
	if len(r2["procedure_reviews"].([]any)) != 1 {
		t.Fatal("control: the unblocked page must be listed as a pending procedure review")
	}
}
