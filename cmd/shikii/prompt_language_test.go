package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode"

	"github.com/hayaenfj/shikii/internal/decide"
	"github.com/hayaenfj/shikii/internal/spec"
)

func hasJapaneseInstruction(s string) bool {
	return strings.IndexFunc(s, func(r rune) bool { return unicode.In(r, unicode.Han, unicode.Hiragana, unicode.Katakana) }) >= 0
}

func TestAgentPlanningAdviceUsesEnglishWithJapaneseDisplay(t *testing.T) {
	sp, err := spec.Load(filepath.Join("..", "..", "integrations", "claude-code", "specs", "needs-plan.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	d := &decide.Decision{TraceID: "0123456789abcdef", Band: decide.Act, Top: "yes", P: 0.9, IsCalibrated: true}
	loc := localize(sp, d, "ja")
	advice := hookOutcome("needs-plan", d, loc)
	if advice.Context == "" || hasJapaneseInstruction(advice.Context) {
		t.Fatal("agent advice must be English regardless of the display preference", advice.Context)
	}
	if !hasJapaneseInstruction(advice.message()) {
		t.Fatal("the page's record must retain the localized explanation", advice.message())
	}
	if hasJapaneseInstruction(string(advice.json("UserPromptSubmit"))) {
		t.Fatal("a page-only translation leaked into the agent's hook JSON")
	}
	d.Band = decide.Ask
	if notice := hookOutcome("needs-plan", d, loc); !hasJapaneseInstruction(notice.System) {
		t.Fatal("the person's notice must retain its configured language", notice.System)
	}
}

func TestAgentGuardRefusalsKeepEnglishInstructionsAndLocalizedRecords(t *testing.T) {
	for _, mode := range []string{"command", "stop"} {
		t.Run(mode, func(t *testing.T) {
			st, scope, _, _ := toolLessonFixture(t)
			setHookLanguage(t, "Japanese")
			g := guard{ID: "english-advice", Kind: guardKindForbiddenCommands, Title: "A synthetic check", Patterns: []string{"blocked-command"}, Example: "blocked-command"}
			if mode == "stop" {
				g.Kind, g.Patterns, g.Example = guardKindPairedFiles, nil, ""
				g.Pairs = [][]string{{"README.md", "partner.md"}}
			}
			writeGuard(t, st.Dir, g)
			runHook(t, "user-prompt-submit", map[string]any{"session_id": "english-advice", "cwd": scope, "prompt": "Update the README"})
			var out string
			if mode == "command" {
				out = runHook(t, "tool-lesson", map[string]any{"session_id": "english-advice", "cwd": scope, "hook_event_name": "PreToolUse", "tool_name": "Bash", "tool_input": map[string]any{"command": "blocked-command"}})
			} else {
				if err := os.WriteFile(filepath.Join(scope, "README.md"), []byte("Changed synthetic content.\n"), 0600); err != nil {
					t.Fatal(err)
				}
				out = runHook(t, "stop", map[string]any{"session_id": "english-advice", "cwd": scope, "last_assistant_message": "Finished."})
			}
			var response map[string]any
			if err := json.Unmarshal([]byte(out), &response); err != nil {
				t.Fatal(err, out)
			}
			reason, _ := response["reason"].(string)
			if mode == "command" {
				hook, _ := response["hookSpecificOutput"].(map[string]any)
				reason, _ = hook["permissionDecisionReason"].(string)
			}
			if !strings.Contains(reason, g.ID) || hasJapaneseInstruction(reason) {
				t.Fatal("the agent must receive the English refusal and checked evidence", reason)
			}
			records := guardRecordsFor(st.Dir, g.ID)
			if len(records) == 0 || !hasJapaneseInstruction(records[len(records)-1].Reason) {
				t.Fatal("the page's refusal record must keep the configured language", records)
			}
		})
	}
}

func TestAgentDecisionMemoryUsesEnglishScaffoldingAndPreservesEvidence(t *testing.T) {
	sp, err := spec.Load(filepath.Join("..", "..", "integrations", "claude-code", "specs", "needs-plan.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	original := "\u5225\u306e\u6761\u4ef6\u306f\u672a\u78ba\u8a8d\u3002"
	text := personAnswerSaid(sp, "yes", "ja", &aiPair{Agent: "no", AgentReason: original}, modelAnswer{Answer: "no"})
	if strings.Contains(text, "\u4f3c\u305f\u4f9d\u983c") || strings.Contains(text, "\u5224\u5b9a\u30e2\u30c7\u30eb\u306f") {
		t.Fatal("generated memory scaffolding must use English", text)
	}
	for _, want := range []string{"In a similar request", "the agent had answered", "the decision model said", original, jaShouldHaveAsked, jaAskFirstTitle} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing evidence or attribution %q: %s", want, text)
		}
	}
}
