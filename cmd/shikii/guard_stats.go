package main

import (
	"fmt"
	"io"
)

func countGuardStats(guards []guard, recs []guardRecord) guardStats {
	var s guardStats
	for _, g := range guards {
		if g.Enabled && !isBuiltinGuard(g.ID) {
			s.Enabled++
		}
	}
	stopped := map[string]bool{}           // session/turn
	failed := map[string]map[string]bool{} // session/turn → guard → not fixed yet
	for _, r := range recs {
		if isBuiltinGuard(r.Guard) {
			continue // ritual checks are counted separately in countRitualStats (0.8.0)
		}
		key := r.Session + "/" + r.Turn
		if !r.Recheck && !r.Pass {
			stopped[key] = true
			if failed[key] == nil {
				failed[key] = map[string]bool{}
			}
			failed[key][r.Guard] = true
		}
		if r.Recheck && r.Pass && failed[key] != nil {
			delete(failed[key], r.Guard)
		}
	}
	for key := range stopped {
		s.Stopped++
		if len(failed[key]) == 0 {
			s.Fixed++
		}
	}
	return s
}

func printGuardStats(out io.Writer, root string) {
	guards, err := loadGuards(root)
	if err != nil {
		fmt.Fprintf(out, "guards: %v\n", err)
		return
	}
	recs := readGuardRecords(root)
	if len(guards) == 0 {
		fmt.Fprintln(out, "guards: none (shikii guard add turns a correction into a check that stops the turn)")
	} else {
		s := countGuardStats(guards, recs)
		fmt.Fprintf(out, "guards: %d of %d enabled; stopped %d turns, fixed before the turn ended %d\n", s.Enabled, len(guards), s.Stopped, s.Fixed)
	}
	// Ritual checks (0.8.0): of the times asked, how many were already done and how many were done after a block. This shows the person "whether the AI follows the rituals"
	r := countRitualStats(recs)
	kinds := ""
	if r.Kinds.Asked > 0 { // only while pages from before the version bump remain (0.9.0)
		kinds = fmt.Sprintf("; wiki-kinds done %d of %d asked (after a stop %d)", r.Kinds.Done, r.Kinds.Asked, r.Kinds.Fixed)
	}
	if r.Lang.Asked > 0 { // only where the language is one written in its own script (0.11)
		kinds += fmt.Sprintf("; reply-language in the person's language %d of %d checked (after a stop %d)", r.Lang.Done, r.Lang.Asked, r.Lang.Fixed)
	}
	if r.Reasoning.Asked > 0 {
		kinds += fmt.Sprintf("; reasoning-before-end done %d of %d asked (after a stop %d)", r.Reasoning.Done, r.Reasoning.Asked, r.Reasoning.Fixed)
	}
	fmt.Fprintf(out, "rituals: plan-before-work done %d of %d asked (after a stop %d); reflect-before-end done %d of %d asked (after a stop %d)%s\n", r.Plan.Done, r.Plan.Asked, r.Plan.Fixed, r.Reflect.Done, r.Reflect.Asked, r.Reflect.Fixed, kinds)
}

// ritualStat counts how one ritual was carried out: turns asked (checked), turns done from the start, and turns done after a block.
type ritualStat struct{ Asked, Done, Fixed int }

type ritualStats struct{ Plan, Reflect, Kinds, Lang, Reasoning ritualStat }

func countRitualStats(recs []guardRecord) ritualStats {
	var out ritualStats
	for _, id := range []string{ritualPlan, ritualReflect, ritualKinds, ritualLang, ritualReasoning} {
		var st ritualStat
		failed := map[string]bool{} // session/turn → blocked and not done yet
		for _, r := range recs {
			if r.Guard != id {
				continue
			}
			key := r.Session + "/" + r.Turn
			switch {
			case !r.Recheck && r.Pass:
				st.Asked++
				st.Done++
			case !r.Recheck && !r.Pass && failed[key]:
				// a second failure in the same turn (the Stop after the notice) is not counted
			case !r.Recheck && !r.Pass:
				st.Asked++
				failed[key] = true
			case r.Recheck && r.Pass && failed[key]:
				delete(failed, key)
				st.Fixed++
			}
		}
		switch id {
		case ritualReasoning:
			out.Reasoning = st
		case ritualPlan:
			out.Plan = st
		case ritualKinds:
			out.Kinds = st
		case ritualLang:
			out.Lang = st
		default:
			out.Reflect = st
		}
	}
	return out
}

// guardSummaryJSON is for the page (summary.guards in /v1/status): counts, and each check's state and last result.
func guardSummaryJSON(root string) map[string]any {
	guards, err := loadGuards(root)
	items := []map[string]any{}
	if err != nil {
		return map[string]any{"total": 0, "enabled": 0, "stopped": 0, "fixed": 0, "items": items, "error": err.Error()}
	}
	recs := readGuardRecords(root)
	last := map[string]guardRecord{}
	for _, r := range recs {
		last[r.Guard] = r
	}
	guards = append(builtinGuards(), guards...) // list the ritual checks (built in) too (0.8.0)
	for _, g := range guards {
		item := map[string]any{"id": g.ID, "kind": g.Kind, "title": g.Title, "enabled": g.Enabled, "reason": g.Reason, "builtin": isBuiltinGuard(g.ID)}
		if r, ok := last[g.ID]; ok {
			item["last"] = map[string]any{"pass": r.Pass, "at": r.At, "reason": r.Reason, "recheck": r.Recheck}
		}
		items = append(items, item)
	}
	s := countGuardStats(guards, recs)
	r := countRitualStats(recs)
	own := 0 // number of checks made by people (built-ins excluded)
	for _, g := range guards {
		if !isBuiltinGuard(g.ID) {
			own++
		}
	}
	return map[string]any{"total": own, "enabled": s.Enabled, "stopped": s.Stopped, "fixed": s.Fixed, "items": items,
		"rituals": map[string]any{"reasoning": map[string]int{"asked": r.Reasoning.Asked, "done": r.Reasoning.Done, "fixed": r.Reasoning.Fixed}, "plan": map[string]int{"asked": r.Plan.Asked, "done": r.Plan.Done, "fixed": r.Plan.Fixed}, "reflect": map[string]int{"asked": r.Reflect.Asked, "done": r.Reflect.Done, "fixed": r.Reflect.Fixed}, "kinds": map[string]int{"asked": r.Kinds.Asked, "done": r.Kinds.Done, "fixed": r.Kinds.Fixed}, "language": map[string]int{"asked": r.Lang.Asked, "done": r.Lang.Done, "fixed": r.Lang.Fixed}}}
}
