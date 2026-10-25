package main

import (
	"os"
	"strings"
	"testing"
)

// 0.9.1: rules, tests and procedures (commands or scripts) the agent newly made or changed itself are also written to the wiki on the spot (adding to or fixing
// an existing page if there is one). The ritual used to write only the person's corrections, so even the way to run tests (scripts/go-test.sh) never reached the wiki (the person: "if you
// make something new as a rule or a test, always write it in the wiki"). So that it reaches agents that do not read the skill (Codex without hooks, OpenCode),
// it goes into every entry point (the ritual one-liner, the reflection text, the AGENTS.md section, the MCP guidance, the skill)
func TestNewRulesTestsAndProceduresGoToTheWiki(t *testing.T) {
	ja := "新しく作った・変えた決まり・テスト・手順"
	en := "rule, test or procedure"
	reflectText := reflectAsk("shikii")
	skill, _ := os.ReadFile("../../skills/shikii/SKILL.md")
	wikiRef, _ := os.ReadFile("../../skills/shikii/references/wiki.md")
	// The pre-work sorting guidance arrives with every request (the ritual one-liner is only once per session, and Claude Code in a project without work rules did not use it when it finished making something; measured in 0.9.1)
	for name, text := range map[string]string{"ritual one-liner (habitsNotice)": habitsNotice, "AGENTS.md section": agentsSection, "reflection text": reflectText, "pre-work sorting guidance (every time)": planningNotice("s", "t")} {
		if !strings.Contains(text, ja) && !strings.Contains(text, en) { // Japanese before the English translation, English after
			t.Errorf("%s lacks the rule to also write %q (or %q) to the wiki", name, en, ja)
		}
	}
	for name, text := range map[string]string{"MCP guidance": mcpInstructions, "SKILL.md": string(skill), "references/wiki.md": string(wikiRef)} {
		if !strings.Contains(text, en) {
			t.Errorf("%s lacks the rule to also write %q to the wiki", name, en)
		}
	}
}
