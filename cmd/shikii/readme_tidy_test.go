package main

import (
	"strings"
	"testing"
)

// The README's opening paragraph is rewritten for every version, and the 0.32.0 one was the only place in the README that said you
// can ask the agent to tidy shikii's wiki (audit of 0.33.0, U-10). A request that starts a feature keeps its line in the body of the
// README, after the opening paragraph, in both languages, and the skill still answers that request.
func TestTheReadmeBodyKeepsHowToAskForATidyUp(t *testing.T) {
	if skill := string(mustRead(t, "../../skills/shikii/SKILL.md")); !strings.Contains(skill, "Asked to tidy shikii or its wiki") {
		t.Fatal("precondition: the skill answers a request to tidy shikii")
	}
	for path, request := range map[string]string{
		"../../README.md":                `Ask the agent to "tidy shikii"`,
		"../../docs/readme/README_ja.md": "\u300cshikii\u3092\u6574\u7406\u3057\u3066\u300d\u3068\u983c\u3080\u3068",
	} {
		text := string(mustRead(t, path))
		start := strings.Index(text, "\n## ")
		if start < 0 {
			t.Fatalf("%s has no section after its opening paragraph", path)
		}
		if !strings.Contains(text[start:], request) {
			t.Errorf("%s does not say in its body how to ask for a tidy-up (%q)", path, request)
		}
	}
}
