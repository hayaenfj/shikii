package main

import (
	"os"
	"strings"
	"time"

	"github.com/hayaenfj/shikii/internal/decide"
	"github.com/hayaenfj/shikii/internal/spec"
	"github.com/hayaenfj/shikii/internal/store"
)

func hookUserPrompt(agent string, in *hookInput, snapshot *hookSession) (hookOut, []func(*hookSession), error) {
	req := cut(strings.TrimSpace(in.Prompt), requestMax)
	if req == "" || in.AgentID != "" {
		return hookOut{}, nil, nil
	}
	if isSlashCommand(req) {
		// not a request: nothing is asked or delivered, and the kept request stays as it is. Only the person's guards get a boundary here,
		// so that this turn's changes and commits are checked at its Stop (guard_turn.go)
		return hookOut{}, openGuardTurn(agent, in, snapshot), nil
	}
	if isNotification(req) {
		// a completion notice of background work is not a request: the 2 questions are not asked and the request, listing and pending are not replaced (the outcome truth is decided at the person's next request).
		// if work continues after the notice, judge the original request once more at the end of that turn (reset Judged)
		// the agent works on: the session's turn is reopened wherever the shell stands (sessionTurnEvent). Only the session's last
		// request goes on, so this project's request is judged again only when it is that one: a notice that came while the shell
		// stood in the project of an older request reopened it (audits of 0.31.0)
		latest, known, _ := readSessionLatest(in.SessionID)
		if snapshot.Request == "" || !known || latest.Root != agent || latest.Turn != snapshot.TurnID {
			return hookOut{}, nil, nil
		}
		return hookOut{}, []func(*hookSession){func(s *hookSession) {
			if s.Request == snapshot.Request {
				s.Judged = false
				closeGuardTurn(agent, in.SessionID, s) // work that goes on after the notice is the request's again, as before a slash command came in between
			}
		}}, nil
	}
	// a bare acknowledgement ("ok", "continue") does not replace the request but is attached to the previous one: so the end-of-turn questions do not read "ok" as the request.
	// The list is the provisional decision: when the agent organizes this turn, its continues_previous corrects it (hook plan submit, 0.9.3)
	joined := isAckReply(req)
	inputs := map[string]string{"request": req}
	if snapshot.Previous != "" {
		inputs["previous"] = snapshot.Previous // the context stage (used only when unsure)
	}
	tree, listing, listed, project, scope := treeStateWithScope(in.CWD) // the current tree: both the "now" that settles the previous request's outcome and this request's record (both in one go)
	pageURL, pageStarted := ensureProjectPage(scope)                    // when the agent starts working (first request), start one page for this project (serve opens it in the browser; the hook does not wait. 0.8.0)
	head := gitHead(in.CWD)                                             // the starting point the stop check uses to see this turn's commits
	request, requestText := groupedRequest(snapshot, req, in.Prompt, scope, joined)
	var lines []byte // keyed hashes of the lines of the files not committed now, for the guards (guard_lines.go)
	if listed {
		lines = uncommittedLines(agent, in.CWD, listingHeader(request), listing)
	}
	turnID := store.NewTraceID()
	previous, _, _ := readSessionLatest(in.SessionID) // the request this one replaces: whether its work is handed over (work_handover.go)
	noteSessionRequest(in.SessionID, agent, turnID)
	in.stateNoted = true
	planning := newPlanningFrame(snapshot, in, scope, turnID)
	if sp, err := spec.Load(hookSpecPath(agent, "needs-plan")); err != nil || !needsPlanningContext(sp) {
		planning.ReasoningOnly = true
	}
	var firstErr error
	pendingNow := snapshot.Pending // the previous request's pending (a copy). Only these traces are settled inside the lock (pending added by an overlapping Stop or the next request is left alone)
	muts := []func(*hookSession){
		func(s *hookSession) {
			if ferr := observeFeedback(agent, s, snapshot, scope, req); ferr != nil {
				hookLog("feedback: " + ferr.Error())
			}
			handover := interruptedHandover(s, scope, agent, previous) // read before the request below replaces this one
			settleGrouping(agent, s)                                   // the previous turn's grouping, if its organizing and Stop never came
			previous := linkedCompletion(s, snapshot, scope)
			if planning != nil && !planning.ReasoningOnly && snapshot.Request != "" {
				// the agent is asked to organize this turn: settling the previous outcome waits for its continues_previous (or this turn's Stop)
				g := &requestGrouping{Joined: joined, Reply: req}
				g.Alone, g.AloneText = groupedRequest(snapshot, req, in.Prompt, scope, false)
				g.Together, g.TogetherText = groupedRequest(snapshot, req, in.Prompt, scope, true)
				if previous != nil {
					c := *previous
					g.Previous = &c
				}
				planning.Grouping = g
			} else if derr := settleDone(agent, previous, req); derr != nil {
				hookLog("outcome: " + derr.Error())
			}
			nextReasoning := nextTurnReasoning(nil, turnID)
			if s.Scope == scope {
				nextReasoning = nextTurnReasoning(s.Reasoning, turnID)
			}
			s.Request, s.Judged, s.TurnID, s.Scope, s.Completed = request, false, turnID, scope, nil
			closeGuardTurn(agent, in.SessionID, s) // a stretch a slash command opened ends with the person's next request
			s.Head, s.HeadAt = head, time.Now()
			s.RequestText = requestText
			s.RawPrompt = cut(strings.TrimSpace(in.Prompt), 4000)
			if planning != nil {
				planning.Handover = handover
			}
			s.Planning = planning
			s.Reasoning = nextReasoning
		},
		// the previous request's outcome truth is decided on the tree at the time the next request arrives (the request boundary = the next request; waiting for Stop mixes two requests)
		func(s *hookSession) {
			if err := settlePending(agent, s, tree, pendingNow, true); err != nil && firstErr == nil {
				firstErr = err
			}
			if err := settleScope(agent, s, listing, listed, true); err != nil && firstErr == nil {
				firstErr = err
			}
		},
		func(s *hookSession) {
			p := sessionFilesPath(agent, in.SessionID)
			lp := sessionLinesPath(agent, in.SessionID)
			if !listed {
				_ = os.Remove(p) // outside git (or too slow, or too large): do not keep the previous request's listing and compare against it
				_ = os.Remove(lp)
				return
			}
			// a header saying "which request's listing this is": Stop only compares with the listing of the request it saw (not with one rewritten by the next request)
			if err := writeAtomic(p, []byte(listingHeader(request)+listing)); err != nil && firstErr == nil {
				firstErr = err
			}
			if err := keepLines(lp, lines); err != nil { // without the table those files are compared with HEAD, as before
				hookLog("guard: the lines of the uncommitted files were not kept: " + err.Error())
			}
		},
	}
	ds, outs, err := hookDecideAll(agent, hookSpecs[:2], inputs, project, func(id string) bool { return id == "needs-plan" && planning != nil && !planning.ReasoningOnly }, map[string]string{"request": strings.TrimSpace(in.Prompt)})
	if err != nil && firstErr == nil {
		firstErr = err
	}
	if planning != nil && planningNotNeeded(ds[0]) {
		planning.ReasoningOnly = true // Keep evidence-linked work available for non-editing requests without adding a planning decision.
	}
	var out hookOut
	if planning != nil {
		out.Context = planningNotice(in.SessionID, turnID) // one form every time; the shape of submit comes back in the reply of hook plan query (0.11)
		if planning.ReasoningOnly {
			out.Context = reasoningNotice(in.SessionID, turnID)
		}
	}
	for i, d := range ds {
		if d == nil || d.Band == decide.Abstain {
			continue
		}
		if hookSpecs[i] == "prompt-kind" && tree != "" { // keep the working tree hash for the outcome truth
			muts = append(muts, func(s *hookSession) {
				s.Pending = append(s.Pending, pendingItem{Trace: d.TraceID, Tree: tree, At: time.Now()})
			})
		}
		muts = append(muts, noteAsked(hookSpecs[i], d))
		out.Context = joinLines(out.Context, outs[i].Context)
		out.System = joinLines(out.System, outs[i].System)
	}
	_ = pageStarted                             // say nothing when it was started (serve opens it in the browser; the hook stays silent while there is no line)
	if pageURL != "" && !snapshot.PageNoticed { // the location of the running page, once per session
		out.Context = joinLines(out.Context, "shikii: this project's page: "+pageURL+"/")
		muts = append(muts, func(s *hookSession) { s.PageNoticed = true })
	}
	// tell once per session how to keep work records and rules, so it arrives even in projects without working rules (CLAUDE.md / AGENTS.md) (0.8.1).
	// only added when the hook already says something this turn (a turn that is silent while there is no line stays silent)
	if out.Context != "" && !snapshot.HabitsNoticed {
		out.Context = joinLines(out.Context, habitsNotice)
		muts = append(muts, func(s *hookSession) { s.HabitsNoticed = true })
	}
	// notice without being told: if this project's agent memory has files not yet moved to the wiki, say so (0.8.0). Once per session
	if !snapshot.MemoryNoticed {
		if notice := memoryImportNotice(scope); notice != "" {
			out.Context = joinLines(out.Context, notice)
			muts = append(muts, func(s *hookSession) { s.MemoryNoticed = true })
		}
	}
	// after the folder was moved or renamed, if a wiki store is left under the old path's name, say so (0.9.2). Once per session
	if !snapshot.StoreNoticed {
		if notice := otherPathNotice(agent, scope); notice != "" {
			out.Context = joinLines(out.Context, notice)
			muts = append(muts, func(s *hookSession) { s.StoreNoticed = true })
		}
	}
	// an older shikii on this machine registered this project again: only told, once per session (0.9.7)
	if !snapshot.OlderNoticed {
		if notice := olderShikiiNotice(scope); notice != "" {
			out.Context = joinLines(out.Context, notice)
			muts = append(muts, func(s *hookSession) { s.OlderNoticed = true })
		}
	}
	return out, muts, firstErr
}

// planningNotNeeded reports whether the pre-start organizing can be skipped: only when prompt-kind has a line and confidently (act) answered explain, investigate or run (no file changes).
// A reply or choice on a proposal (decide) needs agreement, so it is not skipped. Without a line it is not skipped (the reason to skip has not been measured).
func planningNotNeeded(d *decide.Decision) bool {
	if d == nil || !d.IsCalibrated || d.Band != decide.Act {
		return false
	}
	switch d.Top {
	case "explain", "investigate", "run":
		return true
	}
	return false
}
