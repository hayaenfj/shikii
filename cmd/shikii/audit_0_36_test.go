package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The audit of 0.36.0 (properties P5 to P12). Line endings: Codex reads a patch line by line and takes a CR before the LF off.
func TestPatchTargetReadsHeadersThatEndWithCRLF(t *testing.T) {
	all := "*** Begin Patch\r\n*** Update File: version.go\r\n@@\r\n-a\r\n+b\r\n*** End Patch\r\n"
	if got := patchTarget(all, "/work"); got != "/work/version.go" {
		t.Errorf("every line ends with CRLF: got %q, want /work/version.go", got)
	}
	mixed := "*** Begin Patch\n*** Update File: version.go\r\n@@\n-a\n+b\n*** Update File: README.md\n@@\n-a\n+b\n*** End Patch\n"
	if got := patchTarget(mixed, "/work"); got != "/work/version.go" {
		t.Errorf("the first header ends with CRLF: got %q, want /work/version.go", got)
	}
}

// P10, a patch longer than patchMax whose header is cut in the middle of the file's name.
func TestPatchTargetDoesNotReadAHeaderCutAtTheLimit(t *testing.T) {
	head := "*** Begin Patch\n*** Add File: notes\n"
	keep := "*** Update File: version.go"
	tail := keep + ".bak\n@@\n-a\n+b\n*** End Patch\n"
	want := patchMax - len(head) - len(keep)
	line := "+" + strings.Repeat("a", 62) + "\n"
	body := strings.Repeat(line, want/len(line))
	if rem := want - len(body); rem == 1 {
		body += "\n"
	} else if rem >= 2 {
		body += "+" + strings.Repeat("a", rem-2) + "\n"
	}
	patch := head + body + tail
	if len(head)+len(body)+len(keep) != patchMax {
		t.Fatalf("precondition: the limit falls right after version.go: %d", len(head)+len(body)+len(keep))
	}
	if got := patchTarget(patch, "/work"); got == "/work/version.go" {
		t.Errorf("the patch touches notes and version.go.bak, never version.go: got %q", got)
	} else {
		t.Logf("got %q", got)
	}
}

// P5, the report's line endings and the cap of 16 lines.
func TestSubagentStopReadsCRLFReportsAndPastUndeliveredLines(t *testing.T) {
	st, scope, key, _, _ := subagentStopFixture(t, "s", "agent-1")
	subagentStop(t, "s", scope, "agent-1", "Read the files.\r\nshikii: "+key+" unused\r\n")
	if got := subagentAssessments(t, st, scope, "agent-1"); len(got) != 1 || got[0] == nil {
		t.Errorf("a report with CRLF line endings: the unused line is not saved: %+v", got)
	}
	var b strings.Builder
	for i := 0; i < 16; i++ {
		b.WriteString("shikii: lesson:0123456789abcdef:0123456789abcde" + string("0123456789abcdef"[i]) + " unused\n")
	}
	b.WriteString("shikii: " + key + " unused\n")
	subagentStop(t, "s", scope, "agent-1", b.String())
	if got := subagentAssessments(t, st, scope, "agent-1"); len(got) != 1 || got[0] == nil {
		t.Errorf("16 lines of keys that were not delivered come first: the delivered one is not saved: %+v", got)
	}
}

// P5/P6, the same lesson reaches one subagent at its start and right before its edit; its one line assesses one of them.
func TestOneLineAssessesEveryDeliveryOfThatLessonToTheSubagent(t *testing.T) {
	st, scope, _, ids := toolLessonFixture(t)
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "s", "cwd": scope, "prompt": toolPrompt})
	task := "Bump the README badges with the version when bumping shikii's version"
	ctx := subagentContext(t, startSubagent(t, "s", scope, "agent-1", "general-purpose", task))
	if !strings.Contains(ctx, ids["version"]) {
		t.Skipf("the start did not deliver the version lesson: %s", ctx)
	}
	out := runHook(t, "tool-lesson", map[string]any{"session_id": "s", "cwd": scope, "agent_id": "agent-1", "agent_type": "general-purpose", "hook_event_name": "PreToolUse", "tool_name": "Edit", "tool_input": map[string]any{"file_path": filepath.Join(scope, "cmd/shikii/version.go")}})
	if !strings.Contains(out, ids["version"]) {
		t.Skipf("the tool call did not deliver the version lesson again: %s", out)
	}
	subagentStop(t, "s", scope, "agent-1", "shikii: "+ids["version"]+" helpful - Updated the badges.")
	got := subagentAssessments(t, st, scope, "agent-1")
	left := 0
	for _, a := range got {
		if a == nil {
			left++
		}
	}
	rep, _ := st.MemoryReport(scope)
	if left != 0 {
		t.Errorf("the subagent reported the lesson, yet %d of its %d deliveries of that lesson stay unassessed for good (status subagent=%d)", left, len(got), rep.Stats.Subagent)
	}
}

// P9, a Codex hooks.json that already has shikii's tool-lesson under another matcher.
func TestInstallRepairsTheMatcherOfShikiisOwnHook(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hooks.json")
	var cmd string
	for _, ev := range hookEventsFor("Codex") {
		if strings.HasSuffix(ev.cmd, " hook tool-lesson") {
			cmd = ev.cmd
		}
	}
	body, _ := json.Marshal(map[string]any{"hooks": map[string]any{"PreToolUse": []any{map[string]any{"matcher": toolLessonMatcher, "hooks": []any{map[string]any{"type": "command", "command": cmd, "timeout": hookTimeout}}}}}})
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := editHooks(path, true, hookEventsFor("Codex")); err != nil {
		t.Fatal(err)
	}
	var cfg map[string]any
	if err := json.Unmarshal(mustRead(t, path), &cfg); err != nil {
		t.Fatal(err)
	}
	for _, g := range cfg["hooks"].(map[string]any)["PreToolUse"].([]any) {
		gm := g.(map[string]any)
		for _, h := range gm["hooks"].([]any) {
			if h.(map[string]any)["command"] == cmd && gm["matcher"] != toolLessonMatcherCodex {
				t.Errorf("after install, Codex's tool-lesson has the matcher %v, not %s", gm["matcher"], toolLessonMatcherCodex)
			}
		}
	}
}

// P9/P12, a Codex hooks.json with a person's hooks, hooks of an older path and of an event Codex does not get.
func TestCodexInstallAndUninstallKeepThePersonsHooks(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hooks.json")
	group := func(matcher string, cmds ...string) map[string]any {
		hs := []any{}
		for _, c := range cmds {
			hs = append(hs, map[string]any{"type": "command", "command": c})
		}
		g := map[string]any{"hooks": hs}
		if matcher != "" {
			g["matcher"] = matcher
		}
		return g
	}
	body, _ := json.Marshal(map[string]any{
		"other": map[string]any{"kept": true},
		"hooks": map[string]any{
			"PreToolUse":   []any{group("^Bash$", "/usr/local/bin/mytool check", "/old/path/shikii hook tool-lesson"), group("Agent|Task", "/old/path/shikii hook agent-task"), group("", "/old/path/shikii hook pre-tool-use")},
			"SubagentStop": []any{group("", "echo done", "/old/path/shikii hook subagent-stop")},
			"Stop":         []any{group("", "~/bin/shikii hook stop"), group("", "/old/path/shikii hook stop")},
			"Notification": []any{group("", "say hi")},
		}})
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	commands := func() (shikiis, others []string, matchers map[string]string) {
		var cfg map[string]any
		if err := json.Unmarshal(mustRead(t, path), &cfg); err != nil {
			t.Fatal(err)
		}
		if _, ok := cfg["other"]; !ok {
			t.Errorf("another key of the file is gone")
		}
		matchers = map[string]string{}
		hooks, _ := cfg["hooks"].(map[string]any)
		for _, v := range hooks {
			for _, g := range v.([]any) {
				gm := g.(map[string]any)
				for _, h := range gm["hooks"].([]any) {
					c := h.(map[string]any)["command"].(string)
					m, _ := gm["matcher"].(string)
					matchers[c] = m
					if isShikiiHook(c) {
						shikiiHooks := c
						shikiis = append(shikiis, shikiiHooks)
					} else {
						others = append(others, c)
					}
				}
			}
		}
		return
	}
	if _, err := editHooks(path, true, hookEventsFor("Codex")); err != nil {
		t.Fatal(err)
	}
	shikiis, others, matchers := commands()
	if len(shikiis) != 6 {
		t.Errorf("after install Codex has 6 shikii hooks, got %d: %v", len(shikiis), shikiis)
	}
	for _, c := range shikiis {
		if strings.HasPrefix(c, "/old/path/") || strings.HasSuffix(c, " hook agent-task") {
			t.Errorf("an older path's hook or agent-task stays: %s", c)
		}
		if strings.HasSuffix(c, " hook tool-lesson") && matchers[c] != toolLessonMatcherCodex {
			t.Errorf("tool-lesson has the matcher %q", matchers[c])
		}
	}
	if len(others) != 4 || matchers["/usr/local/bin/mytool check"] != "^Bash$" {
		t.Errorf("the person's four hooks stay as they are: %v %v", others, matchers)
	}
	if _, err := editHooks(path, false, hookEventsFor("Codex")); err != nil {
		t.Fatal(err)
	}
	shikiis, others, matchers = commands()
	if len(shikiis) != 0 {
		t.Errorf("after uninstall no shikii hook stays: %v", shikiis)
	}
	if len(others) != 4 || matchers["/usr/local/bin/mytool check"] != "^Bash$" {
		t.Errorf("the person's four hooks stay after uninstall: %v %v", others, matchers)
	}
}

// P5, a line for a revision that was not delivered leaves no assessment file at all (the report lists only delivered keys,
// so it cannot see a file saved under another key).
func TestALineForARevisionNotDeliveredLeavesNoAssessmentFile(t *testing.T) {
	st, scope, key, _, _ := subagentStopFixture(t, "s", "agent-1")
	other := key[:len(key)-1] + "0"
	if other == key {
		other = key[:len(key)-1] + "1"
	}
	subagentStop(t, "s", scope, "agent-1", "shikii: "+other+" helpful - Followed it.")
	all, err := st.MemoryAssessments(scope)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range all {
		if a.Key == other {
			t.Errorf("an assessment was saved for a revision that was not delivered: %+v", a)
		}
	}
}

// P12, a tool-lesson that fails adds nothing and refuses nothing.
func TestAFailingToolLessonAddsNothingAndRefusesNothing(t *testing.T) {
	st, scope, _, ids := toolLessonFixture(t)
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "s", "cwd": scope, "prompt": toolPrompt})
	var dirs []string
	filepath.WalkDir(st.Dir, func(p string, d os.DirEntry, err error) error {
		if err == nil && d.IsDir() {
			dirs = append(dirs, p)
		}
		return nil
	})
	for _, d := range dirs {
		os.Chmod(d, 0o500)
	}
	t.Cleanup(func() {
		for _, d := range dirs {
			os.Chmod(d, 0o755)
		}
	})
	// whether the failure can be made is asked of the folder, not of the hook's output: a hook that adds the lesson anyway fails below
	if probe := filepath.Join(st.Dir, "probe"); os.WriteFile(probe, nil, 0o600) == nil {
		os.Remove(probe)
		t.Skip("this user writes into read-only folders, so the store cannot be made to fail")
	}
	out := runHook(t, "tool-lesson", map[string]any{"session_id": "s", "cwd": scope, "hook_event_name": "PreToolUse", "tool_name": "apply_patch", "tool_input": map[string]any{"command": "*** Begin Patch\n*** Update File: cmd/shikii/version.go\n@@\n-a\n+b\n*** End Patch"}})
	_ = ids
	if log, _ := os.ReadFile(filepath.Join(filepath.Dir(st.Dir), "hook.log")); len(log) > 0 {
		t.Logf("hook.log: %s", log)
	}
	if out != "" {
		t.Errorf("a failing tool-lesson outputs nothing: %s", out)
	}
}

// A subagent that runs in the background can end after the person's next request. Its report is still about the lessons delivered
// to it, so it is saved: a delivery is tied to the subagent, not to the turn that happens to be open when the subagent ends.
func TestASubagentThatEndsAfterThePersonsNextRequestStillAssessesItsDeliveries(t *testing.T) {
	st, scope, key, _, _ := subagentStopFixture(t, "s", "agent-1")
	memoryEffectComplete(t, st, scope, "s") // the parent ends its turn, and the person's next request opens another
	subagentStop(t, "s", scope, "agent-1", "Done.\nshikii: "+key+" helpful - Checked the uninitialized folder before recording.")
	if got := subagentAssessments(t, st, scope, "agent-1"); len(got) != 1 || got[0] == nil || got[0].Result != "helpful" {
		t.Fatalf("the report of a subagent that ends after the next request is saved for its delivery: %+v", got)
	}
	// another subagent's line is still not taken for this one's delivery
	subagentStop(t, "s", scope, "agent-2", "shikii: "+key+" unused")
	if got := subagentAssessments(t, st, scope, "agent-2"); len(got) != 0 {
		t.Fatalf("a subagent that received nothing assesses nothing: %+v", got)
	}
}

// What a subagent reports about a lesson delivered right before its command or file is also the answer to the delivery's own
// question (lesson-applies): helpful is yes, unused is no. Before, only the parent's reflection gave that answer, and it no longer
// lists the subagent's deliveries.
func TestASubagentsReportAnswersTheQuestionOfItsDelivery(t *testing.T) {
	st, scope, _, ids := toolLessonFixture(t)
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "codex", "cwd": scope, "prompt": toolPrompt})
	patch := "*** Begin Patch\n*** Update File: " + scope + "/cmd/shikii/version.go\n@@\n-a\n+b\n*** End Patch"
	out := runHook(t, "tool-lesson", map[string]any{"session_id": "codex", "cwd": scope, "agent_id": "thread-1", "agent_type": "default", "hook_event_name": "PreToolUse", "tool_name": "apply_patch", "tool_input": map[string]any{"command": patch}})
	if !strings.Contains(out, ids["version"]) {
		t.Fatalf("precondition: the lesson arrives before the subagent's patch: %s", out)
	}
	trace := lastToolDelivery(t, st, scope).Quotes[0].Trace
	if trace == "" {
		t.Fatalf("precondition: the delivery carries the trace of its lesson-applies decision")
	}
	subagentStop(t, "codex", scope, "thread-1", "shikii: "+ids["version"]+" helpful - Updated the README badges together with the version.")
	labeled, err := st.LabeledTraces("lesson-applies")
	if err != nil {
		t.Fatal(err)
	}
	if got := labeled[trace].Truth; got != "yes" {
		t.Fatalf("the subagent found the lesson helpful, so the truth of its lesson-applies decision is yes, got %q", got)
	}
}
