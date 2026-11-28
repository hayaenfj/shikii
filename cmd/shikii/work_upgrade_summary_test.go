package main

import (
	"strings"
	"testing"

	"github.com/hayaenfj/shikii/internal/notebook"
)

// 2026-11-28: the summary of the memory upgrade that every prepared work carries while an upgrade is under way repeated, word for word, the
// long guidance (memoryUpgradeNext) that the reply to the prepare that starts the batch carries again (and the hook says the short form
// of it in its own sentence). The summary tells when and how to start; how to review a batch is said where the batch is, in the reply
// that holds its pages. These tests keep every sentence of what the summary said in one of the two places.

// the sentences of the summary as it was, before it left how to review to the reply of the batch
var upgradeSummaryBefore = []string{
	"At a break in the task, prepare this receipt with query memory-upgrade to review existing knowledge in bounded batches.",
	"Review one batch per task (at most 10 articles), against original sources and current behavior.",
	"Save necessary corrections, then maintenance with the latest returned ref and a checked reason.",
	"Preserve sources, preferences and history; reading alone is not review.",
	"Resume remaining articles on the next task from durable progress.",
	"An explicit request to finish the upgrade may continue with prepare:{receipt,query:memory-upgrade}.",
	"Report actual blocked sources; never replace missing evidence with deletion or human grading.",
}

func TestTheUpgradeSummaryStartsTheBatchAndTheReplyOfTheBatchSaysHowToReviewIt(t *testing.T) {
	summary := memoryUpgradeSummaryNext(notebook.MemoryUpgrade{Status: "upgrading"})
	for _, start := range []string{"At a break in the task, prepare this receipt with query memory-upgrade", "one batch per task", "at most 10 articles"} {
		if !strings.Contains(summary, start) {
			t.Errorf("the summary tells when and how to start, and the size of a batch: %q is not in %q", start, summary)
		}
	}
	if strings.Contains(summary, memoryUpgradeNext) || strings.Contains(summary, "reading alone is not review") {
		t.Errorf("the summary does not say again what the reply of the batch says: %q", summary)
	}
	if !strings.Contains(summary, "reply") {
		t.Errorf("the summary says where the way to review is: %q", summary)
	}

	// the batch itself: its reply carries the guidance whole, with its policy
	repo, root, b := upgradeBatchFixture(t, 2)
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "upgrade-summary", "cwd": repo, "prompt": "Check the routing implementation."})
	v, err := workCall(t, b, "prepare", map[string]any{"session_id": "upgrade-summary", "turn_id": loadSession(root, "upgrade-summary").TurnID, "query": "memory-upgrade"})
	if err != nil {
		t.Fatal(err)
	}
	u, _ := v["memory_upgrade"].(map[string]any)
	reply, _ := u["next_action"].(string)
	if reply != memoryUpgradeNext || u["policy"] == nil {
		t.Fatalf("the reply of the batch carries the guidance and the policy: %v", u)
	}
	for _, sentence := range upgradeSummaryBefore {
		if !strings.Contains(summary, strings.TrimSuffix(sentence, ".")) && !strings.Contains(reply, sentence) {
			t.Errorf("a sentence the summary had is neither in the summary nor in the reply of the batch: %q", sentence)
		}
	}
}
