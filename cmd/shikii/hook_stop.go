package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/hayaenfj/shikii/internal/decide"
	"github.com/hayaenfj/shikii/internal/store"
)

// hookStop asks the two post-thinking questions (changes-in-scope and done-as-asked) with the request, the changed file list and the last reply together.
// Only once per request (Judged). Not asked if there is no kept request. If nothing changed, changes-in-scope is not asked.
// It also keeps the assistant's last message and attaches outcome truths to the remaining pending. The waiting-answers summary is added by the caller inside the lock (stopSummary).
func hookStop(agent string, in *hookInput, snapshot *hookSession) (hookOut, []func(*hookSession), error) {
	if snapshot.GuardTurn != nil {
		// a slash command opened this stretch: it is not a request, so only the person's guards look at it. Nothing of the kept request
		// is settled, checked by the routine or judged at this Stop, and its record stays as it is
		pruneSessions(agent)
		out, err := guardTurnStop(agent, in, snapshot)
		if err != nil { // the guards could not be applied or their records not kept: end incomplete instead of passing in silence
			out = hookOut{Halt: ritualHalt(true)}
		}
		return out, nil, err
	}
	tree, listing, listed, project, scope := treeStateWithScope(in.CWD)
	last := cut(strings.TrimSpace(in.LastAssistantMessage), requestMax)
	req := snapshot.Request // the request this Stop evaluates. If the kept request changed inside the lock (the next request came first), this Stop's marks are not applied
	pendingNow := snapshot.Pending
	recheck := snapshot.Judged || in.StopHookActive || snapshot.RitualStopped == snapshot.TurnID && snapshot.TurnID != ""
	readyToJudge := false // grouping is settled only after all required work is saved
	var firstErr error
	muts := []func(*hookSession){
		func(s *hookSession) {
			if s.TurnID == snapshot.TurnID && readyToJudge {
				settleGrouping(agent, s) // the Stop judges this turn with the grouping it has: a later organizing no longer moves it
				// an organizing that landed while this Stop was asking the model (after its snapshot) regrouped the request this Stop judged:
				// the Stop's grouping wins, as for an organizing after the Stop, so the judgment and the reply are kept (0.9.3 audit 3)
				if g := groupingOf(s); g != nil && req != "" && s.Request != req && (req == g.Alone || req == g.Together) {
					moved := s.Request
					g.Joined = req == g.Together
					s.Request, s.RequestText = req, g.AloneText
					if g.Joined {
						s.RequestText = g.TogetherText
					}
					p := sessionFilesPath(agent, in.SessionID)
					if b, err := os.ReadFile(p); err == nil && strings.HasPrefix(string(b), listingHeader(moved)) {
						if err := writeAtomic(p, []byte(listingHeader(req)+string(b[len(listingHeader(moved)):]))); err != nil {
							hookLog("grouping: " + err.Error())
						}
					}
				}
			}
			if last != "" && s.Request == req && s.TurnID == snapshot.TurnID {
				s.Previous = last
				if s.TurnID != "" && scope != "" && s.Scope == scope {
					var targets []store.ReviewTarget
					if s.Completed != nil && s.Completed.TurnID == s.TurnID {
						targets = s.Completed.Targets
					}
					s.Completed = &completedTurn{TurnID: s.TurnID, Request: req, Summary: last, At: time.Now(), Targets: targets}
				}
			}
		},
		func(s *hookSession) {
			if err := settlePending(agent, s, tree, pendingNow, false); err != nil && firstErr == nil {
				firstErr = err
			}
			if err := settleScope(agent, s, listing, listed, false); err != nil && firstErr == nil {
				firstErr = err
			}
		},
	}
	pruneSessions(agent)
	if req == "" {
		return hookOut{}, muts, firstErr
	}
	var changed []string
	known := false
	var before []byte
	if listed {
		var err error
		if before, err = os.ReadFile(sessionFilesPath(agent, in.SessionID)); err == nil {
			if !strings.HasPrefix(string(before), listingHeader(req)) {
				return hookOut{}, muts, firstErr // the listing already belongs to the next request: this Stop lost what to compare with and does not evaluate (the next request's Stop will)
			}
			changed, known = changedFiles(string(before), listing), true
		}
	}
	var out hookOut
	var checkpoint func(*hookSession)
	{
		// the routine check comes first: its recovery count decides alone whether the turn ends incomplete, and a guard stops nothing at such a Stop
		rout, rerr := ritualStop(agent, snapshot.Scope, in, snapshot, changed, known, recheck)
		if rerr != nil {
			if firstErr == nil {
				firstErr = rerr
			}
			rout.Halt = ritualHalt(true)
			rout.Recovery = &ritualRecovery{TurnID: snapshot.TurnID, Pending: true, FilesKnown: known}
		}
		// stop checks (0.7.3): apply enabled guards to the changed files, added lines and this turn's commits. If one is hit, stop the Stop and return the reason.
		// A guard that has not stopped this turn yet stops it once, also after the routine check or another guard stopped an earlier Stop
		if !in.late {
			var gerr error
			if out, gerr = guardStop(agent, in, guardBaselineOf(agent, in.SessionID, snapshot), string(before), changed, known, rout.Halt != ""); gerr != nil && firstErr == nil {
				firstErr = gerr
			}
		}
		if rout.Recovery != nil {
			recovery := rout.Recovery
			checkpoint = func(s *hookSession) {
				if s.TurnID == snapshot.TurnID && workHash(s.Reasoning) == workHash(snapshot.Reasoning) {
					saved := *recovery
					if previous := s.RitualRecovery; previous != nil && previous.TurnID == s.TurnID {
						saved.Left = min(saved.Left, previous.Left)
						saved.FilesKnown = saved.FilesKnown || previous.FilesKnown
					}
					s.RitualRecovery = &saved
				}
			}
			muts = append(muts, checkpoint)
		}
		out.System = joinLines(out.System, rout.System)
		if rout.Halt != "" {
			out.Halt, out.Block, out.Reason = rout.Halt, false, ""
		} else if rout.Block {
			out.Block, out.Reason = true, joinLines(out.Reason, rout.Reason)
		}
	}
	if out.Block || out.Halt != "" {
		turn := snapshot.TurnID
		muts = append(muts, func(s *hookSession) {
			if s.TurnID == turn {
				s.RitualStopped = turn
				if !s.Judged {
					s.GroupingAsked = turn
				}
			}
		})
		return out, muts, firstErr
	}
	if snapshot.Judged {
		return out, nil, firstErr // a repeated Stop must not replace the final reply or judge it twice
	}
	// Persist the verification checkpoint before producing any completion judgment.
	// Model calls stay outside the lock. A later request must not inherit this Stop's checkpoint.
	if checkpoint != nil {
		current := false
		if err := updateSession(agent, in.SessionID, func(s *hookSession) {
			current = s.TurnID == snapshot.TurnID && s.Request == req
			if current {
				checkpoint(s)
			}
		}); err != nil {
			out.Halt, out.Block, out.Reason = ritualHalt(true), false, ""
			return out, nil, err
		}
		if !current {
			return hookOut{}, nil, firstErr
		}
	}
	readyToJudge = true
	inputs := map[string]string{"request": snapshot.Request, "files": filesInput(changed, known), "summary": last}
	if inputs["summary"] == "" {
		inputs["summary"] = "(none)"
	}
	originals := map[string]string{"summary": strings.TrimSpace(in.LastAssistantMessage)}
	if snapshot.RequestText != "" {
		originals["request"] = snapshot.RequestText
	}
	completionCommit := workCommitter(func(action func() error) error { return commitStopReasoning(agent, in.SessionID, snapshot, action) })
	ds, outs, err := hookDecideAllWithCommit(agent, hookSpecs[2:], inputs, project, func(id string) bool {
		return id == "changes-in-scope" && (!known || len(changed) == 0) // if nothing changed (or it is unknown), "within scope" cannot be asked
	}, completionCommit, originals)
	if err != nil && firstErr == nil {
		firstErr = err
	}
	if err := completionCommit(nil); err != nil {
		readyToJudge = false
		if errors.Is(err, errStopOvertaken) {
			return hookOut{}, nil, err // the next request came while the model was asked: its own Stop judges it, this one says nothing
		}
		reason := "shikii: completion evidence changed or could not be checked. Read the current work context, resolve pending notes and save reasoning finish before ending. " + err.Error()
		if !in.late { // shikii's own stop: kept like the routine check's, so that a later Stop that finds the session record gone ends the continuation
			if kerr := keepStopRecords(agent, in, []guardRecord{{At: time.Now().UTC(), Session: in.SessionID, Turn: snapshot.TurnID, Guard: ritualReasoning, Pass: false, Reason: reason, Recheck: recheck}}); kerr != nil {
				hookLog("stop: " + kerr.Error())
			}
		}
		return hookOut{Block: true, Reason: reason}, muts, err
	}
	targets := []store.ReviewTarget{}
	for i, d := range ds {
		if d == nil || d.Band == decide.Abstain {
			continue
		}
		target := askedItem{Spec: hookSpecs[2+i], Trace: d.TraceID}
		var watch *scopeWatch
		if target.Spec == "changes-in-scope" { // watch whether the changed files are reverted before the next request (outcome truth)
			w := newScopeWatch(d.TraceID, string(before), changed)
			watch = &w
		}
		muts = append(muts, func(s *hookSession) {
			if s.Request == req && s.TurnID == snapshot.TurnID {
				s.Reviews = append(s.Reviews, target)
				if watch != nil {
					s.Watch = append(s.Watch, *watch)
				}
			}
		})
		targets = append(targets, store.ReviewTarget{Spec: hookSpecs[2+i], TraceID: d.TraceID})
		out.System = joinLines(out.System, outs[i].System)
	}
	muts = append(muts, func(s *hookSession) {
		if s.Request == req && s.TurnID == snapshot.TurnID && workHash(s.Reasoning) == workHash(snapshot.Reasoning) {
			s.Judged = true
			if s.GroupingAsked == s.TurnID {
				s.GroupingAsked = "" // judged: from here the turn is like any other (a notice whose Stop never comes is not judged late, 0.9.3 audit 6)
			}
			if s.Completed != nil && s.Completed.TurnID == s.TurnID {
				s.Completed.Targets = targets
			}
		}
	})
	return out, muts, firstErr
}

// judgeAskedTurn judges, at the person's next request, a turn whose Stop asked only for continues_previous and whose Stop after the
// block never came. It judges only when the required work is now saved, without counting a later human fix as agent recovery.
// Notifications, slash commands and children are left alone.
func judgeAskedTurn(agent string, in *hookInput, snapshot *hookSession) {
	prompt := strings.TrimSpace(in.Prompt)
	if snapshot.TurnID == "" || snapshot.GroupingAsked != snapshot.TurnID || snapshot.Judged || in.AgentID != "" || prompt == "" || isSlashCommand(prompt) || isNotification(prompt) {
		return
	}
	// the reply is this turn's, kept by its first Stop: Previous still holds the previous turn's reply when that Stop came without one
	reply := ""
	if c := snapshot.Completed; c != nil && c.TurnID == snapshot.TurnID {
		reply = c.Summary
	}
	stop := hookInput{SessionID: in.SessionID, CWD: in.CWD, StopHookActive: true, LastAssistantMessage: reply, late: true}
	if snapshot.GuardTurn != nil { // a slash command's stretch came in between: this judges the request's own turn, not the stretch
		request := *snapshot
		request.GuardTurn = nil
		snapshot = &request
	}
	out, muts, err := hookStop(agent, &stop, snapshot)
	if err != nil {
		hookLog("grouping: " + err.Error())
	}
	if err := saveHookMutations(agent, in.SessionID, "stop", snapshot, &out, muts, nil); err != nil {
		hookLog("grouping: " + err.Error())
	}
}

// listingHeader is the first line of the working tree listing: which request's listing it is (a hash of the request). changedFiles does not read lines without a tab, so it is not compared.
func listingHeader(request string) string {
	sum := sha256.Sum256([]byte(request))
	return "#request " + hex.EncodeToString(sum[:])[:16] + "\n"
}

// stopSummary is the waiting-answers summary (inside the lock, after this turn's decisions are added): decisions in this session waiting for the person's answer that have no answer yet.
// One line only when there is a new one (do not repeat the same count every turn). If the page is not running it is started in the background; the line is always the URL.
func stopSummary(agent string, sess *hookSession) string {
	st := store.Store{Dir: agent}
	waiting := unanswered(st, append(append([]askedItem{}, sess.Asked...), sess.Reviews...))
	n := len(waiting)
	if n == 0 || !hasNew(waiting, sess.Reported) {
		return ""
	}
	sess.Reported = waiting
	page := pageRef(feedbackScope(filepath.Dir(agent))) // absolute: MCP started with --store .shikii would look up and start a second page under "." // start it in the background if not running. The person never types a start command
	return waitingNotice(n, page, hookLoc{Lang: hookLang()})
}

// waitingNotice is the waiting-answers line the person reads at the end of the turn (in the page's plain words, see web/scripts/test-glossary.mjs).
func waitingNotice(n int, page string, l hookLoc) string {
	return bi(
		fmt.Sprintf("shikii: %d of this session's decisions wait for your answer: %s", n, page),
		fmt.Sprintf("shikii: このセッションで、あなたの答えを待っている物が %d 件あります: %s", n, page), l)
}
