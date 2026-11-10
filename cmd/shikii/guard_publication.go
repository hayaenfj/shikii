package main

import (
	"os"
	"path"
	"slices"
	"strings"
	"time"
)

// Reuse the existing observed-command parser. This is a check at supported
// publication calls, not a shell interpreter or an authorization boundary.
func publicationCommands(command string) [][]string {
	var commands [][]string
	for _, words := range shellSegments(publicationCommandText(readablePart(command))) {
		for len(words) > 0 && (assignment.MatchString(words[0]) || commandPrefixes[words[0]]) {
			words = words[1:]
		}
		if len(words) > 0 && path.Base(words[0]) == "rtk" {
			words = words[1:]
		}
		if len(words) > 0 {
			commands = append(commands, words)
		}
	}
	return commands
}

// FD redirections contain an ampersand without starting another command. Hide
// only that operator from the shared coarse segmenter, preserving quoted text.
// This text is used for observation only, never executed.
func publicationCommandText(command string) string {
	var text strings.Builder
	var quote byte
	for i := 0; i < len(command); i++ {
		c := command[i]
		if c == '\\' && quote != '\'' && i+1 < len(command) {
			text.WriteByte(c)
			i++
			text.WriteByte(command[i])
			continue
		}
		if quote != 0 {
			if c == quote {
				quote = 0
			}
		} else if c == '\'' || c == '"' {
			quote = c
		} else if c == '&' && (i > 0 && strings.ContainsRune("<>", rune(command[i-1])) || i+1 < len(command) && command[i+1] == '>') {
			continue
		}
		text.WriteByte(c)
	}
	return text.String()
}

func publicationArguments(words []string) (string, []string) {
	if len(words) == 0 {
		return "", nil
	}
	program := path.Base(words[0])
	for i := 1; i < len(words); i++ {
		word := words[i]
		if word == "--" {
			return program, words[i+1:]
		}
		if !strings.HasPrefix(word, "-") {
			return program, words[i:]
		}
		if word == "--help" || word == "-h" || word == "--version" {
			return program, nil
		}
		if program == "git" && slices.Contains([]string{"-C", "-c", "--git-dir", "--work-tree", "--namespace", "--config-env"}, word) ||
			program == "gh" && slices.Contains([]string{"-R", "--repo", "--hostname"}, word) {
			i++
		}
	}
	return program, nil
}

func publicationReadOnly(args []string, git bool) bool {
	for i := 0; i < len(args); i++ {
		word := args[i]
		if word == "--" {
			break
		}
		if word == "--help" || word == "-h" || git && (word == "--dry-run" || word == "-n") {
			return true
		}
		if git && slices.Contains([]string{"--repo", "--receive-pack", "--exec", "--push-option", "-o"}, word) {
			i++
		}
		if !git && slices.Contains([]string{"--repo", "-R", "--hostname", "--notes", "-n", "--notes-file", "-F", "--notes-start-tag", "--target", "--title", "-t", "--tag", "--discussion-category"}, word) {
			i++
		}
	}
	return false
}

func isPublicationCommand(command string) bool {
	for _, words := range publicationCommands(command) {
		program, args := publicationArguments(words)
		if program == "git" && len(args) > 0 && args[0] == "push" && !publicationReadOnly(args[1:], true) {
			return true
		}
		if program == "gh" && len(args) > 1 && args[0] == "release" {
			_, release := publicationArguments(append([]string{"gh"}, args[1:]...))
			if len(release) > 0 && slices.Contains([]string{"create", "new", "edit", "upload", "delete", "delete-asset"}, release[0]) && !publicationReadOnly(release[1:], false) {
				return true
			}
		}
	}
	return false
}

func guardPublication(root string, in *hookInput, snapshot *hookSession, command string, guards []guard) (hookOut, error) {
	if snapshot == nil || !isPublicationCommand(command) {
		return hookOut{}, nil
	}
	base := guardBaselineOf(root, in.SessionID, snapshot) // the request's start, or the start of the stretch a slash command opened
	if base.Turn == "" {
		return hookOut{}, nil
	}
	applicable := false
	for _, g := range guards {
		applicable = applicable || g.Kind != guardKindForbiddenCommands
	}
	if !applicable {
		return hookOut{}, nil
	}
	if len(publicationCommands(command)) > 1 {
		return hookOut{Deny: true, Reason: "shikii: run publication as a separate tool call after other commands, so enabled checks inspect the actual files and commit bodies being published."}, nil
	}
	_, listing, listed, _, scope := treeStateWithScope(in.CWD)
	if scope != base.Scope {
		return hookOut{}, nil
	}
	before, err := os.ReadFile(base.Files)
	known := err == nil && listed && strings.HasPrefix(string(before), base.Header)
	var changed []string
	if known {
		changed = changedFiles(string(before), listing)
	}
	input := guardTurnInput(in.CWD, string(before), changed, known, base.Head, base.HeadAt)
	failed := []guardResult{}
	records := []guardRecord{}
	now := time.Now().UTC()
	loc := hookLoc{Lang: hookLang()}
	for _, g := range guards {
		if g.Kind == guardKindForbiddenCommands {
			continue
		}
		r := evalGuard(g, input)
		records = append(records, guardRecord{At: now, Session: in.SessionID, Turn: base.Turn, Guard: r.ID, Pass: r.Pass, Reason: bi(r.Reason, r.ReasonJA, loc), Tool: !r.Pass})
		if !r.Pass {
			failed = append(failed, r)
		}
	}
	if len(records) > 0 {
		if err := appendGuardRecords(root, records); err != nil {
			return hookOut{}, err
		}
	}
	if len(failed) == 0 {
		return hookOut{}, nil
	}
	var reason strings.Builder
	reason.WriteString("shikii: enabled checks failed before publication. Fix the actual files or commit bodies, then retry.\n")
	for _, r := range failed {
		reason.WriteString("- " + r.ID + ": " + r.Reason + "\n")
	}
	reason.WriteString("Removing a check itself is the person's decision: shikii guard disable <id>")
	return hookOut{Deny: true, Reason: reason.String()}, nil
}
