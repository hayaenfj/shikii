package main

// Audit of 0.11.0 (2026-10-28): concrete inputs that break a property of .shikii/handoff/audit-0.11/invariants.md. Each test is written
// to fail on the audited HEAD; the report names the failure output.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// auditToolCall runs the PreToolUse hook for one tool call in session and returns the raw output and its hookSpecificOutput.
func auditToolCall(t *testing.T, scope, session, cwd, tool string, input map[string]any) (string, map[string]any) {
	t.Helper()
	out := runHook(t, "tool-lesson", map[string]any{"session_id": session, "cwd": cwd, "hook_event_name": "PreToolUse", "tool_name": tool, "tool_input": input})
	var v map[string]any
	if out != "" {
		if err := json.Unmarshal([]byte(out), &v); err != nil {
			t.Fatalf("one JSON object: %v\n%s", err, out)
		}
	}
	spec, _ := v["hookSpecificOutput"].(map[string]any)
	return out, spec
}

// Property 15: the documents do not contradict what the hooks and guards do. Each statement below was read from the files at the
// audited HEAD; the number of hooks comes from hookEventsFor, the kinds from guardKinds.
func TestAudit011DocsMatchTheHooksAndTheGuards(t *testing.T) {
	read := func(path string) string {
		t.Helper()
		b, err := os.ReadFile(filepath.Join("..", "..", path))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	claude, codex := len(hookEventsFor("Claude Code")), len(hookEventsFor("Codex"))
	if claude != 6 || codex != 3 {
		t.Fatalf("precondition: 6 hooks for Claude Code and 3 for Codex, got %d and %d", claude, codex)
	}
	if n := strings.Count(guardKinds, "|") + 1; n != 4 {
		t.Fatalf("precondition: 4 kinds of guard, got %d", n)
	}
	codexHasSessionStart := false
	for _, ev := range hookEventsFor("Codex") {
		codexHasSessionStart = codexHasSessionStart || ev.event == "SessionStart"
	}
	if !codexHasSessionStart {
		t.Fatal("precondition: install adds SessionStart to Codex, so ~/.codex/hooks.json changes")
	}
	type claim struct {
		file, text, why string
	}
	stale := []claim{
		{"integrations/claude-code.md", "There is no hook per tool call (right before Bash or Edit).", "hook tool-lesson runs at PreToolUse of Edit/Write/MultiEdit/NotebookEdit/Bash"},
		{"integrations/claude-code.md", "It never stops a tool.", "an enabled forbidden-words or forbidden-commands guard returns permissionDecision deny"},
		{"integrations/claude-code.md", "**It does not stop tools. It does not give permission decisions (allow / ask / deny) either.**", "an enabled guard returns permissionDecision deny at PreToolUse"},
		{"integrations/claude-code.ja.md", "\u30c4\u30fc\u30eb\u306f\u4e00\u5207\u6b62\u3081\u306a\u3044", "an enabled guard returns permissionDecision deny at PreToolUse"},
		{"integrations/claude-code.md", "`~/.codex/hooks.json` does not change and Codex users need not trust the hooks again", "install adds SessionStart to ~/.codex/hooks.json (3 hooks)"},
		{"integrations/claude-code.md", "and for Claude Code 3 more", "Claude Code gets 4 more than the 2 shared ones (SessionStart, SubagentStart, PreToolUse x2), or the 2 are 3 (SessionStart is shared)"},
		{"integrations/claude-code.md", "nothing is added to tool calls", "hook tool-lesson runs at PreToolUse of Edit/Write/MultiEdit/NotebookEdit/Bash"},
		{"integrations/claude-code.md", "There are 3 kinds, `paired-files`", "there are 4 kinds (forbidden-commands)"},
		{"integrations/claude-code.md", "Never allows, denies\n                                                     or asks. Claude Code only", "the same hook returns permissionDecision deny for an enabled guard"},
		{"integrations/claude-code.ja.md", "\u30c4\u30fc\u30eb\u547c\u3073\u51fa\u3057\u3054\u3068(Bash \u3084 Edit \u306e\u76f4\u524d)\u306e hook \u306f\u6301\u305f\u306a\u3044", "hook tool-lesson runs before Edit and Bash"},
		{"integrations/claude-code.ja.md", "`~/.codex/hooks.json` \u306f\u5909\u308f\u3089\u306a\u3044\u306e\u3067\u3001Codex \u3067 hook \u306e\u4fe1\u983c\u3092\u3084\u308a\u76f4\u3059\u5fc5\u8981\u306f\u7121\u3044", "install adds SessionStart to ~/.codex/hooks.json"},
		{"integrations/claude-code.ja.md", "\u30c4\u30fc\u30eb\u547c\u3073\u51fa\u3057\u306b\u306f\u4f55\u3082\u8db3\u3055\u308c\u306a\u3044", "hook tool-lesson runs at PreToolUse"},
		{"integrations/claude-code.ja.md", "\u8a31\u53ef\u306e\u5224\u5b9a(allow / ask / deny)\u3082\u51fa\u3055\u306a\u3044", "an enabled guard returns permissionDecision deny at PreToolUse"},
		{"integrations/claude-code.ja.md", "`commit-body`(\u305d\u306e\u30bf\u30fc\u30f3\u306e\u30b3\u30df\u30c3\u30c8\u306e\u672c\u6587\u306b\u542b\u3080\u8a9e)\u306e 3 \u3064\u3067", "there are 4 kinds"},
		{"integrations/claude-code.ja.md", "\u8a31\u53ef\u3082\u62d2\u5426\u3082\u78ba\u8a8d\u3082\u3057\u306a\u3044\u3002Claude Code \u3060\u3051", "the same hook returns permissionDecision deny for an enabled guard"},
		{"skills/shikii/SKILL.md", "not before every tool", "hook tool-lesson runs before every edit and command"},
		{"docs/guide.md", "both only make the agent fix the work at the end of the turn", "forbidden-words and forbidden-commands guards refuse at PreToolUse"},
		{"docs/guide.ja.md", "\u3069\u3061\u3089\u3082\u30bf\u30fc\u30f3\u306e\u7d42\u308f\u308a\u306b\u76f4\u3055\u305b\u308b\u3060\u3051", "forbidden-words and forbidden-commands guards refuse at PreToolUse"},
	}
	for _, c := range stale {
		if strings.Contains(read(c.file), c.text) {
			t.Errorf("%s still says %q, but %s", c.file, c.text, c.why)
		}
	}
}
