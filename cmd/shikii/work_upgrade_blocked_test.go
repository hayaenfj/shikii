package main

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/notebook"
)

// The agent declares the page it was shown blocked in the work record (maintenance with blocked: true and a reason). The record
// is complete without a review of that page, and the next preparation moves on to the pages after it instead of showing the same
// page again (2026-11-16: the batch held 3e99c69627aacb5e and 4a3d1d4c7a64453d for every preparation, 29 of 103 reviewed).
func TestWorkRecordBlockedPageLetsTheBatchMoveOn(t *testing.T) {
	repo, root, _ := planningFixture(t)
	b := notebook.Book{Root: root, Scope: feedbackScope(repo)}
	s, err := b.CaptureObserved(notebook.Observation{Kind: "test", Title: "Original rule", Content: "Keep existing evidence and current preferences.", Reference: "synthetic", Version: "1", ObservedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	// one page more than a batch holds: the page after the batch is the one that enters when a page is declared blocked
	for i := 1; i <= notebook.MemoryUpgradeBatch+1; i++ {
		loopPage(t, b, s.ID, map[string]any{"title": fmt.Sprintf("Upgrade rule %02d", i)})
	}
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "upgrade", "cwd": repo, "prompt": "Check the routing implementation."})
	v, err := workCall(t, b, "prepare", map[string]any{"session_id": "upgrade", "turn_id": loadSession(root, "upgrade").TurnID, "query": "memory-upgrade"})
	if err != nil {
		t.Fatal(err)
	}
	pages := v["memory_upgrade"].(map[string]any)["pages"].([]any)
	if len(pages) != notebook.MemoryUpgradeBatch {
		t.Fatal("the first preparation shows a full batch", len(pages))
	}
	blocked := pages[0].(map[string]any)
	blockedID := blocked["article"].(map[string]any)["id"].(string)
	r, err := workCall(t, b, "record", map[string]any{"receipt": v["receipt"], "maintenance": []any{map[string]any{"ref": blocked["maintenance_ref"], "reason": "The cited source file does not exist.", "blocked": true}}, "result": map[string]any{"changed": "Declared one page blocked.", "checked": "The cited source file is missing from the store."}})
	if err != nil || r["complete"] != true {
		t.Fatal("a blocked declaration completes the record without reviewing that page", r, err)
	}

	runHook(t, "user-prompt-submit", map[string]any{"session_id": "upgrade", "cwd": repo, "prompt": "Check the next routing condition."})
	next, err := workCall(t, b, "prepare", map[string]any{"session_id": "upgrade", "turn_id": loadSession(root, "upgrade").TurnID, "query": "memory-upgrade"})
	if err != nil {
		t.Fatal(err)
	}
	u := next["memory_upgrade"].(map[string]any)
	if u["reviewed"] != float64(0) {
		t.Fatal("a blocked page is not reviewed", u)
	}
	for _, raw := range u["pages"].([]any) {
		if raw.(map[string]any)["article"].(map[string]any)["id"] == blockedID {
			t.Fatal("the blocked page must not come back at the head of the batch", u["pages"])
		}
	}
	if len(u["pages"].([]any)) != notebook.MemoryUpgradeBatch {
		t.Fatal("the batch moves on to the other pages, the page after the first batch among them", len(u["pages"].([]any)))
	}
	if !strings.Contains(strings.Join(anyStrings(u["unavailable_sources"]), " "), blockedID) {
		t.Fatal("the blocked page is reported by its id", u["unavailable_sources"])
	}
}

func anyStrings(v any) []string {
	out := []string{}
	list, _ := v.([]any)
	for _, item := range list {
		out = append(out, item.(string))
	}
	return out
}
