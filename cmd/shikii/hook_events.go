package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/hayaenfj/shikii/internal/store"
)

func runHookEvent(event string) error {
	agent, in, err := readHookInput()
	if err != nil {
		return err
	}
	defer beginEventReads()() // the wiki reads of this event share validated parsing (hook_reads.go)
	stopTurn := ""            // the request a Stop ends: the session's turn when the Stop came (a request that comes since is left open)
	if event == "stop" {
		stopTurn = sessionTurnAt(in.SessionID)
	}
	// the state of the session's turn changes with every event, also when the hook leaves on the way (a project it cannot prepare,
	// a folder that is gone): audit M of 0.31.0 found such a request and Stop leaving the state as it was
	defer sessionTurnOnExit(event, &in, stopTurn)
	common := agent
	moved := ""
	if event == "user-prompt-submit" {
		moved = relinkMovedProject(in.CWD, false) // the folder was moved or renamed: reconnect it here and say so once (0.9.7)
	}
	agent, _, err = prepareHookProject(in.CWD)
	if errors.Is(err, errProjectInactive) {
		// in an uninitialized place, do not save, change Git or start the model. Only when this .shikii was copied from a project still in place, say so
		if event == "user-prompt-submit" {
			if prompt := strings.TrimSpace(in.Prompt); prompt != "" && in.AgentID == "" && !isNotification(prompt) && !isSlashCommand(prompt) {
				noteSessionRequest(in.SessionID, "", "") // a request outside every project: no request of a project goes on (work_handover.go)
				in.stateNoted = true
			}
			notice := copiedNotice(in.CWD)
			if notice != "" && !noticeOnce(in.SessionID, projectScopeAt(in.CWD)) {
				notice = "" // told once in this session
			}
			if b := (hookOut{Context: notice}).json(hookEventName(event)); b != nil {
				fmt.Fprintln(stdout, string(b))
			}
		}
		sessionTurnEvent(event, &in, stopTurn, false) // nothing is checked outside a project, yet the agent's turn ends, or goes on, here too
		in.stateNoted = true
		return nil
	}
	if err != nil {
		if moved != "" { // reconnected, but the hook could not go on: still tell the move, it is not told again (0.9.7 full audit 3, doubt 6)
			if b := (hookOut{Context: moved}).json(hookEventName(event)); b != nil {
				fmt.Fprintln(stdout, string(b))
			}
		}
		return err
	}
	if event != "agent-task" && event != "tool-lesson" {
		ensureModel(common) // start it in the background if missing (this turn just gets no record; do not wait). Not started when only keeping a task for a child or matching a lesson to a tool call
	}
	if event == "user-prompt-submit" {
		judgeAskedTurn(agent, &in, loadSession(agent, in.SessionID)) // the turn that waited for the Stop after its block, when that Stop never came
	}
	// model calls are outside the lock (they only read a copy of the record). Changes to the record are collected in muts and applied to the record re-read inside the lock
	snapshot := loadSession(agent, in.SessionID)
	if event == "stop" {
		if in.AgentID != "" {
			// a Stop with the id of a subagent is not the end of the person's request: nothing is judged or recorded, as a request
			// with that id is not taken (hookUserPrompt). It judged the parent's request on the child's last reply (audit of 0.31.0)
			return nil
		}
		var serr error
		snapshot, serr = readSession(agent, in.SessionID)
		if serr != nil || (in.StopHookActive && snapshot.TurnID == "" && snapshot.GuardTurn == nil && shikiiStoppedSession(agent, in.SessionID)) {
			// the turn ends here (continue:false), incomplete: the same session's requests in other projects end with it
			sessionTurnEvent(event, &in, stopTurn, false)
			in.stateNoted = true
			fmt.Fprintln(stdout, string((hookOut{Halt: ritualHalt(true)}).json(hookEventName(event))))
			return serr
		}
	}
	var out hookOut
	var muts []func(*hookSession)
	var summary string
	var stopRecords []guardRecord // what this Stop writes to records/guards.jsonl, held until the session record is committed
	overtaken := false            // the next request replaced this Stop's turn: the Stop says nothing
	switch event {
	case "user-prompt-submit":
		out, muts, err = hookUserPrompt(agent, &in, snapshot)
	case "stop":
		in.stopRecords = &stopRecords
		out, muts, err = hookStop(agent, &in, snapshot)
		// A Stop that a check did not stop lets the turn end, with or without a last message, and so does one whose judgment failed
		// (the model of the closing questions was not reachable): sessionTurnEvent below ends the session's turn. A Stop that
		// carries the id of a subagent is not the end of the person's request.
		muts = append(muts, func(s *hookSession) { summary = stopSummary(agent, s) }) // the waiting-answers summary is counted on the record after this turn's decisions are added
	case "subagent-start":
		out, muts, err = hookSubagentStart(agent, &in, snapshot) // the parent's record only drops the used task for the child
	case "agent-task":
		muts = hookAgentTask(&in) // output nothing (do not stop the tool)
	case "tool-lesson":
		// a guard the person enabled refuses a command matching a pattern before it runs (0.11); otherwise a lesson naming the file or
		// command: context only (never stops the tool); records the delivery under the session lock itself
		if out, err = guardToolCall(agent, &in, snapshot); err == nil && !out.Deny {
			out, err = hookToolLesson(agent, &in, snapshot)
		}
	}
	if moved != "" {
		out.Context = joinLines(moved, out.Context)
	}
	blocked := out.Block // whether the agent's turn goes on after this event
	if len(muts) > 0 {
		if serr := saveHookMutations(agent, in.SessionID, event, snapshot, &out, muts, stopRecords); serr != nil {
			if err == nil {
				err = serr
			}
			if event == "stop" && errors.Is(serr, errStopOvertaken) {
				overtaken = true
				out = hookOut{} // the record is the next request's by now: this Stop has nothing to verify and must not end that request's work
			} else if event == "stop" {
				out.Halt, out.Block, out.Reason = ritualHalt(true), false, ""
				blocked = false // the turn ends incomplete (continue:false), as with a record that cannot be read (audit of 0.31.0)
			}
		}
	}
	sessionTurnEvent(event, &in, stopTurn, blocked) // the session's turn ends, or goes on, with this event
	in.stateNoted = true
	if event == "user-prompt-submit" || event == "stop" {
		sweepDeferrals(agent, in.SessionID) // other sessions' outcomes left waiting by a session that was closed mid-turn (outside this session's lock)
	}
	if event == "user-prompt-submit" && len(muts) > 0 {
		// only when the person's request moved on. A notification has the same TurnID, so it adds no wiki work.
		current := loadSession(agent, in.SessionID)
		if current.TurnID != snapshot.TurnID {
			// chosen, decided (lesson-applies, a model call of seconds) and recorded on a copy outside the session lock (audit A of 0.12: the
			// lock was held through the model call, and a parallel hook gives it up after 2 seconds and writes without it); the lock then
			// only adds the delivered versions, so memories delivered to the same session stay deduplicated
			working := *current
			working.Memories = append([]memoryDelivery(nil), current.Memories...)
			context, merr := memoryContext(agent, working.Scope, cut(in.Prompt, requestMax), &working)
			if merr != nil {
				hookLog("memory: " + merr.Error())
			} else if context != "" {
				out.Context = joinLines(out.Context, context)
				had := map[string]bool{}
				for _, m := range current.Memories {
					had[m.Key+"@"+m.At.Format(time.RFC3339Nano)] = true
				}
				var added []memoryDelivery
				for _, m := range working.Memories {
					if !had[m.Key+"@"+m.At.Format(time.RFC3339Nano)] {
						added = append(added, m)
					}
				}
				if uerr := updateSession(agent, in.SessionID, func(s *hookSession) {
					if s.TurnID != current.TurnID || s.Scope != current.Scope {
						return
					}
					s.Memories = keepMemories(append(s.Memories, added...), s.HeadAt)
				}); uerr != nil {
					hookLog("memory: " + uerr.Error())
				}
			}
			context, ids, rerr := reflectContext(agent, current.Scope)
			if rerr != nil {
				hookLog("reflect: " + rerr.Error())
				ids = nil // a turn where asking failed does not stop on the previous turn's leftovers (audit R5)
			} else if context != "" {
				out.Context = joinLines(out.Context, context) // one form every time; the shape of submit comes back in the reply of reflect query (0.11)
			}
			// write the asked conversations and turn to the record (the ritual check looks at it at Stop: was the reflection done). A turn that did not ask is cleared
			if uerr := updateSession(agent, in.SessionID, func(s *hookSession) {
				if s.TurnID == current.TurnID {
					s.ReflectAsked, s.ReflectAskedTurn = ids, current.TurnID
				}
			}); uerr != nil {
				hookLog("reflect: " + uerr.Error())
			}
			// have pages from before the version bump (no kind, no scene) filled in 5 at a time (0.9.0). The asked pages are written to the record; the ritual check at Stop looks at them
			kctx, kids, kerr := classifyContext(agent, current.Scope)
			if kerr != nil {
				hookLog("classify: " + kerr.Error())
				kids = nil
			}
			out.Context = joinLines(out.Context, kctx)
			if uerr := updateSession(agent, in.SessionID, func(s *hookSession) {
				if s.TurnID == current.TurnID {
					s.ClassifyAsked, s.ClassifyAskedTurn = kids, current.TurnID
				}
			}); uerr != nil {
				hookLog("classify: " + uerr.Error())
			}
			if in.AgentID == "" {
				upgrade, key, uerr := memoryUpgradeHook(agent, current.Scope, current)
				if uerr != nil {
					hookLog("memory upgrade: " + uerr.Error())
				} else {
					out.Context = joinLines(out.Context, upgrade.Context)
					out.System = joinLines(out.System, upgrade.System)
					if e := updateSession(agent, in.SessionID, func(s *hookSession) {
						if s.TurnID == current.TurnID {
							s.MemoryUpgrade = key
						}
					}); e != nil {
						hookLog("memory upgrade: " + e.Error())
					}
				}
			}
			out.Context = joinLines(out.Context, internalLineSummary(agent))
			out.Context = joinLines(out.Context, variantsContext(agent))
			out.Context = joinLines(out.Context, wordingCheckContext(agent))
		}
	}
	if event == "stop" && !overtaken { // the one-time notices wait for a Stop that still has its turn
		if line := wordingSummary(agent); line != "" {
			ensurePage(agent)
			summary = joinLines(summary, line)
		}
		if line := lineSummary(agent); line != "" { // the line was drawn / cannot be drawn (0.8.3). Only once, when the state changes
			summary = joinLines(summary, line)
		}
	}
	if summary != "" {
		out.System = joinLines(out.System, summary)
	}
	if b := out.json(hookEventName(event)); b != nil {
		fmt.Fprintln(stdout, string(b))
	}
	if event == "stop" {
		// drawing lines and trying wordings run outside the lock as separate processes (slow-cycle work, like the lint of Karpathy's wiki)
		for _, id := range hookSpecs {
			cerr, terr := stopSpecChecks(agent, id)
			if cerr != nil && err == nil {
				err = cerr
			}
			if terr != nil && err == nil {
				err = terr
			}
		}
		// wiki insertion points and contradictions, and lesson-applies (0.12), also get lines on the same cycle once outcome truths pile up
		for _, id := range append(append([]string{}, wikiSpecs...), deliverySpecs...) {
			if cerr := hookMaybeCalibrate(agent, id); cerr != nil && err == nil {
				err = cerr
			}
		}
		if werr := notebookMaintenance(agent, snapshot.Scope); werr != nil {
			hookLog("wiki lint: " + werr.Error())
		}
	}
	return err
}

// isSlashCommand reports whether the message is a slash command (/model, /plugin:skill args), which is not a request. Only the
// first word decides: a command name has letters, digits, '_', ':' and '-'. A first word with a second slash, a dot or any other
// character is a file path the person pasted or dragged in ("/Users/me/notes.txt add this to the README"), and that is a request.
func isSlashCommand(prompt string) bool {
	p := strings.TrimSpace(prompt)
	if !strings.HasPrefix(p, "/") {
		return false
	}
	word := p
	if i := strings.IndexFunc(p, unicode.IsSpace); i >= 0 {
		word = p[:i]
	}
	for _, r := range word[1:] {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == ':' || r == '-') {
			return false
		}
	}
	return true
}

func joinLines(a, b string) string {
	switch {
	case a == "":
		return b
	case b == "":
		return a
	}
	return a + "\n" + b
}

func hookEventName(event string) string {
	switch event {
	case "user-prompt-submit":
		return "UserPromptSubmit"
	case "subagent-start":
		return "SubagentStart"
	case "agent-task", "tool-lesson":
		return "PreToolUse"
	case "session-start":
		return "SessionStart"
	}
	return "Stop"
}

// isNotification reports whether the input is one where Claude Code streams the completion of background work (subagents, background commands) as a "user message":
// <task-notification> / [SYSTEM NOTIFICATION - NOT USER INPUT] (not the person's request. Measured: nearly half of the records were these).
func isNotification(prompt string) bool {
	p := strings.TrimSpace(prompt)
	if strings.HasPrefix(p, "<task-notification>") || strings.HasPrefix(p, "<system-reminder>") {
		return true
	}
	head := p
	if len(head) > 300 {
		head = head[:300]
	}
	return strings.Contains(head, "[SYSTEM NOTIFICATION - NOT USER INPUT]")
}

// hookSubagentStart delivers lessons in the same form when a subagent (Claude Code's Agent tool) starts.
// The subagent's task text arrives in neither UserPromptSubmit nor SubagentStart. Since 0.9.1 the task text the parent passed to the Agent tool is
// kept at PreToolUse (agent-task), and the oldest kept task of a child of the same type picks the lessons. Without one, only the rules that apply to every request (scene "いつも") are delivered.
// Up to 0.9.0 lessons were picked by the parent's request, and lessons unrelated to the child's work arrived (measured: of 37 deliveries to children, 4 helped).
// Deliveries are recorded under an ID separate from the parent's (a hash of the turn and the subagent). Nothing is output if there is no parent request yet. No decisions (the 2 questions) are made.
func hookSubagentStart(agent string, in *hookInput, snapshot *hookSession) (hookOut, []func(*hookSession), error) {
	if snapshot.Request == "" || snapshot.Scope == "" || snapshot.TurnID == "" {
		return hookOut{}, nil, nil
	}
	kind := in.AgentType
	if kind == "" {
		kind = "subagent"
	}
	// the kept task is picked and dropped inside the lock (picking from a copy outside the lock lets children of the same type started side by side use the same task. audit 0.9.1 mid-2)
	var task subagentTask
	var ok bool
	if err := updateSession(agent, in.SessionID, func(s *hookSession) {
		if task, ok = pickSubagentTask(s.SubagentTasks, in.AgentType, time.Now()); ok {
			s.SubagentTasks = dropSubagentTask(s.SubagentTasks, task)
		}
	}); err != nil {
		return hookOut{}, nil, err
	}
	var muts []func(*hookSession)
	sub := hookSession{Scope: snapshot.Scope} // no record of delivered versions: deliver to the subagent even if the parent already received them
	var context, head string
	var err error
	if ok {
		context, err = memoryContextFor(agent, snapshot.Scope, cut(task.Prompt, requestMax), &sub, snapshot.TurnID, in.AgentID)
		head = "shikii: delivering to this subagent (" + kind + ") memories close to its task \"" + cut(strings.Join(strings.Fields(task.Prompt), " "), 80) + "\"."
	} else {
		context, err = alwaysContextFor(agent, snapshot.Scope, snapshot.Request, &sub, snapshot.TurnID, in.AgentID)
		head = "shikii: delivering to this subagent (" + kind + ") the rules that apply to every request."
	}
	if err != nil {
		return hookOut{}, muts, err
	}
	// the report location is delivered to subagents too (do not scatter audit and investigation reports inside the repo. 0.8.1)
	if context == "" {
		return hookOut{Context: subagentHandoffNotice}, muts, nil
	}
	return hookOut{Context: joinLines(joinLines(head, context), subagentHandoffNotice)}, muts, nil
}

// hookAgentTask, at PreToolUse (only the Agent / Task tools), adds the task text and type passed to the child agent to the parent's record. The tool is not stopped
func hookAgentTask(in *hookInput) []func(*hookSession) {
	if in.ToolName != "Agent" && in.ToolName != "Task" {
		return nil
	}
	var input struct {
		Prompt       string `json:"prompt"`
		SubagentType string `json:"subagent_type"`
	}
	if json.Unmarshal(in.ToolInput, &input) != nil || strings.TrimSpace(input.Prompt) == "" {
		return nil
	}
	typ := input.SubagentType
	if typ == "" {
		typ = "general-purpose" // a child started without a type starts as general-purpose (treated as the same type for the 30-second drop rule)
	}
	task := subagentTask{Type: typ, Prompt: cut(strings.TrimSpace(input.Prompt), requestMax), At: time.Now().UTC()}
	return []func(*hookSession){func(s *hookSession) {
		kept := []subagentTask{}
		for _, t := range s.SubagentTasks {
			if time.Since(t.At) >= subagentTaskTTL || t.Type == task.Type && task.At.Sub(t.At) > subagentTaskBurst {
				continue
			}
			kept = append(kept, t)
		}
		kept = append(kept, task)
		if len(kept) > subagentTaskMax {
			kept = kept[len(kept)-subagentTaskMax:]
		}
		s.SubagentTasks = kept
	}}
}

// pickSubagentTask picks, among records of the same type as the started child, the oldest one that is not too old. A child started without a type starts as general-purpose
func pickSubagentTask(tasks []subagentTask, agentType string, now time.Time) (subagentTask, bool) {
	for _, t := range tasks {
		typ := t.Type
		if typ == "" {
			typ = "general-purpose"
		}
		if typ == agentType && now.Sub(t.At) < subagentTaskTTL {
			return t, true
		}
	}
	return subagentTask{}, false
}

// dropSubagentTask drops exactly one used record (if two records have the same text, one stays)
func dropSubagentTask(tasks []subagentTask, used subagentTask) []subagentTask {
	for i, t := range tasks {
		if t.Type == used.Type && t.Prompt == used.Prompt && t.At.Equal(used.At) {
			return append(append([]subagentTask{}, tasks[:i]...), tasks[i+1:]...)
		}
	}
	return tasks
}

// habitsNotice is a one-line note on working habits that arrives even without working rules (once per session). subagentHandoffNotice is where subagents put their reports.
const (
	habitsNotice          = "shikii: Put reports, audits, investigation notes, diffs and images that a person or another agent will read later in the work records .shikii/handoff/<work>/, not inside the repo (shikii handoff write <path>, or handoff_write in shikii_wiki; the person reads them in the knowledge section of the page). Point subagents to the same place for their output. When the person states a rule or correction for the future, turn it into a wiki lesson right away instead of waiting for the next reflection (shikii wiki capture → reflect place → reflect submit). When you create or change a rule, test or procedure (commands or scripts) yourself, write it right away to a howto or rule page in the wiki even if nobody asked (add to or edit the closest page if there is one)."
	subagentHandoffNotice = "shikii: If you leave a report or working files, write them in the work records .shikii/handoff/<work>/, not inside the repo (shikii handoff write <path>, or handoff_write in shikii_wiki). If the parent named a location, use that."
)

// hookUserPrompt asks the two pre-thinking questions (prompt-kind and needs-plan) and keeps the request and the working tree state. The outcome truth of the previous request is decided here (when the next request arrives).
// ackReplies are the bare acknowledgements that are attached to the previous request (0.9.3). An allowlist: anything else, however short,
// is a request of its own and starts a new conversation (Japanese requests are short, and the old 40-character rule merged a Codex
// session's 19 requests into one conversation, so reflection almost never ran). Compared after lowercasing and trimming punctuation and spaces.
var ackReplies = map[string]bool{"y": true, "yes": true, "yeah": true, "yep": true, "yup": true, "ok": true, "okay": true, "k": true, "sure": true, "go": true, "go on": true, "go ahead": true, "continue": true, "next": true, "proceed": true, "do it": true, "lgtm": true, "sounds good": true, "thanks": true, "thank you": true, "please": true, "please do": true, "yes please": true, "ok go": true, "ok thanks": true, "agreed": true, "fine": true, "はい": true, "うん": true, "ええ": true, "了解": true, "了解です": true, "承知しました": true, "わかりました": true, "分かりました": true, "いいよ": true, "いいです": true, "いいですよ": true, "大丈夫": true, "大丈夫です": true, "どうぞ": true, "お願い": true, "お願いします": true, "おねがい": true, "おねがいします": true, "よろしく": true, "よろしくお願いします": true, "続けて": true, "続けてください": true, "続けましょう": true, "では続けましょう": true, "じゃあ続けて": true, "次へ": true, "次": true, "次に進んで": true, "進めて": true, "進めてください": true, "進めましょう": true, "やって": true, "やってください": true, "やりましょう": true, "ではやりましょう": true, "それで": true, "それでお願いします": true, "それでいい": true, "それでいいよ": true, "ありがとう": true, "ありがとうございます": true, "いいね": true, "そうして": true, "そうしましょう": true, "了解しました": true, "わかった": true, "分かった": true}

func isAckReply(s string) bool {
	// full-width letters and digits as their ASCII forms (an IME types a full-width OK), and no variation selectors or joiners (an emoji picker adds U+FE0F after a thumbs-up)
	t := strings.Map(func(r rune) rune {
		switch {
		case r >= 0xFF01 && r <= 0xFF5E:
			return r - 0xFEE0
		case r >= 0xFE00 && r <= 0xFE0F, r == 0x200D:
			return -1
		}
		return r
	}, s)
	t = strings.ToLower(strings.TrimSpace(t))
	t = strings.TrimFunc(t, func(r rune) bool { return unicode.IsPunct(r) || unicode.IsSpace(r) || unicode.IsSymbol(r) })
	t = strings.Join(strings.Fields(t), " ")
	return t == "" || ackReplies[t]
}

// groupedRequest is this message's request and page text, attached to the previous request (joined) or standing alone.
func groupedRequest(snapshot *hookSession, req, prompt, scope string, joined bool) (string, string) {
	request := req
	if joined && snapshot.Request != "" {
		request = cut(snapshot.Request+" / "+req, requestMax)
	}
	text := strings.TrimSpace(prompt)
	if joined && snapshot.Request != "" && snapshot.Scope == scope {
		previous := snapshot.RequestText
		if previous == "" {
			previous = snapshot.Request
		}
		text = previous + " / " + text
	}
	if !strings.HasPrefix(text, request) {
		text = ""
	}
	return request, cut(text, store.RecordTextLimit+utf8.UTFMax)
}
