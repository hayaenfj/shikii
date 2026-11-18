package main

// Audit of 0.11.0 (2026-10-28): concrete inputs that break a property of .shikii/handoff/audit-0.11/invariants.md. Each test is written
// to fail on the audited HEAD; the report names the failure output.

import (
	"encoding/json"
	"fmt"
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
	if claude != 6 || codex != 6 {
		t.Fatalf("precondition: 6 hooks for Claude Code and 6 for Codex, got %d and %d", claude, codex)
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
		{"integrations/claude-code.md", "`~/.codex/hooks.json` does not change and Codex users need not trust the hooks again", "install adds SessionStart to ~/.codex/hooks.json (6 hooks)"},
		{"integrations/claude-code.md", "none is added to Codex", "Codex gets SubagentStart, SubagentStop and the PreToolUse of tool-lesson"},
		{"integrations/claude-code.ja.md", "\u3069\u308c\u3082 Codex \u306b\u306f\u8db3\u3055\u306a\u3044", "Codex gets SubagentStart, SubagentStop and the PreToolUse of tool-lesson"},
		{"docs/guide.md", "Codex has 3)", "Codex has 6 hooks"},
		{"docs/guide.md", "Codex does not get this hook", "Codex gets the PreToolUse of tool-lesson for its shell and apply_patch"},
		{"docs/guide.ja.md", "Codex \u306b\u306f\u8db3\u3055\u306a\u3044\u3002", "Codex gets the PreToolUse of tool-lesson for its shell and apply_patch"},
		{"docs/guide.md", "Codex retains turn-end checks without this command", "Codex has the command hook from 0.36.0"},
		{"docs/guide.ja.md", "\u3053\u306e\u30b3\u30de\u30f3\u30c9\u7528hook\u3092\u767b\u9332\u305b\u305a", "Codex has the command hook from 0.36.0"},
		{"README.md", "right before it runs in Claude Code.", "a guard on commands refuses in Codex too"},
		{"docs/guide.ja.md", "\u4eca\u307e\u3067\u3069\u304a\u308a Stop \u3060\u3051\u3067\u52d5\u304f", "the word guards also run before a publication command"},
		{"docs/guide.md", "for `forbidden-commands`, right before the command)", "the file and commit-body guards also refuse a publication command"},
		{"docs/guide.ja.md", "`forbidden-commands` \u306f\u30b3\u30de\u30f3\u30c9\u306e\u76f4\u524d\u306b\u76f4\u3055\u305b\u308b", "the file and commit-body guards also refuse a publication command"},
		{"docs/architecture.md", "runs the first three of these", "the plugin runs the hooks of a session's start, a request, a stop and a subagent"},
		{"docs/architecture.ja.md", "\u6700\u521d\u306e3\u3064\u3092\u52d5\u304b\u3059", "the plugin runs the hooks of a session's start, a request, a stop and a subagent"},
		{"docs/architecture.md", "which runs only those before a request", "the plugin runs the session's start too"},
		{"docs/architecture.ja.md", "\u30b5\u30d6\u30a8\u30fc\u30b8\u30a7\u30f3\u30c8\u306e\u958b\u59cb\u3068\u7d42\u4e86\u3060\u3051\u3092\u52d5\u304b\u3059", "the plugin runs the session's start too"},
		{"CHANGELOG.md", "reaches the model right before the command runs", "the agent reads the lesson with the call's result"},
		{"CHANGELOG_ja.md", "\u6559\u8a13\u304c\u30e2\u30c7\u30eb\u306b\u5c4a\u304f\u3088\u3046\u306b\u306a\u308a\u307e\u3057\u305f", "the agent reads the lesson with the call's result"},
		{"docs/readme/README_ja.md", "\u76f4\u524d\u306b\u3082\u5c4a\u304d\u3001\u30b5\u30d6\u30a8\u30fc\u30b8\u30a7\u30f3\u30c8\u306b\u306f\u3001\u983c\u307e\u308c\u305f\u4ed5\u4e8b\u306e\u6587\u306b\u8fd1\u3044\u6559\u8a13\u304c\u5c4a\u304d\u307e\u3059", "a Codex subagent gets no lesson chosen by its task"},
		{"skills/shikii/references/guards.md", "Codex retains its turn-end checks", "Codex has the command hook from 0.36.0"},
		{"skills/shikii/references/guards.md", "OpenCode uses MCP and skills without automatic hooks", "shikii's OpenCode plugin runs the hooks of a request, a stop and a subagent"},
		{"docs/guide.ja.md", "Claude Code \u304c\u30b3\u30de\u30f3\u30c9\u3092\u6d41\u3059\u76f4\u524d(Bash", "a forbidden-commands guard runs in Codex too"},
		{"integrations/claude-code.md", "only when a `forbidden-commands` guard the person enabled", "the file and commit-body guards also refuse a publication command"},
		{"integrations/claude-code.md", "an enabled forbidden-commands guard", "the file and commit-body guards also refuse a publication command"},
		{"integrations/claude-code.ja.md", "\u4eba\u304c\u6709\u52b9\u306b\u3057\u305f `forbidden-commands` \u306e\u691c\u67fb\u304c", "the file and commit-body guards also refuse a publication command"},
		{"integrations/claude-code.ja.md", "\u6709\u52b9\u306a forbidden-commands \u306e\u691c\u67fb", "the file and commit-body guards also refuse a publication command"},
		{"README.md", "and a subagent receives lessons close to the task it was given", "a Codex subagent gets no lesson chosen by its task"},
		{"skills/shikii/references/guards.md", "that same Claude Code Bash hook", "the hook before a command runs in Codex too"},
		{"docs/guide.ja.md", "Codex \u306f 3 \u3064)", "Codex has 6 hooks"},
		{"README.md", "Delivery to subagents is in Claude Code and OpenCode 2.x |", "Codex subagents get the rules for every request too"},
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
	// The numbers and the matchers the documents give are the ones of the list itself: a claim that names them is built from
	// hookEventsFor, so a document left behind when the list changes fails here whatever its wording around the number.
	says := []claim{
		{"integrations/claude-code.md", fmt.Sprintf("(%d for Claude Code, %d for Codex)", claude, codex), "hook uninstall removes the hooks shikii added"},
		{"integrations/claude-code.ja.md", fmt.Sprintf("(Claude Code %d \u3064\u3001Codex %d \u3064)", claude, codex), "hook uninstall removes the hooks shikii added"},
		{"integrations/claude-code.md", fmt.Sprintf("2. Adds %d to the `hooks` of", codex), "install adds that many to both agents"},
		{"integrations/claude-code.ja.md", fmt.Sprintf("\u306e `hooks` \u306b %d \u3064\u3092\u8db3\u3059", codex), "install adds that many to both agents"},
		{"docs/guide.md", fmt.Sprintf("(%d in both)", codex), "the guide counts the hooks"},
		{"docs/guide.ja.md", fmt.Sprintf("(\u3069\u3061\u3089\u3082 %d \u3064)", codex), "the guide counts the hooks"},
		{"integrations/claude-code.md", "`" + toolLessonMatcher + "` in Claude Code, `" + toolLessonMatcherCodex + "` in Codex", "the matchers of tool-lesson"},
		{"integrations/claude-code.ja.md", "Claude Code \u3067\u306f `" + toolLessonMatcher + "`\u3001Codex \u3067\u306f `" + toolLessonMatcherCodex + "`", "the matchers of tool-lesson"},
	}
	// the timeouts the document gives are the ones of the list: Stop has its own, every other hook has one and the same
	stopTimeout, otherTimeout := 0, 0
	for _, ev := range hookEventsFor("Claude Code") {
		switch {
		case ev.event == "Stop":
			stopTimeout = ev.timeout
		case otherTimeout == 0:
			otherTimeout = ev.timeout
		case ev.timeout != otherTimeout:
			t.Fatalf("precondition: every hook but Stop has the same timeout, got %d and %d", otherTimeout, ev.timeout)
		}
	}
	says = append(says,
		claim{"integrations/claude-code.md", fmt.Sprintf("Timeouts are %d seconds for Stop and %d seconds for every other hook.", stopTimeout, otherTimeout), "the timeouts of the hooks"},
		claim{"integrations/claude-code.ja.md", fmt.Sprintf("timeout \u306f Stop \u304c %d \u79d2\u3001\u307b\u304b\u306e hook \u306f\u5168\u90e8 %d \u79d2\u3002", stopTimeout, otherTimeout), "the timeouts of the hooks"},
	)
	// how much of a subagent's report is read, from the limits themselves
	says = append(says,
		claim{"docs/guide.md", fmt.Sprintf("wherever they stand in the last %d KiB of that report, for up to %d lessons", subagentReportMax>>10, subagentAssessMax), "the part of a subagent's report that is read"},
		claim{"docs/guide.ja.md", fmt.Sprintf("\u305d\u306e\u5831\u544a\u306e\u672b\u5c3e %d KiB \u306e\u3069\u3053\u306b\u3042\u3063\u3066\u3082\u305d\u306e\u884c\u3092\u4fdd\u5b58\u3059\u308b(\u6559\u8a13 %d \u4ef6\u307e\u3067)", subagentReportMax>>10, subagentAssessMax), "the part of a subagent's report that is read"},
	)
	for _, c := range says {
		if !strings.Contains(read(c.file), c.text) {
			t.Errorf("%s does not say %q (%s)", c.file, c.text, c.why)
		}
	}
}
