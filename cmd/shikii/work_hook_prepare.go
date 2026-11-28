package main

import (
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
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
	// the notice as it stands in the context now, with what addEndingHints put in it (hook_ending.go); the same hints go in the new one
	prior := turnNotice(in.SessionID, in.workTurn, s.Planning.ReasoningOnly, in.ending)
	if !strings.Contains(out.Context, prior) {
		return
	}
	base := strings.Replace(out.Context, prior, hookPreparedNotice(in.SessionID, in.workTurn, in.ending), 1)
	// The prepared work is written in the reading order of every other reply (receipt first), and the whole context, with the lessons
	// and notices before it, is bounded by what the agent is shown: what does not fit is left out by limitWorkPreparation, which
	// names it in output with the way back to it.
	render := func(v any) hookOut {
		b, _ := orderedText(v)
		next := *out
		next.Context = joinLines(base, "shikii prepared work:\n"+string(b))
		return next
	}
	// what the agent that runs this hook shows whole, and how it counts it: Claude Code 10,000 characters, Codex 9,800 bytes (hook_bound.go)
	bound := hookBoundFor(in.agentKind)
	req := workRequest{SessionID: in.SessionID, TurnID: in.workTurn, Query: cut(strings.TrimSpace(in.Prompt), requestMax), fromHook: true, MaxBytes: bound.max}
	req.responseSize = func(v any) int { return bound.length(render(v).Context) }
	v, err := prepareWork(root, s.Scope, req)
	if err != nil && strings.Contains(err.Error(), "exceed max_bytes") {
		// The request itself does not fit what the agent is shown with the rest (about 7,000 characters of English, or any request
		// over the 64 KiB the hook keeps): the copy of it in task is cut to its head, and task_context says where the rest is. The
		// hook keeps the request, and a refused preparation saves nothing, as in the CLI's widening.
		req.taskMax, req.taskMeasure = bound.max/hookTaskHeadParts, bound.length
		v, err = prepareWork(root, s.Scope, req)
	}
	if err != nil && strings.Contains(err.Error(), "exceed max_bytes") {
		// Not even the head of the request fits: the last fields of the reply (maintenance, carried, the articles' entries) go too,
		// with output naming each. Before this, the hook fell back to the manual prepare, a second call with a reply of 28 KiB.
		// This is the last try, so that what the first two would have delivered is never traded for it.
		req.leaveOutLast = true
		v, err = prepareWork(root, s.Scope, req)
	}
	if err != nil {
		logRefusedPreparation(base, err, bound)
		return
	}
	*out = render(v)
}

// hookRefusal is the error of a hook preparation the bound refused: the message of the refusal as it was, with the reply as it stood.
type hookRefusal struct {
	err   error
	reply map[string]any
}

func (r *hookRefusal) Error() string { return r.err.Error() }
func (r *hookRefusal) Unwrap() error { return r.err }

// logRefusedPreparation writes the hook's log line for a refused preparation: the refusal's message (cut as every message is) and the
// sizes, in a line that may be hookRefusalLogExtra longer than an ordinary one.
func logRefusedPreparation(before string, err error, bound hookBound) {
	hookLogWithin(cut("work prepare: "+err.Error(), hookLogLineMax)+refusalSizes(before, err, bound), hookLogLineMax+hookRefusalLogExtra)
}

// hookRefusalLogExtra is how much longer than an ordinary line the hook's log keeps a refused preparation's line: the sizes it adds
// are field names and numbers.
const hookRefusalLogExtra = 800

// refusalSizes is what a refused preparation adds to the hook's log line: how many characters the hook had written before the
// prepared work (what the preparation had to fit beside), and each field of the reply that was left when it was refused, with its size
// as the hook writes it and the agent counts it (bound), the largest first. Only the fields' names and numbers: never the request, an article or any
// other text of the reply. A refusal that built no reply (another reason) names no fields.
func refusalSizes(before string, err error, bound hookBound) string {
	text := " | the hook's text before the prepared work: " + strconv.Itoa(bound.length(before)) + " of " + strconv.Itoa(bound.max) + " " + bound.unit
	var refused *hookRefusal
	if !errors.As(err, &refused) {
		return text + " | no reply was built"
	}
	type field struct {
		name string
		size int
	}
	fields := []field{}
	for name, value := range refused.reply {
		if value == nil {
			continue
		}
		b, _ := orderedText(value)
		if string(b) == "null" {
			continue
		}
		fields = append(fields, field{name, bound.length(string(b))})
	}
	sort.Slice(fields, func(i, j int) bool {
		if fields[i].size != fields[j].size {
			return fields[i].size > fields[j].size
		}
		return fields[i].name < fields[j].name
	})
	text += " | reply fields left, in " + bound.unit + ":"
	for _, f := range fields {
		text += " " + f.name + "=" + strconv.Itoa(f.size)
	}
	return text
}

// hookPreparedNotice is the notice that stands in the context in place of the planning or reasoning notice once the hook has prepared
// the work, with what the hook knows of the ending record (hook_ending.go) put in the sentence about it.
func hookPreparedNotice(sessionID, turnID string, h endingHints) string {
	return fmt.Sprintf("shikii: this task is prepared below. Read its context and evidence before working. Recover omitted originals; add topic words in query with work prepare using its receipt when needed. Do not prepare again from scratch or run hook plan query. End by sending the IDs below and result on standard input to %s work record (or as work to the MCP tool shikii_wiki, operation record).%s If preparation is replaced, use its new receipt.\n", noticeCommand(), h.text()) + planJSON(sessionID, turnID)
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

// hookTaskHeadMax is the most of a request the hook delivers in task when the whole request does not fit, in characters as
// delivered (Claude Code's count; for another agent a fifth of its bound, in its own count): the agent has the whole request in its
// conversation, so the rest of the bound goes to the lessons.
const (
	hookTaskHeadParts = 5
	hookTaskHeadMax   = hookContextMax / hookTaskHeadParts
)
