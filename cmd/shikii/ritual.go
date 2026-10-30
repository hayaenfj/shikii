package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/hayaenfj/shikii/internal/store"
)

// Ritual checks (0.8.0): when the agent tries to finish without doing a ritual the hook asked for (sorting out the plan before work, saving the reflection before finishing),
// Stop requests recovery before judgment. Saved progress permits another continuation; unchanged pending work ends explicitly incomplete. Merely asking got put off during long work, and nobody noticed
// (in haya1, 0 needs-plan records, and 6 of 26 delivered lessons assessed). Built in and always enabled; there is no setting to turn it off.
// Records go into records/guards.jsonl, the same as the checks that stop the turn, with guard = plan-before-work / reflect-before-end, and appear in status and on the page.

const (
	ritualPlan    = "plan-before-work"
	ritualReflect = "reflect-before-end"
	ritualKinds   = "wiki-kinds"     // give a kind and a scene to pages from before the version bump (0.9.0)
	ritualLang    = "reply-language" // write the final reply in the person's language (0.11)
)

// builtinGuards are the built-in ritual checks (for the list, totals and the page; ritualStop does the evaluation).
func builtinGuards() []guard {
	return []guard{
		{ID: ritualPlan, Kind: "ritual", Title: "Sort out the plan before work (hook plan query \u2192 submit)", Reason: "if files were changed in a request the hook asked about, submit the sorted plan before finishing", Enabled: true},
		{ID: ritualReflect, Kind: "ritual", Title: "Save the reflection before finishing (reflect query \u2192 submit)", Reason: "if the hook listed conversations, submit the provisional scores, wiki and assessments of delivered lessons before finishing", Enabled: true},
		{ID: ritualKinds, Kind: "ritual", Title: "Give a kind and a scene to pages without them (shikii wiki classify)", Reason: "if the hook listed pages from before the version bump, give them a kind and a scene before finishing", Enabled: true},
		{ID: ritualLang, Kind: "ritual", Title: "Write the final reply in the person's language", Reason: "if Claude Code's language is one written in its own script (Japanese, Chinese, Korean, Russian, Ukrainian) and the final reply is written mostly in Latin letters, write it again in that language before finishing", Enabled: true},
	}
}

func isBuiltinGuard(id string) bool {
	return id == ritualPlan || id == ritualReflect || id == ritualKinds || id == ritualLang
}

// ritualRecovery bounds automatic recovery by saved progress, independently of host continuation flags.
// Left is a decreasing count of the required receipts and pages, never an agent's claim of progress.
type ritualRecovery struct {
	TurnID  string `json:"turn_id"`
	Left    int    `json:"left"`
	Pending bool   `json:"pending"`
}

// ritualStop checks saved results before completion. The first miss requests recovery; another continuation
// is allowed only when the remaining work decreases. Otherwise it stops explicitly incomplete, avoiding a loop.
func ritualStop(root, scope string, in *hookInput, snapshot *hookSession, changed []string, known bool, recheck bool) (hookOut, error) {
	if snapshot.RitualStopped == snapshot.TurnID && snapshot.TurnID != "" {
		recheck = true // background notifications do not carry stop_hook_active
	}
	now := time.Now().UTC()
	var recs []guardRecord
	var failed []string
	remaining := 0
	// Plan: asked for (controls: Planning belongs to this turn) but no submit, and files were changed, or the message is short and
	// outside the acknowledgement list so its grouping is still open (0.9.3: then only continues_previous is asked for)
	// a turn the person's guard stopped is also GroupingAsked (so the Stop after the block judges it): only a short message's grouping was asked for
	grouping := groupingAsk(snapshot) || (snapshot.TurnID != "" && snapshot.GroupingAsked == snapshot.TurnID && groupingShort(groupingOf(snapshot)))
	if p := snapshot.Planning; p != nil && p.TurnID == snapshot.TurnID && ((known && len(changed) > 0) || grouping) {
		pass := p.Result != nil
		reason := ""
		if !pass {
			remaining++
			if known && len(changed) > 0 {
				reason = planningBlockReason(in.SessionID, snapshot.TurnID, len(changed))
				if grouping {
					reason += " Include continues_previous (true or false) for this short message."
				}
			} else {
				reason = groupingBlockReason(in.SessionID, snapshot.TurnID)
			}
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
				remaining += l.targets + l.memories
				if l.wiki {
					remaining++
				}
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
			remaining += len(left)
			reason = fmt.Sprintf("Pages that still need a kind and a scene: %d (%s). Give them with %s wiki classify <page ID> <rule|howto|criteria|fact|history> <scene> (on MCP, operation classify of shikii_wiki), then finish", len(left), strings.Join(left, ", "), hookExe())
			failed = append(failed, reason)
		}
		recs = append(recs, guardRecord{At: now, Session: in.SessionID, Turn: snapshot.TurnID, Guard: ritualKinds, Pass: pass, Reason: reason, Recheck: recheck})
	}
	// Reply language (0.11): every turn while the language is one written in its own script and the reply is long enough to tell
	langFailed := ""
	if snapshot.TurnID != "" {
		lang := hookLang()
		if checked, pass, share := replyLanguageCheck(lang, in.LastAssistantMessage); checked {
			reason := ""
			if !pass {
				remaining++
				reason = replyLanguageReason(lang, share)
				langFailed = reason
			}
			recs = append(recs, guardRecord{At: now, Session: in.SessionID, Turn: snapshot.TurnID, Guard: ritualLang, Pass: pass, Reason: reason, Recheck: recheck})
		}
	}
	if len(recs) > 0 && !in.late {
		if err := appendGuardRecords(root, recs); err != nil {
			return hookOut{}, err
		}
	}
	recovery := &ritualRecovery{TurnID: snapshot.TurnID, Left: remaining, Pending: remaining > 0}
	if remaining == 0 {
		return hookOut{Recovery: recovery}, nil
	}
	if previous := snapshot.RitualRecovery; previous != nil && previous.TurnID == snapshot.TurnID && remaining >= previous.Left {
		// Keep the smallest saved count: new or swapped failures cannot replenish the continuation budget.
		recovery.Left = previous.Left
		return hookOut{Halt: ritualHalt(false), Recovery: recovery}, nil
	}
	if len(failed) == 0 { // only the language: a short block that asks for the reply again, without the ritual wording
		return hookOut{Block: true, Reason: "shikii: the final reply is not in the person's language (the built-in check " + ritualLang + "). Rewrite it before finishing.\n- " + langFailed, Recovery: recovery}, nil
	}
	if langFailed != "" {
		failed = append(failed, langFailed) // with other rituals: one more line of the same block
	}
	var b strings.Builder
	b.WriteString("shikii: required work is not saved yet. Run the commands below now and check their results before finishing. Explaining these steps does not complete them. Use the same project and request IDs; do not ask the person to run these internal steps. If a command fails, inspect the error and correct it before retrying. After saving, briefly restate the result of the original work in your final reply.\n")
	// Claude Code mostly acts on this block rather than on the notices before the request (audit 0.9.2 English, medium 3), so the rule rides along here too
	b.WriteString("- If this turn created or changed a rule, test or procedure (a command or a script), also write it to the wiki as a howto or rule page (add to or edit the closest page) before finishing\n")
	for _, f := range failed {
		b.WriteString("- " + f + "\n")
	}
	return hookOut{Block: true, Reason: strings.TrimRight(b.String(), "\n"), Recovery: recovery}, nil
}

// reflectLeftItem is a requested conversation still waiting for reflection, and what is not done yet.
type reflectLeftItem struct {
	id                string
	targets, memories int
	wiki              bool
	items             []reflectReceipt // the deliveries still to assess (source_id and key), as the agent sends them in memory (0.12)
}

func reflectLeft(root, scope string, asked []string) ([]reflectLeftItem, error) {
	view, err := reflectQueryWith(root, scope, 0) // every pending conversation: what is left is worked out for the ones named, beyond any cap on heads
	if err != nil {
		return nil, err
	}
	want := map[string]bool{}
	for _, id := range asked {
		want[id] = true
	}
	// A lesson delivered to a subagent is assessed under the delivery's own source_id; the view gathers it under the parent conversation.
	deliveries, err := (store.Store{Dir: root}).MemoryDeliveries(scope)
	if err != nil {
		return nil, err
	}
	for _, d := range deliveries {
		if d.Parent != "" && want[d.ID] {
			want[d.Parent] = true
		}
	}
	var out []reflectLeftItem
	for _, s := range view.Sources {
		if !want[s.Source.ID] {
			continue
		}
		item := reflectLeftItem{id: s.Source.ID, targets: len(s.Targets), memories: len(s.Memories), wiki: s.WikiPending}
		for _, m := range s.Memories {
			item.items = append(item.items, reflectReceipt{SourceID: m.SourceID, Key: m.Quote.Key})
		}
		out = append(out, item)
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
const planningShape = `agreed and open are each one object, not an array: "agreed":{"summary":"...","evidence":[{"source":"current_user","quote":"..."}]}, "open":{"unknown":true}; continues_previous is true, false or left out; restates is the turn id of any earlier request of this session (the <id> in history:<id>:user, however far back) or left out`

// groupingAsk reports whether this turn's Stop asks only for continues_previous (0.9.3 real-agent check medium 3): the hook asked for the
// organizing, none came, and the message is short (under outcomeMovedMinRunes) and not in the acknowledgement list, so the list's
// provisional "new request" may be wrong. Claude Code skips the organizing in turns that change no files. Once per turn, like the other rituals.
func groupingAsk(s *hookSession) bool {
	p := s.Planning
	if p == nil || s.TurnID == "" || p.TurnID != s.TurnID || p.Result != nil || s.Judged || s.RitualStopped == s.TurnID {
		return false
	}
	return p.Grouping != nil && !p.Grouping.Settled && groupingShort(p.Grouping)
}

// ritualHalt is shown to the person by the host; detailed failures stay in the hook records.
func ritualHalt(verificationError bool) string {
	loc := hookLoc{Lang: hookLang()}
	if verificationError {
		return bi("shikii: required work could not be verified or its recovery state could not be saved. This turn remains incomplete; automatic continuation was stopped. The agent must inspect the shikii hook log and correct the failed operation before finishing.",
			"shikii: \u5fc5\u8981\u306a\u51e6\u7406\u306e\u78ba\u8a8d\u307e\u305f\u306f\u5fa9\u65e7\u72b6\u614b\u306e\u4fdd\u5b58\u306b\u5931\u6557\u3057\u307e\u3057\u305f\u3002\u672a\u5b8c\u4e86\u306e\u307e\u307e\u81ea\u52d5\u7d99\u7d9a\u3092\u6b62\u3081\u307e\u3057\u305f\u3002AI\u306fshikii\u306ehook\u30ed\u30b0\u3092\u8abf\u3079\u3001\u5931\u6557\u3057\u305f\u51e6\u7406\u3092\u76f4\u3057\u3066\u304b\u3089\u7d42\u3048\u308b\u5fc5\u8981\u304c\u3042\u308a\u307e\u3059\u3002", loc)
	}
	return bi("shikii: required work is still unsaved and recovery made no progress. This turn remains incomplete; automatic continuation was stopped. The agent must save the pending work before finishing.",
		"shikii: \u5fc5\u8981\u306a\u51e6\u7406\u304c\u4fdd\u5b58\u3055\u308c\u305a\u3001\u6b8b\u4ef6\u3082\u6e1b\u3063\u3066\u3044\u307e\u305b\u3093\u3002\u672a\u5b8c\u4e86\u306e\u307e\u307e\u81ea\u52d5\u7d99\u7d9a\u3092\u6b62\u3081\u307e\u3057\u305f\u3002AI\u306f\u6b8b\u3063\u3066\u3044\u308b\u51e6\u7406\u3092\u4fdd\u5b58\u3057\u3066\u304b\u3089\u7d42\u3048\u308b\u5fc5\u8981\u304c\u3042\u308a\u307e\u3059\u3002", loc)
}

// groupingShort reports whether the message is short (under outcomeMovedMinRunes) and not in the acknowledgement list.
func groupingShort(g *requestGrouping) bool {
	return g != nil && !isAckReply(g.Reply) && utf8.RuneCountInString(strings.TrimSpace(g.Reply)) < outcomeMovedMinRunes
}

// groupingBlockReason is the one line of the Stop block that asks only for continues_previous, with the minimal JSON hook plan submit accepts.
func groupingBlockReason(sessionID, turnID string) string {
	b, _ := json.Marshal(map[string]any{"session_id": sessionID, "turn_id": turnID, "agreed": map[string]bool{"unknown": true}, "open": map[string]bool{"unknown": true}, "continues_previous": false})
	return fmt.Sprintf("The person's message is short and not a bare acknowledgement, and this turn was not organized, so it is not known whether it continues the previous request: pass %s on standard input to %s hook plan submit (continues_previous true when it only continues or answers the previous request and asks for nothing new), then finish", b, hookExe())
}

// planningBlockReason is the line of the Stop block when the plan was not sorted out but files were changed.
func planningBlockReason(sessionID, turnID string, changed int) string {
	files := fmt.Sprintf("%d files were", changed)
	if changed == 1 {
		files = "1 file was"
	}
	return fmt.Sprintf("The plan has not been sorted out (hook plan query \u2192 submit), yet %s changed and you are about to finish. Pass %s to %s hook plan query, read this and the recent conversations, submit agreed and open, then finish. %s", files, planJSON(sessionID, turnID), hookExe(), planningShape)
}
