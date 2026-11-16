package main

import "testing"

// A memory optimization sent with the receipt of an earlier prepare (the documented step) must start the review over for the
// pages declared blocked, the same as a prepare without a receipt. The receipt path goes through extendWork, which must reopen them too.
func TestMemoryOptimizeWithReceiptReviewsBlockedPagesAgain(t *testing.T) {
	_, root, b, _ := blockedUpgradeFixture(t)
	turn := loadSession(root, "upgrade").TurnID
	base, err := workCall(t, b, "prepare", map[string]any{"session_id": "upgrade", "turn_id": turn, "query": "memory-upgrade"})
	if err != nil {
		t.Fatal("setup: plain prepare", err)
	}
	receipt := base["receipt"]

	// control: a plain upgrade with the receipt keeps the declared pages skipped
	plain, err := workCall(t, b, "prepare", map[string]any{"receipt": receipt, "query": "memory-upgrade"})
	if err != nil {
		t.Fatal("control: plain upgrade with receipt", err)
	}
	pu, _ := plain["memory_upgrade"].(map[string]any)
	if pages, _ := pu["pages"].([]any); len(pages) != 0 {
		t.Fatalf("control: plain upgrade with receipt must skip blocked pages: pages=%d", len(pages))
	}

	// the documented step: optimization with the receipt
	opt, err := workCall(t, b, "prepare", map[string]any{"receipt": receipt, "query": "memory-optimize"})
	if err != nil {
		t.Fatal("optimize with receipt", err)
	}
	ou, _ := opt["memory_upgrade"].(map[string]any)
	pages, _ := ou["pages"].([]any)
	if len(pages) == 0 {
		t.Fatalf("optimization with the receipt must review the declared pages again: status=%v pages=0 unavailable=%v", ou["status"], ou["unavailable_sources"])
	}
}
