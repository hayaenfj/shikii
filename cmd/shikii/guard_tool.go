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

// toolAddedLines returns, for Edit, Write and MultiEdit, the file (relative to the project when its real path is inside it, else as
// given) and the lines the call adds: the lines of the new text that the file does not already hold (a new file: all of them). The
// file on disk is the one truth before the tool runs; old_string is not read (MultiEdit applies its edits in order, and pooling their
// old lines let a line one edit added hide behind the next edit's old_string: confirmation audit 0.11). NotebookEdit is left to Stop:
// an .ipynb is JSON, so its raw lines never equal a cell's. Any file the tool writes is checked (a path through a link or in another
// spelling is not "outside"); a guard's except patterns match the project-relative path or the name.
func toolAddedLines(in *hookInput, scope string) (string, []string, bool) {
	field, ok := toolLessonEdits[in.ToolName]
	if !ok || in.ToolName == "NotebookEdit" {
		return "", nil, false
	}
	var input map[string]json.RawMessage
	if json.Unmarshal(in.ToolInput, &input) != nil {
		return "", nil, false
	}
	var file string
	if json.Unmarshal(input[field], &file) != nil || strings.TrimSpace(file) == "" {
		return "", nil, false
	}
	if !filepath.IsAbs(file) && in.CWD != "" {
		file = filepath.Join(in.CWD, file)
	}
	text := func(raw json.RawMessage) string {
		var s string
		json.Unmarshal(raw, &s)
		return s
	}
	var now []string
	switch in.ToolName {
	case "Edit":
		now = strings.Split(text(input["new_string"]), "\n")
	case "Write":
		now = strings.Split(text(input["content"]), "\n")
	case "MultiEdit":
		var edits []struct { // an edit also carries old_string and replace_all (a boolean): only the new text is read
			New string `json:"new_string"`
		}
		if json.Unmarshal(input["edits"], &edits) != nil {
			return "", nil, false
		}
		for _, e := range edits {
			now = append(now, strings.Split(e.New, "\n")...)
		}
	}
	kept := map[string]bool{}
	if b, err := os.ReadFile(file); err == nil && len(b) <= guardFileBytesMax && !isBinary(b) {
		for _, l := range strings.Split(string(b), "\n") {
			kept[l] = true
		}
	}
	var added []string
	for _, l := range now {
		if strings.TrimSpace(l) != "" && !kept[l] {
			added = append(added, l)
		}
	}
	return projectRelative(file, scope), added, true
}

// projectRelative is the file's path relative to the project when its real path (links resolved; the file may not exist yet, so its
// folder's) is inside the project's real path, else the path as given, with forward slashes.
func projectRelative(file, scope string) string {
	shown := filepath.ToSlash(file)
	if scope == "" {
		return shown
	}
	realScope, err := filepath.EvalSymlinks(scope)
	if err != nil {
		realScope = scope
	}
	dir := filepath.Dir(file)
	if real, err := filepath.EvalSymlinks(dir); err == nil {
		dir = real
	}
	if rel, err := filepath.Rel(realScope, filepath.Join(dir, filepath.Base(file))); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel) {
		return filepath.ToSlash(rel)
	}
	return shown
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
