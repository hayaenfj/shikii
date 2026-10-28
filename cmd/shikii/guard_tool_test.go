package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 0.11: a guard the person enabled stops a change right before it is written, not at the end of the turn: a forbidden word in what Edit,
// Write, MultiEdit or NotebookEdit is about to add is refused (PreToolUse permissionDecision deny) with the reason, so the agent fixes
// the text and changes the file again in the same breath (the person, 2026-10-28: improve during the work, not at the end of the turn).
func TestEnabledForbiddenWordsGuardStopsTheChangeBeforeItIsWritten(t *testing.T) {
	st, scope, _, _ := toolLessonFixture(t)
	writeGuard(t, st.Dir, guard{ID: "no-ne", Kind: guardKindForbiddenWords, Title: "根を使わない", Words: []string{"根"}, Except: []string{"docs/operations.md"}})
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "g", "cwd": scope, "prompt": "README に置き場の説明を足して"})
	call := func(tool string, input map[string]any) (string, map[string]any) {
		t.Helper()
		out := runHook(t, "tool-lesson", map[string]any{"session_id": "g", "cwd": scope, "hook_event_name": "PreToolUse", "tool_name": tool, "tool_input": input})
		var v map[string]any
		if out != "" {
			if err := json.Unmarshal([]byte(out), &v); err != nil {
				t.Fatalf("one JSON object: %v\n%s", err, out)
			}
		}
		spec, _ := v["hookSpecificOutput"].(map[string]any)
		return out, spec
	}
	readme := filepath.Join(scope, "README.md")
	withWord := "# shikii\n\n記録はプロジェクトの根に置く\n"
	out, spec := call("Edit", map[string]any{"file_path": readme, "old_string": "# shikii\n", "new_string": withWord})
	reason, _ := spec["permissionDecisionReason"].(string)
	if spec["permissionDecision"] != "deny" || spec["hookEventName"] != "PreToolUse" || !strings.Contains(reason, "no-ne") || !strings.Contains(reason, `README.md: "根"`) || !strings.Contains(reason, "shikii guard disable") {
		t.Fatalf("the change is refused with the guard, the file, the word and how a person removes the check:\n%s", out)
	}
	for name, c := range map[string]struct {
		tool  string
		input map[string]any
	}{
		"Write":        {"Write", map[string]any{"file_path": readme, "content": withWord}},
		"MultiEdit":    {"MultiEdit", map[string]any{"file_path": readme, "edits": []map[string]any{{"old_string": "a", "new_string": "b"}, {"old_string": "# shikii\n", "new_string": withWord}}}},
		"NotebookEdit": {"NotebookEdit", map[string]any{"notebook_path": filepath.Join(scope, "notes.ipynb"), "new_source": withWord}},
	} {
		if out, spec := call(c.tool, c.input); spec["permissionDecision"] != "deny" {
			t.Fatalf("%s: the change is refused:\n%s", name, out)
		}
	}
	// what was already there is not the agent's: an Edit that keeps a line with the word, adding none, passes
	if out, spec := call("Edit", map[string]any{"file_path": readme, "old_string": withWord, "new_string": withWord + "\nmore\n"}); spec["permissionDecision"] != nil {
		t.Fatalf("a line the agent keeps is not an added line:\n%s", out)
	}
	// an excepted path passes
	if out, spec := call("Write", map[string]any{"file_path": filepath.Join(scope, "docs", "operations.md"), "content": withWord}); spec["permissionDecision"] != nil {
		t.Fatalf("an excepted path is not checked:\n%s", out)
	}
	// a command is not a file change: Bash is left to the Stop check (the written text is not known before it runs)
	if out, spec := call("Bash", map[string]any{"command": "echo 根 >> README.md"}); spec["permissionDecision"] != nil {
		t.Fatalf("Bash is not refused here:\n%s", out)
	}
	// the stops are recorded for this turn, marked as before the change, so status counts the turn as stopped
	recs := guardRecordsFor(st.Dir, "no-ne")
	if len(recs) != 4 || recs[0].Pass || !recs[0].Tool || recs[0].Turn != loadSession(st.Dir, "g").TurnID {
		t.Fatalf("each refusal is recorded as a stop before the change of this turn: %+v", recs)
	}
	// a disabled guard stops nothing
	g, _ := loadGuard(guardPath(st.Dir, "no-ne"))
	g.Enabled = false
	if err := saveGuard(st.Dir, g); err != nil {
		t.Fatal(err)
	}
	if out, spec := call("Write", map[string]any{"file_path": readme, "content": withWord}); spec["permissionDecision"] != nil {
		t.Fatalf("a disabled guard does not refuse:\n%s", out)
	}
}

// After a stop before the change, the Stop check that finds the guard passing records it as the fix (recheck), so status counts the turn as fixed.
func TestAStopBeforeTheChangeCountsAsFixedWhenTheTurnEndsClean(t *testing.T) {
	st, scope, _, _ := toolLessonFixture(t)
	writeGuard(t, st.Dir, guard{ID: "no-ne", Kind: guardKindForbiddenWords, Title: "根を使わない", Words: []string{"根"}})
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "g", "cwd": scope, "prompt": "README に置き場の説明を足して"})
	runHook(t, "tool-lesson", map[string]any{"session_id": "g", "cwd": scope, "hook_event_name": "PreToolUse", "tool_name": "Write", "tool_input": map[string]any{"file_path": filepath.Join(scope, "README.md"), "content": "根\n"}})
	runHook(t, "stop", map[string]any{"session_id": "g", "cwd": scope, "last_assistant_message": "done"})
	guards, _ := loadGuards(st.Dir)
	if s := countGuardStats(guards, readGuardRecords(st.Dir)); s.Stopped != 1 || s.Fixed != 1 {
		t.Fatalf("the turn was stopped before the change and fixed by its end: %+v (%+v)", s, guardRecordsFor(st.Dir, "no-ne"))
	}
}

// 0.11: a guard of kind forbidden-commands stops a command right before it runs (PreToolUse Bash): the agent's own repeated mistakes
// (a check piped into tail, then git commit in the same command: the exit code is lost) become a check the person enables, and the
// refusal comes with the reason so the agent runs a corrected command at once. Commands are not observed at Stop, so there the kind passes.
func TestEnabledForbiddenCommandsGuardStopsTheCommandBeforeItRuns(t *testing.T) {
	st, scope, _, _ := toolLessonFixture(t)
	writeGuard(t, st.Dir, guard{ID: "check-piped-before-commit", Kind: guardKindForbiddenCommands, Title: "do not pipe a check into tail before committing",
		Patterns: []string{`\|\s*(tail|head|grep)\b[^\n]*\bgit\s+(-c\s+\S+\s+)*commit\b`}, Example: "make test 2>&1 | tail -3; git commit -m x"})
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "g", "cwd": scope, "prompt": "commit the fix"})
	call := func(tool string, input map[string]any) (string, map[string]any) {
		t.Helper()
		out := runHook(t, "tool-lesson", map[string]any{"session_id": "g", "cwd": scope, "hook_event_name": "PreToolUse", "tool_name": tool, "tool_input": input})
		var v map[string]any
		if out != "" {
			if err := json.Unmarshal([]byte(out), &v); err != nil {
				t.Fatalf("one JSON object: %v\n%s", err, out)
			}
		}
		spec, _ := v["hookSpecificOutput"].(map[string]any)
		return out, spec
	}
	out, spec := call("Bash", map[string]any{"command": "make test 2>&1 | tail -3 && git -c user.name=x commit -q -m 'fix'"})
	reason, _ := spec["permissionDecisionReason"].(string)
	if spec["permissionDecision"] != "deny" || !strings.Contains(reason, "check-piped-before-commit") || !strings.Contains(reason, "tail") || !strings.Contains(reason, "shikii guard disable") {
		t.Fatalf("the command is refused with the guard, the pattern and how a person removes the check:\n%s", out)
	}
	for name, command := range map[string]string{"check alone": "make test 2>&1 | tail -3", "commit alone": "git commit -m x", "check then commit without a pipe": "make test && git commit -m x"} {
		if out, spec := call("Bash", map[string]any{"command": command}); spec["permissionDecision"] != nil {
			t.Fatalf("%s: not refused:\n%s", name, out)
		}
	}
	// a file change is not a command
	if out, spec := call("Edit", map[string]any{"file_path": filepath.Join(scope, "README.md"), "old_string": "a", "new_string": "make test | tail; git commit"}); spec["permissionDecision"] != nil {
		t.Fatalf("a file change is not judged by the commands guard:\n%s", out)
	}
	recs := guardRecordsFor(st.Dir, "check-piped-before-commit")
	if len(recs) != 1 || recs[0].Pass || !recs[0].Tool {
		t.Fatalf("the refusal is recorded as a stop before the command: %+v", recs)
	}
	// at Stop commands are not observed: the kind passes there
	runHook(t, "stop", map[string]any{"session_id": "g", "cwd": scope, "last_assistant_message": "done"})
	guards, _ := loadGuards(st.Dir)
	if s := countGuardStats(guards, readGuardRecords(st.Dir)); s.Stopped != 1 || s.Fixed != 1 {
		t.Fatalf("a refused command counts as stopped and, the command never having run, as fixed: %+v", s)
	}
}

// Audit 0.11, a note outside the list: a whole-file write (Write, NotebookEdit) replaces the text, so a line the file already holds is not an added line;
// only a line new to the file is. And a file outside the project (the person's memory index in HOME) is not the project's: not checked.
func TestWholeFileWritesCountOnlyNewLinesAndOnlyInsideTheProject(t *testing.T) {
	st, scope, _, _ := toolLessonFixture(t)
	writeGuard(t, st.Dir, guard{ID: "no-ne", Kind: guardKindForbiddenWords, Title: "x", Words: []string{"\u6839"}})
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "g", "cwd": scope, "prompt": "README \u3092\u76f4\u3057\u3066"})
	call := func(tool string, input map[string]any) map[string]any {
		t.Helper()
		out := runHook(t, "tool-lesson", map[string]any{"session_id": "g", "cwd": scope, "hook_event_name": "PreToolUse", "tool_name": tool, "tool_input": input})
		var v map[string]any
		if out != "" {
			if err := json.Unmarshal([]byte(out), &v); err != nil {
				t.Fatalf("one JSON object: %v\n%s", err, out)
			}
		}
		spec, _ := v["hookSpecificOutput"].(map[string]any)
		return spec
	}
	readme := filepath.Join(scope, "README.md")
	existing := "# shikii\n\n\u8a18\u9332\u306f\u30d7\u30ed\u30b8\u30a7\u30af\u30c8\u306e\u6839\u306b\u7f6e\u304f\n"
	if err := os.WriteFile(readme, []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}
	if spec := call("Write", map[string]any{"file_path": readme, "content": existing + "\nmore\n"}); spec["permissionDecision"] != nil {
		t.Fatalf("a line the file already holds is not added: %v", spec)
	}
	if spec := call("Write", map[string]any{"file_path": readme, "content": existing + "\n\u6839\u3092\u8db3\u3059\n"}); spec["permissionDecision"] != "deny" {
		t.Fatalf("a line new to the file with the word is refused: %v", spec)
	}
	outside := filepath.Join(t.TempDir(), "MEMORY.md")
	if spec := call("Write", map[string]any{"file_path": outside, "content": "\u6839\n"}); spec["permissionDecision"] != nil {
		t.Fatalf("a file outside the project is not the project's: %v", spec)
	}
}
