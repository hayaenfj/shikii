package main

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

// After a stop before a command, the Stop check that finds the guard passing records it as the fix (recheck), so status counts the
// turn as stopped and fixed (the refused command never ran).
func TestAStopBeforeTheCommandCountsAsFixedWhenTheTurnEndsClean(t *testing.T) {
	st, scope, _, _ := toolLessonFixture(t)
	writeGuard(t, st.Dir, guard{ID: "no-pipe-commit", Kind: guardKindForbiddenCommands, Title: "x", Patterns: []string{`\|\s*tail\b[^\n]*\bgit\s+commit\b`}, Example: "make test | tail -3; git commit -m x"})
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "g", "cwd": scope, "prompt": "commit the fix"})
	runHook(t, "tool-lesson", map[string]any{"session_id": "g", "cwd": scope, "hook_event_name": "PreToolUse", "tool_name": "Bash", "tool_input": map[string]any{"command": "make test | tail -3 && git commit -m x"}})
	runHook(t, "stop", map[string]any{"session_id": "g", "cwd": scope, "last_assistant_message": "done"})
	guards, _ := loadGuards(st.Dir)
	if s := countGuardStats(guards, readGuardRecords(st.Dir)); s.Stopped != 1 || s.Fixed != 1 {
		t.Fatalf("the turn was stopped before the command and fixed by its end: %+v (%+v)", s, guardRecordsFor(st.Dir, "no-pipe-commit"))
	}
}

// 0.11: forbidden-words is not judged before a file change. The check of the lines an edit adds was built and removed after three
// audits in a row found a miss or a wrong refusal (paths through links or in another spelling, folders not yet there, CRLF, the order
// of a MultiEdit's edits); it runs at Stop only, as before. An enabled forbidden-words guard therefore never refuses an edit here.
func TestForbiddenWordsIsNotJudgedBeforeTheChange(t *testing.T) {
	st, scope, _, _ := toolLessonFixture(t)
	writeGuard(t, st.Dir, guard{ID: "no-ne", Kind: guardKindForbiddenWords, Title: "x", Words: []string{"\u6839"}})
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "g", "cwd": scope, "prompt": "README \u3092\u76f4\u3057\u3066"})
	for tool, input := range map[string]map[string]any{
		"Edit":      {"file_path": filepath.Join(scope, "README.md"), "old_string": "# shikii", "new_string": "\u6839"},
		"Write":     {"file_path": filepath.Join(scope, "README.md"), "content": "\u6839\n"},
		"MultiEdit": {"file_path": filepath.Join(scope, "README.md"), "edits": []map[string]any{{"old_string": "a", "new_string": "\u6839", "replace_all": true}}},
	} {
		out := runHook(t, "tool-lesson", map[string]any{"session_id": "g", "cwd": scope, "hook_event_name": "PreToolUse", "tool_name": tool, "tool_input": input})
		if strings.Contains(out, "permissionDecision") {
			t.Fatalf("%s: a file change is not refused before it happens (forbidden-words runs at Stop):\n%s", tool, out)
		}
	}
	if recs := guardRecordsFor(st.Dir, "no-ne"); len(recs) != 0 {
		t.Fatalf("nothing is recorded before the change: %+v", recs)
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
