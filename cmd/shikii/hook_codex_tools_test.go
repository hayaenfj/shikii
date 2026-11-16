package main

import (
	"strings"
	"testing"
)

// Measured on Codex 0.169.0: every file change is one apply_patch call whose tool_input.command is the patch, with a header line per
// file; a path is absolute or relative to the folder Codex works in. The file of the patch is the first one with a name a lesson
// can be looked up by.
func TestPatchTargetIsTheFirstNamedFileOfThePatch(t *testing.T) {
	for name, c := range map[string]struct{ patch, cwd, want string }{
		"an update with an absolute path":                      {"*** Begin Patch\n*** Update File: /work/cmd/shikii/version.go\n@@\n-a\n+b\n*** End Patch", "/work", "/work/cmd/shikii/version.go"},
		"a relative path joins the folder":                     {"*** Begin Patch\n*** Add File: cmd/shikii/version.go\n+a\n*** End Patch", "/work", "/work/cmd/shikii/version.go"},
		"a delete":                                             {"*** Begin Patch\n*** Delete File: README.md\n*** End Patch", "/work", "/work/README.md"},
		"the first file with a real name":                      {"*** Begin Patch\n*** Add File: notes\n+a\n*** Update File: version.go\n@@\n-a\n+b\n*** End Patch", "/work", "/work/version.go"},
		"a header inside the added text":                       {"*** Begin Patch\n*** Add File: notes\n+*** Update File: version.go\n*** End Patch", "/work", ""},
		"a move target is not a file touched before the patch": {"*** Begin Patch\n*** Update File: notes\n*** Move to: version.go\n*** End Patch", "/work", ""},
		"no header":                        {"sed -i s/a/b/ version.go", "/work", ""},
		"a relative path without a folder": {"*** Begin Patch\n*** Update File: version.go\n*** End Patch", "", "version.go"},
	} {
		if got := patchTarget(c.patch, c.cwd); got != c.want {
			t.Errorf("%s: got %q, want %q", name, got, c.want)
		}
	}
}

// Measured on Codex 0.169.0 (its apply_patch run on these patches): a file header may be indented, except inside the section of an
// updated file, where an indented line is a line of that file. shikii reads a patch the same way, so the file it looks lessons up
// for is the file Codex changes.
func TestPatchHeadersAreReadAsCodexReadsThem(t *testing.T) {
	for name, c := range map[string]struct{ patch, want string }{
		"an indented first header":                {"*** Begin Patch\n  *** Update File: version.go\n@@\n-a\n+b\n*** End Patch", "/work/version.go"},
		"an indented header after an added file":  {"*** Begin Patch\n*** Add File: notes\n+a\n  *** Update File: version.go\n@@\n-a\n+b\n*** End Patch", "/work/version.go"},
		"a tab before a header after a delete":    {"*** Begin Patch\n*** Delete File: notes\n\t*** Update File: version.go\n@@\n-a\n+b\n*** End Patch", "/work/version.go"},
		"an indented line of an updated file":     {"*** Begin Patch\n*** Update File: notes\n@@\n-a\n+b\n  *** Update File: version.go\n*** End Patch", ""},
		"a header at the line's head ends it":     {"*** Begin Patch\n*** Update File: notes\n@@\n-a\n+b\n*** Add File: ab\n+a\n  *** Update File: version.go\n@@\n-a\n+b\n*** End Patch", "/work/version.go"},
		"an indented header with CRLF":            {"*** Begin Patch\r\n  *** Update File: version.go  \r\n@@\r\n-a\r\n+b\r\n*** End Patch", "/work/version.go"},
		"an indented line of an updated file, CR": {"*** Begin Patch\r\n*** Update File: notes\r\n@@\r\n-a\r\n+b\r\n  *** Update File: version.go\r\n*** End Patch", ""},
	} {
		if got := patchTarget(c.patch, "/work"); got != c.want {
			t.Errorf("%s: got %q, want %q", name, got, c.want)
		}
	}
}

// The lesson that names a file arrives right before Codex patches that file, and right before a command through Codex's shell (it
// is reported as Bash, with the command in tool_input.command, as in Claude Code). A tool this hook does not look at gets nothing.
func TestToolLessonComesBeforeACodexPatchAndCommand(t *testing.T) {
	_, scope, _, ids := toolLessonFixture(t)
	patch := "*** Begin Patch\n*** Update File: cmd/shikii/version.go\n@@\n-const version = \"1\"\n+const version = \"2\"\n*** End Patch"
	out := toolHook(t, scope, "codex", toolPrompt, "apply_patch", map[string]any{"command": patch})
	if ctx := feedbackContext(t, out); !strings.Contains(ctx, ids["version"]) || !strings.Contains(ctx, "apply_patch cmd/shikii/version.go") {
		t.Fatalf("the lesson naming the patched file arrives, with the tool and the file: %s", out)
	}
	if out := toolHook(t, scope, "codex", "", "Bash", map[string]any{"command": "git commit -m x"}); !strings.Contains(out, ids["commit"]) {
		t.Fatalf("the lesson naming the command arrives before Codex's shell runs it: %s", out)
	}
	for name, in := range map[string]map[string]any{
		"a patch that names no file a lesson knows": {"command": "*** Begin Patch\n*** Add File: notes\n+a\n*** End Patch"},
		"a patch that is not a string":              {"command": 7},
		"no patch":                                  {},
	} {
		if out := toolHook(t, scope, "codex-other", toolPrompt, "apply_patch", in); out != "" {
			t.Errorf("%s: nothing is delivered: %s", name, out)
		}
	}
	if out := toolHook(t, scope, "codex-third", toolPrompt, "collaborationspawn_agent", map[string]any{"command": patch}); out != "" {
		t.Errorf("a tool that changes no file and runs no command gets nothing: %s", out)
	}
}

// Measured on Codex 0.169.0: a subagent's PreToolUse carries the parent's session and the subagent's agent_id, its start carries no
// task, and its stop carries its last message. The lesson delivered right before the subagent's patch is the subagent's own: it
// reports it, and the parent's reflection is not asked for it.
func TestACodexSubagentAssessesTheLessonDeliveredBeforeItsPatch(t *testing.T) {
	st, scope, _, ids := toolLessonFixture(t)
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "codex", "cwd": scope, "prompt": toolPrompt})
	start := subagentContext(t, runHook(t, "subagent-start", map[string]any{"session_id": "codex", "cwd": scope, "agent_id": "thread-1", "agent_type": "default"}))
	if !strings.Contains(start, "shikii: <its key> unused") || strings.Contains(start, ids["version"]) {
		t.Fatalf("with no task to choose by, the subagent is told how to report and gets no lesson chosen by a task: %s", start)
	}
	patch := "*** Begin Patch\n*** Update File: " + scope + "/cmd/shikii/version.go\n@@\n-a\n+b\n*** End Patch"
	out := runHook(t, "tool-lesson", map[string]any{"session_id": "codex", "cwd": scope, "agent_id": "thread-1", "agent_type": "default", "hook_event_name": "PreToolUse", "tool_name": "apply_patch", "tool_input": map[string]any{"command": patch}})
	if !strings.Contains(out, ids["version"]) {
		t.Fatalf("precondition: the lesson arrives before the subagent's patch: %s", out)
	}
	subagentStop(t, "codex", scope, "thread-1", "Bumped the version.\nshikii: "+ids["version"]+" helpful - Updated the README badges together with the version.")
	var got []string
	report, err := st.MemoryReport(scope)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range report.All {
		if item.Agent == "thread-1" && item.Assessment != nil && item.Quote.Key == ids["version"] {
			got = append(got, item.Assessment.Result+" "+item.Assessment.Phrase+" "+item.Assessment.Agent)
		}
	}
	if len(got) != 1 || got[0] != "helpful version.go thread-1" {
		t.Fatalf("the subagent's report is saved for the delivery before its patch, through the file's door: %v", got)
	}
}
