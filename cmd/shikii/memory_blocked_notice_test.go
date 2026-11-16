package main

import (
	"strings"
	"testing"
	"unicode"
)

// The notice a person sees while the upgrade is blocked is in Japanese and does not name a field of the agent's JSON reply.
func TestBlockedNoticeIsJapaneseAndNamesNoField(t *testing.T) {
	repo, root, _, _ := blockedUpgradeFixture(t)
	out, _, err := memoryUpgradeHook(root, feedbackScope(repo), &hookSession{})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("System notice: %q", out.System)
	if strings.Contains(out.System, "unavailable_reasons") {
		t.Fatalf("the person-visible notice names a JSON field the person cannot see: %q", out.System)
	}
	if !containsJapanese(out.System) {
		t.Fatalf("the person-visible notice is not in Japanese: %q", out.System)
	}
}

// The next action of a blocked upgrade names the fields the reply really carries, so the agent reports the pages it can find there.
func TestBlockedNextActionNamesTheReplyFields(t *testing.T) {
	_, root, b, _ := blockedUpgradeFixture(t)
	turn := loadSession(root, "upgrade").TurnID
	base, err := workCall(t, b, "prepare", map[string]any{"session_id": "upgrade", "turn_id": turn, "query": "memory-upgrade"})
	if err != nil {
		t.Fatal("setup: plain prepare", err)
	}
	mu, _ := base["memory_upgrade"].(map[string]any)
	if _, ok := mu["unavailable_sources"]; !ok {
		t.Fatalf("fixture: the reply does not carry unavailable_sources: %v", mu)
	}
	for _, field := range []string{"unavailable_sources", "unavailable_reasons"} {
		if !strings.Contains(memoryUpgradeBlockedNext, field) {
			t.Errorf("the blocked next action does not name the reply field %q: %q", field, memoryUpgradeBlockedNext)
		}
	}
}

// containsJapanese reports whether s has a kanji, hiragana or katakana letter.
func containsJapanese(s string) bool {
	for _, r := range s {
		if unicode.In(r, unicode.Han, unicode.Hiragana, unicode.Katakana) {
			return true
		}
	}
	return false
}
