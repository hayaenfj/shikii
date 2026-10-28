package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// A guard the person enabled stops a file change right before it is written (PreToolUse, 0.11): a forbidden word in what Edit, Write,
// MultiEdit or NotebookEdit is about to add is refused with the reason, so the agent fixes the text and changes the file again in the
// same breath instead of at the end of the turn (the person, 2026-10-28: improve during the work, not at the end). Only forbidden-words
// can be judged from one tool call; paired-files and commit-body still run at Stop, and so does forbidden-words for text written another
// way (a command). Unknown shapes pass: it stops only when certain.

// toolAddedLines returns, for a file-changing tool call on a file inside the project scope, the file relative to the project and the
// lines it adds: the lines of the new text that are not in the text it replaces (nor, for a whole-file write, already in the file).
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
		var edits []struct { // an edit also carries replace_all (a boolean): only the two strings are read (audit 0.11)
			Old string `json:"old_string"`
			New string `json:"new_string"`
		}
		if json.Unmarshal(input["edits"], &edits) != nil {
			return "", nil, false
		}
		for _, e := range edits {
			old = append(old, strings.Split(e.Old, "\n")...)
			now = append(now, strings.Split(e.New, "\n")...)
		}
	}
	// only files inside the project are checked: the Stop check looks at the project's files, and a guard's except patterns are written
	// against the project root. A file elsewhere (the person's memory index in HOME, for instance) is not the project's (audit 0.11)
	rel, err := filepath.Rel(scope, file)
	if scope == "" || err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", nil, false
	}
	if in.ToolName == "Write" || in.ToolName == "NotebookEdit" { // the whole text is replaced: lines the file already holds are not added
		if b, rerr := os.ReadFile(file); rerr == nil && len(b) <= guardFileBytesMax && !isBinary(b) {
			old = append(old, strings.Split(string(b), "\n")...)
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
	return filepath.ToSlash(rel), added, true
}

// guardToolCall applies the enabled forbidden-words guards to the lines a tool call is about to add. A hit refuses the call (Deny) with
// the reason and records the stop for the turn (marked Tool), which the Stop check closes as fixed when the guard passes at the end.
// scope is the project's folder from the hook's own lookup: the session's scope when the session has one, else the project the cwd is in
// (a session whose first message was a slash command has none; the cwd may be a subfolder, which would make the paths wrong. audit 0.11)
func guardToolCall(root string, in *hookInput, snapshot *hookSession, scope string) (hookOut, error) {
	if snapshot.Scope != "" {
		scope = snapshot.Scope
	}
	var input guardInput
	kind := ""
	if in.ToolName == "Bash" { // a command about to run: forbidden-commands (0.11)
		var raw map[string]json.RawMessage
		var command string
		if json.Unmarshal(in.ToolInput, &raw) != nil || json.Unmarshal(raw["command"], &command) != nil || strings.TrimSpace(command) == "" {
			return hookOut{}, nil
		}
		input, kind = guardInput{Commands: []string{command}, CommandsKnown: true}, guardKindForbiddenCommands
	} else {
		file, added, ok := toolAddedLines(in, scope)
		if !ok || len(added) == 0 {
			return hookOut{}, nil
		}
		input, kind = guardInput{Added: map[string][]string{file: added}}, guardKindForbiddenWords
	}
	guards, err := enabledGuards(root)
	if err != nil || len(guards) == 0 {
		return hookOut{}, err
	}
	loc := hookLoc{Lang: hookLang()}
	now := time.Now().UTC()
	var recs []guardRecord
	var failed []guardResult
	for _, g := range guards {
		if g.Kind != kind {
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
	if kind == guardKindForbiddenCommands {
		b.WriteString(bi("shikii: a check the person set stops this command. Run a corrected command instead.", "shikii: \u4eba\u304c\u6c7a\u3081\u305f\u6b62\u3081\u308b\u691c\u67fb\u304c\u3053\u306e\u30b3\u30de\u30f3\u30c9\u3092\u6b62\u3081\u307e\u3057\u305f\u3002\u76f4\u3057\u305f\u30b3\u30de\u30f3\u30c9\u3092\u6d41\u3057\u3066\u304f\u3060\u3055\u3044\u3002", loc) + "\n")
	} else {
		b.WriteString(bi("shikii: a check the person set stops this change. Fix the text and change the file again.", "shikii: 人が決めた止める検査がこの変更を止めました。文を直して、もう一度ファイルを変えてください。", loc) + "\n")
	}
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
