package main

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

var hookWorkPreparation = prepareHookWork

// The hook supplies the actual preparation, so the agent can read it and call
// record once. A failed preparation retains the ordinary recovery instructions.
func prepareHookWork(root string, in *hookInput, out *hookOut) {
	s, err := readSession(root, in.SessionID)
	if err != nil || currentPlanningFrame(s, in.workTurn, s.Scope) != nil || s.Planning.Work != nil {
		return
	}
	prior := planningNotice(in.SessionID, in.workTurn)
	if s.Planning.ReasoningOnly {
		prior = reasoningNotice(in.SessionID, in.workTurn)
	}
	if !strings.Contains(out.Context, prior) {
		return
	}
	notice := fmt.Sprintf("shikii: this task is prepared below. Read its context and evidence before working. Recover omitted originals; add topic words in query with work prepare using its receipt when needed. Do not prepare again from scratch or run hook plan query. End by sending the IDs below and result on standard input to %s work record (or as work to the MCP tool shikii_wiki, operation record). If preparation is replaced, use its new receipt.\n", noticeCommand()) + planJSON(in.SessionID, in.workTurn)
	base := strings.Replace(out.Context, prior, notice, 1)
	// The prepared work is written in the reading order of every other reply (receipt first), and the whole context, with the lessons
	// and notices before it, is bounded by what the agent is shown: what does not fit is left out by limitWorkPreparation, which
	// names it in output with the way back to it.
	render := func(v any) hookOut {
		b, _ := orderedReply(v)
		next := *out
		next.Context = joinLines(base, "shikii prepared work:\n"+string(b))
		return next
	}
	req := workRequest{SessionID: in.SessionID, TurnID: in.workTurn, Query: cut(strings.TrimSpace(in.Prompt), requestMax), fromHook: true, MaxBytes: hookContextMax}
	req.responseSize = func(v any) int { return hookContextLength(render(v).Context) }
	v, err := prepareWork(root, s.Scope, req)
	if err != nil {
		hookLog("work prepare: " + err.Error())
		return
	}
	*out = render(v)
}

// IDs select only the hook's first preparation. Explicit preparation can assign
// the same local ref to different evidence, so it still needs its nonce receipt.
func hookWorkReceipt(root, scope, session, turn string) (string, error) {
	if session == "" || len(session) > 200 || len(turn) != 16 {
		return "", errInvalidReceipt
	}
	dir, err := openWorkSessions(root)
	if err != nil {
		return "", err
	}
	defer dir.Close()
	s, err := readPlanningSession(dir, filepath.Base(sessionPath(root, session)))
	if err != nil {
		return "", err
	}
	if err := currentPlanningFrame(s, turn, scope); err != nil {
		return "", err
	}
	w := s.Planning.Work
	if w == nil || !w.HookPrepared || w.Standalone {
		return "", errors.New("these IDs do not name an unchanged hook preparation; use the receipt of the latest work prepare")
	}
	return workEncode(workToken{Session: session, Turn: turn, Nonce: w.Nonce}), nil
}

// hookContextMax is the most of a hook's additionalContext Claude Code shows whole, counted as JavaScript counts a string's length,
// in UTF-16 code units. A longer one is saved to a file and only a short start of it is shown, or, when it cannot be saved, cut at
// 10,000 characters ("Hook additionalContext truncated at 10000 chars").
const hookContextMax = 10_000

// hookContextLength counts s as hookContextMax does: a character outside the Basic Multilingual Plane counts twice.
func hookContextLength(s string) int {
	n := 0
	for _, r := range s {
		n++
		if r > 0xFFFF {
			n++
		}
	}
	return n
}
