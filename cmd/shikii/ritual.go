package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
)

// Ritual checks (0.8.0): when the agent tries to finish without doing a ritual the hook asked for (sorting out the plan before work, saving the reflection before finishing),
// Stop is blocked once, with the reason, using the same mechanism as the checks that stop the turn. Merely asking got put off during long work, and nobody noticed
// (in haya1, 0 needs-plan records, and 6 of 26 delivered lessons assessed). Built in and always enabled; there is no setting to turn it off.
// Records go into records/guards.jsonl, the same as the checks that stop the turn, with guard = plan-before-work / reflect-before-end, and appear in status and on the page.

const (
	ritualPlan    = "plan-before-work"
	ritualReflect = "reflect-before-end"
	ritualKinds   = "wiki-kinds" // give a kind and a scene to pages from before the version bump (0.9.0)
)

// builtinGuards are the built-in ritual checks (for the list, totals and the page; ritualStop does the evaluation).
func builtinGuards() []guard {
	return []guard{
		{ID: ritualPlan, Kind: "ritual", Title: "Sort out the plan before work (hook plan query → submit)", Reason: "if files were changed in a request the hook asked about, submit the sorted plan before finishing", Enabled: true},
		{ID: ritualReflect, Kind: "ritual", Title: "Save the reflection before finishing (reflect query → submit)", Reason: "if the hook listed conversations, submit the provisional scores, wiki and assessments of delivered lessons before finishing", Enabled: true},
		{ID: ritualKinds, Kind: "ritual", Title: "Give a kind and a scene to pages without them (shikii wiki classify)", Reason: "if the hook listed pages from before the version bump, give them a kind and a scene before finishing", Enabled: true},
	}
}

func isBuiltinGuard(id string) bool {
	return id == ritualPlan || id == ritualReflect || id == ritualKinds
}

// ritualStop checks whether the rituals asked for in this turn are done. On recheck (the Stop after a block) it only records and does not block.
func ritualStop(root, scope string, in *hookInput, snapshot *hookSession, changed []string, known bool, recheck bool) (hookOut, error) {
	if snapshot.RitualStopped == snapshot.TurnID && snapshot.TurnID != "" {
		recheck = true // already blocked once in this turn (the Stop after a notice has no stop_hook_active): only record, do not block again (audit R5)
	}
	now := time.Now().UTC()
	var recs []guardRecord
	var failed []string
	// Plan: asked for (controls: Planning belongs to this turn) but no submit, and files were changed
	if p := snapshot.Planning; p != nil && p.TurnID == snapshot.TurnID && known && len(changed) > 0 {
		pass := p.Result != nil
		reason := ""
		if !pass {
			reason = planningBlockReason(in.SessionID, snapshot.TurnID, len(changed))
			failed = append(failed, reason)
		}
		recs = append(recs, guardRecord{At: now, Session: in.SessionID, Turn: snapshot.TurnID, Guard: ritualPlan, Pass: pass, Reason: reason, Recheck: recheck})
	}
	// Reflection: conversations listed before the request are still waiting for reflection (provisional scores, wiki or assessments of delivered lessons not done)
	if len(snapshot.ReflectAsked) > 0 && snapshot.ReflectAskedTurn == snapshot.TurnID {
		left, err := reflectLeft(root, scope, snapshot.ReflectAsked)
		if err != nil {
			return hookOut{}, err
		}
		pass := len(left) == 0
		reason := ""
		if !pass {
			ids := make([]string, 0, len(left))
			for _, l := range left {
				ids = append(ids, l.id)
			}
			data, _ := json.Marshal(map[string]any{"sources": ids})
			reason = "The reflection has not been saved and you are about to finish: " + strings.Join(leftLines(left), "; ") + ". Read the raw text and delivered memories with " + hookExe() + " reflect query (or query of shikii_reflect; targets " + string(data) + "), submit reviews, wiki (only sources when there is no lesson) and memory (helpful / unused / harmful / unknown for each delivered lesson), then finish"
			failed = append(failed, reason)
		}
		recs = append(recs, guardRecord{At: now, Session: in.SessionID, Turn: snapshot.TurnID, Guard: ritualReflect, Pass: pass, Reason: reason, Recheck: recheck})
	}
	// Kind and scene: pages listed before the request still have no kind or scene (0.9.0)
	if len(snapshot.ClassifyAsked) > 0 && snapshot.ClassifyAskedTurn == snapshot.TurnID {
		left, err := classifyLeft(root, scope, snapshot.ClassifyAsked)
		if err != nil {
			return hookOut{}, err
		}
		pass := len(left) == 0
		reason := ""
		if !pass {
			reason = fmt.Sprintf("Pages that still need a kind and a scene: %d (%s). Give them with %s wiki classify <page ID> <rule|howto|criteria|fact|history> <scene> (on MCP, operation classify of shikii_wiki), then finish", len(left), strings.Join(left, ", "), hookExe())
			failed = append(failed, reason)
		}
		recs = append(recs, guardRecord{At: now, Session: in.SessionID, Turn: snapshot.TurnID, Guard: ritualKinds, Pass: pass, Reason: reason, Recheck: recheck})
	}
	if len(recs) > 0 {
		if err := appendGuardRecords(root, recs); err != nil {
			return hookOut{}, err
		}
	}
	if len(failed) == 0 || recheck {
		return hookOut{}, nil
	}
	var b strings.Builder
	b.WriteString("shikii: rituals are not done. Do them before finishing (a built-in check; a second Stop is not blocked). In your final reply after doing them, briefly restate the result of the work the person asked for, not a report on the rituals.\n")
	// Claude Code mostly acts on this block rather than on the notices before the request (audit 0.9.2 English, medium 3), so the rule rides along here too
	b.WriteString("- If this turn created or changed a rule, test or procedure (a command or a script), also write it to the wiki as a howto or rule page (add to or edit the closest page) before finishing\n")
	for _, f := range failed {
		b.WriteString("- " + f + "\n")
	}
	return hookOut{Block: true, Reason: strings.TrimRight(b.String(), "\n")}, nil
}

// reflectLeftItem is a requested conversation still waiting for reflection, and what is not done yet.
type reflectLeftItem struct {
	id                string
	targets, memories int
	wiki              bool
}

func reflectLeft(root, scope string, asked []string) ([]reflectLeftItem, error) {
	view, err := reflectQuery(root, scope)
	if err != nil {
		return nil, err
	}
	want := map[string]bool{}
	for _, id := range asked {
		want[id] = true
	}
	var out []reflectLeftItem
	for _, s := range view.Sources {
		if !want[s.Source.ID] {
			continue
		}
		out = append(out, reflectLeftItem{id: s.Source.ID, targets: len(s.Targets), memories: len(s.Memories), wiki: s.WikiPending})
	}
	for _, o := range view.Observations {
		if want[o.ID] {
			out = append(out, reflectLeftItem{id: o.ID, wiki: true})
		}
	}
	return out, nil
}

func leftLines(left []reflectLeftItem) []string {
	var out []string
	for _, l := range left {
		var parts []string
		if l.targets > 0 {
			parts = append(parts, fmt.Sprintf("provisional scores %d: send reviews with your own truth yes, no or unknown (unknown when you cannot tell; it is your provisional score, not the person's answer)", l.targets))
		}
		if l.wiki {
			parts = append(parts, "wiki unsorted")
		}
		if l.memories > 0 {
			parts = append(parts, fmt.Sprintf("delivered lessons to assess %d", l.memories))
		}
		out = append(out, l.id+" ("+strings.Join(parts, ", ")+")")
	}
	return out
}

func hookExe() string {
	exe, err := os.Executable()
	if err != nil {
		return "shikii"
	}
	return "'" + strings.ReplaceAll(exe, "'", "'\\''") + "'"
}

func planJSON(session, turn string) string {
	b, _ := json.Marshal(map[string]string{"session_id": session, "turn_id": turn})
	return string(b)
}

// planningShape shows that agreed and open are objects (Claude first passed an array in every run of audit 0.9.2 English).
const planningShape = `agreed and open are each one object, not an array: "agreed":{"summary":"...","evidence":[{"source":"current_user","quote":"..."}]}, "open":{"unknown":true}`

// planningBlockReason is the line of the Stop block when the plan was not sorted out but files were changed.
func planningBlockReason(sessionID, turnID string, changed int) string {
	files := fmt.Sprintf("%d files were", changed)
	if changed == 1 {
		files = "1 file was"
	}
	return fmt.Sprintf("The plan has not been sorted out (hook plan query → submit), yet %s changed and you are about to finish. Pass %s to %s hook plan query, read this and the recent conversations, submit agreed and open, then finish. %s", files, planJSON(sessionID, turnID), hookExe(), planningShape)
}
