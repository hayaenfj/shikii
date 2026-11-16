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

// A receipt extension that carries only a proposal inherits the stored query. The stored memory-optimize must not start the review over
// again: only the request's own query does, so the declared blocks stay skipped.
func TestProposalExtensionKeepsDeclaredBlocks(t *testing.T) {
	_, root, b, _ := blockedUpgradeFixture(t)
	turn := loadSession(root, "upgrade").TurnID
	v1, err := workCall(t, b, "prepare", map[string]any{"session_id": "upgrade", "turn_id": turn, "query": "memory-optimize"})
	if err != nil {
		t.Fatal("setup: optimize", err)
	}
	pages, _ := v1["memory_upgrade"].(map[string]any)["pages"].([]any)
	maint := []any{}
	for _, p := range pages {
		maint = append(maint, map[string]any{"ref": p.(map[string]any)["maintenance_ref"], "reason": "The cited source file does not exist.", "blocked": true})
	}
	if _, err := workCall(t, b, "record", map[string]any{"receipt": v1["receipt"], "maintenance": maint, "result": map[string]any{"changed": "Declared pages blocked.", "checked": "Sources missing."}}); err != nil {
		t.Fatal("setup: block", err)
	}
	receipt := v1["receipt"]
	// control: a plain upgrade with the receipt skips the declared pages
	ctl, err := workCall(t, b, "prepare", map[string]any{"receipt": receipt, "query": "memory-upgrade"})
	if err != nil {
		t.Fatal("control", err)
	}
	if cp, _ := ctl["memory_upgrade"].(map[string]any)["pages"].([]any); len(cp) != 0 {
		t.Fatalf("control: a plain upgrade with the receipt must skip the declared pages: pages=%d", len(cp))
	}
	// the extension with a proposal only
	pr, err := workCall(t, b, "prepare", map[string]any{"receipt": receipt, "proposal": "Check the routing implementation before changing it."})
	if err != nil {
		t.Fatal("proposal extension", err)
	}
	mu, _ := pr["memory_upgrade"].(map[string]any)
	if pp, _ := mu["pages"].([]any); len(pp) != 0 {
		t.Fatalf("a proposal extension reopened the declared blocked pages: pages=%d status=%v", len(pp), mu["status"])
	}
}
