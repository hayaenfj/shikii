package main

import (
	"encoding/json"
	"strings"
	"time"
)

// A guard the person enabled stops a command right before it runs (PreToolUse Bash, 0.11): a command matching a forbidden-commands
// pattern is refused with the reason, so the agent runs a corrected command in the same breath instead of hearing about it at the end
// of the turn. File changes are not judged here: forbidden-words, paired-files and commit-body run at Stop. A pre-tool check of the
// lines a file edit adds was built and removed in 0.11: three audits in a row found a way to miss a line or to refuse an excepted file
// (the order of a MultiEdit's edits, paths through links or in another spelling, folders that do not exist yet, CRLF), and the rule for
// a property broken again is to simplify or remove, not to patch (wiki 325c0267cfe4ce6d).

// guardToolCall applies the enabled forbidden-commands guards to the command about to run. A hit refuses the call (Deny) with the
// reason and records the stop for the turn (marked Tool); the Stop check closes it as fixed when the guard passes there (commands are
// not observed at Stop, so a refused command, which never ran, counts as fixed).
func guardToolCall(root string, in *hookInput, snapshot *hookSession) (hookOut, error) {
	if in.ToolName != "Bash" {
		return hookOut{}, nil
	}
	var raw map[string]json.RawMessage
	var command string
	if json.Unmarshal(in.ToolInput, &raw) != nil || json.Unmarshal(raw["command"], &command) != nil || strings.TrimSpace(command) == "" {
		return hookOut{}, nil
	}
	guards, err := enabledGuards(root)
	if err != nil || len(guards) == 0 {
		return hookOut{}, err
	}
	input := guardInput{Commands: []string{command}, CommandsKnown: true}
	loc := hookLoc{Lang: hookLang()}
	now := time.Now().UTC()
	var recs []guardRecord
	var failed []guardResult
	for _, g := range guards {
		if g.Kind != guardKindForbiddenCommands {
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
	b.WriteString(bi("shikii: a check the person set stops this command. Run a corrected command instead.", "shikii: \u4eba\u304c\u6c7a\u3081\u305f\u6b62\u3081\u308b\u691c\u67fb\u304c\u3053\u306e\u30b3\u30de\u30f3\u30c9\u3092\u6b62\u3081\u307e\u3057\u305f\u3002\u76f4\u3057\u305f\u30b3\u30de\u30f3\u30c9\u3092\u6d41\u3057\u3066\u304f\u3060\u3055\u3044\u3002", loc) + "\n")
	ids := make([]string, 0, len(failed))
	for _, f := range failed {
		b.WriteString("- " + f.ID + ": " + f.Reason + "\n")
		ids = append(ids, f.ID)
	}
	b.WriteString("Removing a check itself is the person's decision: shikii guard disable <id>")
	return hookOut{Deny: true, Reason: b.String(), System: bi(
		"shikii: the check that stops ("+strings.Join(ids, ", ")+") refused a command before it ran, so the agent was told to run a corrected one",
		"shikii: \u6b62\u3081\u308b\u691c\u67fb("+strings.Join(ids, ", ")+")\u304c\u6d41\u3059\u524d\u306e\u30b3\u30de\u30f3\u30c9\u3092\u6b62\u3081\u305f\u306e\u3067\u3001\u76f4\u3057\u305f\u30b3\u30de\u30f3\u30c9\u3092\u6d41\u3059\u3088\u3046\u4f1d\u3048\u307e\u3057\u305f", loc)}, nil
}

// toolStoppedGuards returns the guards that stopped a command before it ran in this turn (records marked Tool).
func toolStoppedGuards(root, session, turn string) map[string]bool {
	out := map[string]bool{}
	for _, r := range readGuardRecords(root) {
		if r.Tool && r.Session == session && r.Turn == turn {
			out[r.Guard] = true
		}
	}
	return out
}
