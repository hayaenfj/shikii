package main

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// 0.11: what every request puts in front of the agent is knowledge (this project's lessons and the person's decisions), and the mechanics
// (the shape of hook plan submit and reflect submit) come back in the tool's own reply at the moment they are needed. Measured on
// 2026-10-28 before this: the organizing notice was 855 bytes (1,900 in its full form), the reflection notice about 1,300 and the MCP
// guidance 1,248, all of them procedure, while the lessons themselves were one or two quotes of about 270 characters each.
func TestEveryRequestTextIsShortAndTheReplyCarriesTheShape(t *testing.T) {
	exe := regexp.MustCompile(`'[^']*'`) // the binary's path is named by the notices: taken out before measuring
	notice := exe.ReplaceAllString(planningNotice("s", "0123456789abcdef"), "'shikii'")
	if len(notice) > 560 {
		t.Errorf("the organizing notice is %d bytes, over 560: %q", len(notice), notice)
	}
	for _, w := range []string{"hook plan query", "hook plan submit", "how_to_submit", "topic words in query", "rule, test or procedure", `{"session_id":"s","turn_id":"0123456789abcdef"}`} {
		if !strings.Contains(notice, w) {
			t.Errorf("the organizing notice lacks %q: %q", w, notice)
		}
	}
	for _, w := range []string{"lesson:<id>:<revision>", "history:<id>:user", "continues_previous", "omit it when unsure", "person's own words", "{unknown:true}", "the skill's references/planning.md", "never permission"} {
		if !strings.Contains(planningHowToSubmit, w) {
			t.Errorf("how_to_submit of hook plan query lacks %q", w)
		}
	}
	ask := exe.ReplaceAllString(reflectAsk("'shikii' reflect", []string{"0123456789abcdef"}), "'shikii'")
	if len(ask) > 520 {
		t.Errorf("the reflection notice is %d bytes, over 520: %q", len(ask), ask)
	}
	for _, w := range []string{"reflect query", "how_to_submit", "rule, test or procedure", `{"sources":["0123456789abcdef"]}`} {
		if !strings.Contains(ask, w) {
			t.Errorf("the reflection notice lacks %q: %q", w, ask)
		}
	}
	for _, w := range []string{"reviews:[", "wiki:[", "memory:[", "maintenance", "correction:{", "shikii guard add", "the latest page_id", "Read the sources and existing pages",
		"memories[].source_id", "check result", "observations", "complete:true", "fix it and resend", "the skill's references/wiki.md", "operation:place"} {
		if !strings.Contains(reflectHowToSubmit, w) {
			t.Errorf("how_to_submit of reflect query lacks %q", w)
		}
	}
	if len(memoryPrefix) > 220 {
		t.Errorf("the memory prefix is %d bytes, over 220: %q", len(memoryPrefix), memoryPrefix)
	}
	if len(mcpInstructions) > 1000 || strings.Index(mcpInstructions, "wiki") > strings.Index(mcpInstructions, "shikii_decide") {
		t.Errorf("the MCP guidance is %d bytes (over 1000) or puts the decision tools before the project's memory: %q", len(mcpInstructions), mcpInstructions)
	}
}

// The reply of hook plan query carries how_to_submit, and the reply of reflect query (the CLI and shikii_reflect share reflectQuery) too.
func TestQueryRepliesCarryHowToSubmit(t *testing.T) {
	skillTexts := map[string]string{}
	for _, path := range []string{"../../skills/shikii/references/planning.md", "../../skills/shikii/references/wiki.md"} { // read before the fixture moves the working directory
		text, _ := os.ReadFile(path)
		skillTexts[path] = string(text)
	}
	repo, root, _ := planningFixture(t)
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": planningTestRequest})
	q, err := planningCall(t, "query", map[string]any{"session_id": "planning", "turn_id": loadSession(root, "planning").TurnID, "query": "CSV columns"})
	if err != nil {
		t.Fatal(err)
	}
	if q["how_to_submit"] != planningHowToSubmit {
		t.Fatalf("hook plan query returns how_to_submit: %v", q["how_to_submit"])
	}
	view, err := reflectQuery(root, feedbackScope(repo))
	if err != nil {
		t.Fatal(err)
	}
	if view.HowToSubmit != reflectHowToSubmit {
		t.Fatalf("reflect query returns how_to_submit: %q", view.HowToSubmit)
	}
	// the skill names the field, so an agent reading the skill knows where the shape comes from
	for path, text := range skillTexts {
		if !strings.Contains(text, "how_to_submit") {
			t.Errorf("%s names how_to_submit", path)
		}
	}
}
