package main

import (
	"strings"
	"testing"
)

// audit A: the notice of a request that needs no planning grade (the other form of the notice).
func TestAuditATheOtherNoticeSaysHowTheJSONReachesTheCommand(t *testing.T) {
	_, scope, _, _, _ := memoryEffectFixture(t)
	notice := feedbackContext(t, runHook(t, "user-prompt-submit", map[string]any{"session_id": "main", "cwd": scope, "prompt": "README \u306e\u5c0e\u5165\u624b\u9806\u3092\u66f4\u65b0\u3057\u3066\u304f\u3060\u3055\u3044"}))
	first := strings.SplitN(notice, "\n", 2)[0]
	t.Logf("%s", first)
	for _, w := range []string{"standard input", "shikii_wiki"} {
		if !strings.Contains(first, w) {
			t.Errorf("the notice of a request without a planning grade lacks %q", w)
		}
	}
}
