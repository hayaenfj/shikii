package main

import (
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/notebook"
)

// Probe: a page declared blocked is then used by an outcome. prepareProcedureReviews skips the blocked
// task but still sets complete = len(tasks) == 0, so review_complete stays false with nothing to review.

// A request to optimize the memory (prepare with query memory-optimize) reviews the declared pages again, too.
func TestMemoryOptimizeReviewsBlockedPagesAgain(t *testing.T) {
	repo, root, b, first := blockedUpgradeFixture(t)
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "upgrade", "cwd": repo, "prompt": "Optimize the memory now."})
	v, err := workCall(t, b, "prepare", map[string]any{"session_id": "upgrade", "turn_id": loadSession(root, "upgrade").TurnID, "query": "memory-optimize"})
	if err != nil {
		t.Fatal(err)
	}
	u, _ := v["memory_upgrade"].(map[string]any)
	pages, _ := u["pages"].([]any)
	if len(pages) == 0 {
		t.Fatalf("the optimization must review the declared pages again: status=%v pages=0", u["status"])
	}
	again := map[string]bool{}
	for _, p := range pages {
		again[p.(map[string]any)["maintenance_ref"].(string)] = true
	}
	hit := false
	for _, ref := range first {
		hit = hit || again[ref]
	}
	if !hit {
		t.Fatalf("a declared-blocked page must come back for review: first=%v again=%v", first, again)
	}
}

// Control: a plain memory upgrade (memory-upgrade) does not send declared pages back for review.
func TestPlainMemoryUpgradeStillSkipsBlockedPages(t *testing.T) {
	repo, root, b, _ := blockedUpgradeFixture(t)
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "upgrade", "cwd": repo, "prompt": "Continue the memory upgrade."})
	v, err := workCall(t, b, "prepare", map[string]any{"session_id": "upgrade", "turn_id": loadSession(root, "upgrade").TurnID, "query": "memory-upgrade"})
	if err != nil {
		t.Fatal(err)
	}
	u, _ := v["memory_upgrade"].(map[string]any)
	if pages, _ := u["pages"].([]any); len(pages) != 0 {
		t.Fatalf("a plain upgrade must not send declared-blocked pages back: pages=%d", len(pages))
	}
}

// blockedUpgradeFixture declares the first batch of the upgrade blocked and returns their maintenance refs.
func blockedUpgradeFixture(t *testing.T) (string, string, notebook.Book, []string) {
	t.Helper()
	repo, root, _ := planningFixture(t)
	b := notebook.Book{Root: root, Scope: feedbackScope(repo)}
	s, err := b.CaptureObserved(notebook.Observation{Kind: "test", Title: "Original rule", Content: "Keep existing evidence and current preferences.", Reference: "synthetic", Version: "1", ObservedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	loopPage(t, b, s.ID, nil)
	loopPage(t, b, s.ID, map[string]any{"title": "Second upgrade rule"})
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "upgrade", "cwd": repo, "prompt": "Check the routing implementation."})
	v, err := workCall(t, b, "prepare", map[string]any{"session_id": "upgrade", "turn_id": loadSession(root, "upgrade").TurnID, "query": "memory-upgrade"})
	if err != nil {
		t.Fatal(err)
	}
	pages, _ := v["memory_upgrade"].(map[string]any)["pages"].([]any)
	maint, refs := []any{}, []string{}
	for _, p := range pages {
		ref := p.(map[string]any)["maintenance_ref"].(string)
		refs = append(refs, ref)
		maint = append(maint, map[string]any{"ref": ref, "reason": "The cited source file does not exist.", "blocked": true})
	}
	if _, err := workCall(t, b, "record", map[string]any{"receipt": v["receipt"], "maintenance": maint, "result": map[string]any{"changed": "Declared pages blocked.", "checked": "Sources missing."}}); err != nil {
		t.Fatal("setup: block", err)
	}
	return repo, root, b, refs
}
