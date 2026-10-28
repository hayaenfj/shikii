package main

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"time"
)

// A guard the person enabled stops a file change right before it is written (PreToolUse, 0.11): a forbidden word in what Edit, Write,
// MultiEdit or NotebookEdit is about to add is refused with the reason, so the agent fixes the text and changes the file again in the
// same breath instead of at the end of the turn (the person, 2026-10-28: improve during the work, not at the end). Only forbidden-words
// can be judged from one tool call; paired-files and commit-body still run at Stop, and so does forbidden-words for text written another
// way (a command). Unknown shapes pass: it stops only when certain.

// toolAddedLines returns, for a file-changing tool call, the file (relative to the project when it is inside it) and the lines it adds:
// the lines of the new text that are not in the text it replaces.
func toolAddedLines(in *hookInput, scope string) (string, []string, bool) {
	field, ok := toolLessonEdits[in.ToolName]
	if !ok {
		return "", nil, false
	}
	var input map[string]json.RawMessage
	if json.Unmarshal(in.ToolInput, &input) != nil {
		return "", nil, false
	}
	var file string
	if json.Unmarshal(input[field], &file) != nil || file == "" {
		return "", nil, false
	}
	text := func(key string) string {
		var s string
		json.Unmarshal(input[key], &s)
		return s
	}
	var old, now []string
	switch in.ToolName {
	case "Edit":
		old, now = strings.Split(text("old_string"), "\n"), strings.Split(text("new_string"), "\n")
	case "Write":
		now = strings.Split(text("content"), "\n")
	case "NotebookEdit":
		now = strings.Split(text("new_source"), "\n")
	case "MultiEdit":
		var edits []struct{ Old, New string }
		var raw []map[string]string
		if json.Unmarshal(input["edits"], &raw) != nil {
			return "", nil, false
		}
		for _, e := range raw {
			edits = append(edits, struct{ Old, New string }{e["old_string"], e["new_string"]})
		}
		for _, e := range edits {
			old = append(old, strings.Split(e.Old, "\n")...)
			now = append(now, strings.Split(e.New, "\n")...)
		}
	}
	kept := map[string]bool{}
	for _, l := range old {
		kept[l] = true
	}
	var added []string
	for _, l := range now {
		if strings.TrimSpace(l) != "" && !kept[l] {
			added = append(added, l)
		}
	}
	shown := filepath.ToSlash(file)
	for _, dir := range []string{scope, in.CWD} {
		if rel, err := filepath.Rel(dir, file); dir != "" && err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			shown = filepath.ToSlash(rel)
			break
		}
	}
	return shown, added, true
}

// guardToolCall applies the enabled forbidden-words guards to the lines a tool call is about to add. A hit refuses the call (Deny) with
// the reason and records the stop for the turn (marked Tool), which the Stop check closes as fixed when the guard passes at the end.
func guardToolCall(root string, in *hookInput, snapshot *hookSession) (hookOut, error) {
	file, added, ok := toolAddedLines(in, snapshot.Scope)
	if !ok || len(added) == 0 {
		return hookOut{}, nil
	}
	guards, err := enabledGuards(root)
	if err != nil || len(guards) == 0 {
		return hookOut{}, err
	}
	input := guardInput{Added: map[string][]string{file: added}}
	loc := hookLoc{Lang: hookLang()}
	now := time.Now().UTC()
	var recs []guardRecord
	var failed []guardResult
	for _, g := range guards {
		if g.Kind != guardKindForbiddenWords {
			continue
		}
		r := evalGuard(g, input)
		if r.Pass {
			continue
		}
		r.Reason = bi(r.Reason, r.ReasonJA, loc)
		failed = append(failed, r)
		recs = append(recs, guardRecord{At: now, Session: in.SessionID, Turn: snapshot.TurnID, Guard: r.ID, Pass: false, Reason: r.Reason, Tool: true})
	}
	if len(failed) == 0 {
		return hookOut{}, nil
	}
	if err := appendGuardRecords(root, recs); err != nil {
		return hookOut{}, err
	}
	var b strings.Builder
	b.WriteString(bi("shikii: a check the person set stops this change. Fix the text and change the file again.", "shikii: 人が決めた止める検査がこの変更を止めました。文を直して、もう一度ファイルを変えてください。", loc) + "\n")
	ids := make([]string, 0, len(failed))
	for _, f := range failed {
		b.WriteString("- " + f.ID + ": " + f.Reason + "\n")
		ids = append(ids, f.ID)
	}
	b.WriteString("Removing a check itself is the person's decision: shikii guard disable <id>")
	return hookOut{Deny: true, Reason: b.String(), System: bi(
		"shikii: the check that stops ("+strings.Join(ids, ", ")+") refused a change before it was written, so the agent was told to fix the text",
		"shikii: 止める検査("+strings.Join(ids, ", ")+")が書き込む前の変更を止めたので、文を直すよう伝えました", loc)}, nil
}

// toolStoppedGuards returns the guards that stopped a change before it was written in this turn (records marked Tool).
func toolStoppedGuards(root, session, turn string) map[string]bool {
	out := map[string]bool{}
	for _, r := range readGuardRecords(root) {
		if r.Tool && r.Session == session && r.Turn == turn {
			out[r.Guard] = true
		}
	}
	return out
}
