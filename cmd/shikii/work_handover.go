package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/hayaenfj/shikii/internal/store"
)

// workHandover is what the work of a request hands to the preparation of the next one when that work did not end: the person sent
// a message while the agent was still working, the hook started a new request, and the receipt of the interrupted one stopped
// working. Only references go over, never obligations: the pages the work read (under the same names), the sources it saved
// (observedN) and the pages its notes wrote. The new request keeps its own result, plan and reviews. Before, each such message
// cost the agent its references: it prepared again and read the same pages again (three times in one piece of work, 2026-11-12).
type workHandover struct {
	Articles map[string]store.KnowledgeReference `json:"articles,omitempty"`
	Sources  map[string]string                   `json:"sources,omitempty"`  // observedN and the id of the saved source
	Observed int                                 `json:"observed,omitempty"` // the highest N of the observedN the work gave out
	Wrote    map[string]workItem                 `json:"wrote,omitempty"`    // the notes the work saved, under their keys
}

// A work holds at most 64 page revisions. More than half of that is not taken over, so the new request has room for its own.
const handoverArticleLimit = 32

// handoverOf collects what a work would hand over; nil when it holds nothing of the kind.
func handoverOf(w *workState) *workHandover {
	h := &workHandover{Sources: map[string]string{}, Wrote: map[string]workItem{}, Observed: w.ObservedBase}
	if len(w.Articles) <= handoverArticleLimit {
		h.Articles = w.Articles
	}
	observedNumber := func(ref string) int {
		n, err := strconv.Atoi(strings.TrimPrefix(ref, "observed"))
		if err != nil || !strings.HasPrefix(ref, "observed") {
			return 0
		}
		return n
	}
	for ref, id := range w.Sources {
		if observedNumber(ref) > 0 && id != "" && !w.Reports[id] { // what the agent observed, not what the work reported about itself
			h.Sources[ref] = id
		}
	}
	for _, capture := range w.Captures { // also the ones that were not saved: their names are not given out again
		h.Observed = max(h.Observed, observedNumber(capture.Ref))
	}
	for key, item := range w.Saved {
		if item.Kind == "note" && item.Error == "" && item.ID != "" && item.Revision != "" {
			h.Wrote[key] = item
		}
	}
	if len(h.Articles) == 0 && len(h.Sources) == 0 && len(h.Wrote) == 0 && h.Observed == 0 {
		return nil
	}
	return h
}

// interruptedHandover is what the request a new message replaces hands over: something only when it is the request the session got
// last (latest, read before the new request was noted), it is in this project, and the agent had not finished it (a Stop let the
// turn end; a Stop a check stopped does not, the agent works on). A request that was itself interrupted before it prepared passes on
// what it was handed.
func interruptedHandover(s *hookSession, scope, root string, latest sessionLatest) *workHandover {
	if s.Scope != scope || s.Planning == nil || s.TurnID == "" || latest.Root != root || latest.Turn != s.TurnID || latest.Ended {
		return nil
	}
	if s.Planning.Work != nil {
		return handoverOf(s.Planning.Work)
	}
	return s.Planning.Handover
}

// sessionLatest is the one state of the session's turn: the request it got last, wherever it came (the .shikii of its project, ""
// outside every project), and whether the agent's turn on it ended. The hook writes it at the person's request, at the Stop that
// lets the turn end, and at a completion notice (the agent works on): wherever the agent's shell stands, the same state changes.
//
// Before it, each project's record kept whether its request had ended, and the hook wrote it to the other projects of the session
// and chose among them by their times: every audit of 0.31.0 found another place where the agent's shell stood (another project,
// outside every project, a folder that was gone, a registry entry that could not be read) and an older request was reopened. A
// session from before it has no state: nothing is handed over and nothing reopened.
type sessionLatest struct {
	Root  string `json:"root"`
	Turn  string `json:"turn"`
	Ended bool   `json:"ended,omitempty"`
	// InTurn says whether the agent is in the middle of a turn: any message of the person that reached the hook (a request, a completion
	// notice, a slash command that is not a request) opens it, and a Stop that lets the turn end closes it. shikii forget goes by it
	// (forget_idle.go), not by Ended, which a slash command leaves as it was. It is a pointer so that a state from before the field was
	// written (nil) is told from one that says the turn has ended (false): that one is unknown, and forget refuses it.
	InTurn *bool `json:"in_turn,omitempty"`
	// Subagents are the subagents that began under the session and whose end the hook has not seen: the id of each, and when the hook
	// saw it begin. A subagent that works in the background thinks inside the agent's own process, so a Stop of the session says
	// nothing of it: forget refuses the session while one is open.
	Subagents map[string]time.Time `json:"subagents,omitempty"`
	// EventAt is when the newest event of the session that is not a Stop reached the hook; ClosedAt is when the Stop that closed the
	// turn began. An event after the Stop began means the turn went on (a Stop closes nothing an event overtook).
	EventAt  time.Time `json:"event_at,omitzero"`
	ClosedAt time.Time `json:"closed_at,omitzero"`
	// Key is the id the agent gave the message that began the turn (OpenCode's message id), when it gives one: a Stop that names
	// another turn's key is not this turn's end.
	Key string `json:"key,omitempty"`
}

// turnFlag is a value for InTurn.
func turnFlag(b bool) *bool { return &b }

// sessionLatestName is the name of a state the hook writes (sessionPath makes it).
var sessionLatestName = regexp.MustCompile(`^[A-Za-z0-9_-]+\.json$`)

func sessionLatestPath(common, sessionID string) string {
	return filepath.Join(common, "latest", filepath.Base(sessionPath("", sessionID)))
}

// readSessionLatest reads the state of the session's turn. known is false for a session from before it was written.
func readSessionLatest(sessionID string) (latest sessionLatest, known bool, err error) {
	common, err := agentHome()
	if err != nil {
		return latest, false, err
	}
	path := sessionLatestPath(common, sessionID)
	// a folder that is a link is no folder of shikii's, read or written: a state read there was never written again, and a Stop
	// that let the turn end left the request open (audit O of 0.31.0)
	if info, err := os.Lstat(filepath.Dir(path)); err == nil && !info.IsDir() {
		return sessionLatest{}, false, fmt.Errorf("%s is not a folder of shikii's", filepath.Dir(path))
	}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return latest, false, nil
	}
	if err == nil {
		err = json.Unmarshal(b, &latest)
	}
	if err != nil {
		return sessionLatest{}, false, err // nothing of a state that cannot be read is used (audit M of 0.31.0)
	}
	return latest, true, nil
}

// updateSessionLatest changes the state of the session's turn under one lock (change returns false to leave it as it is), and
// removes the states of sessions that ended long ago, so the folder does not grow with every session.
func updateSessionLatest(sessionID string, change func(latest *sessionLatest, known bool) bool) {
	common, err := agentHome()
	if err != nil || sessionID == "" {
		return
	}
	path := sessionLatestPath(common, sessionID)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		hookLog(fmt.Sprintf("session %s: the state of the turn was not written (%v)", sessionID, err))
		return
	}
	// a folder that is a link is no folder of shikii's: nothing is written or removed there (audit M of 0.31.0: the removal of old
	// states removed the person's files where the link pointed)
	if info, err := os.Lstat(filepath.Dir(path)); err != nil || !info.IsDir() {
		hookLog(fmt.Sprintf("session %s: the state of the turn was not written (%s is not a folder of shikii's)", sessionID, filepath.Dir(path)))
		return
	}
	lock, err := os.OpenFile(filepath.Join(filepath.Dir(path), ".lock"), os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		hookLog(fmt.Sprintf("session %s: the state of the turn was not written (%v)", sessionID, err))
		return
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		hookLog(fmt.Sprintf("session %s: the state of the turn was not written (%v)", sessionID, err))
		return
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	latest, known, err := readSessionLatest(sessionID)
	if err != nil {
		latest, known = sessionLatest{}, false // an unreadable state is replaced by the next request
	}
	if !change(&latest, known) {
		return
	}
	b, _ := json.Marshal(latest)
	if err := writeAtomic(path, b); err != nil {
		hookLog(fmt.Sprintf("session %s: the state of the turn was not written (%v)", sessionID, err))
		return
	}
	old, _ := filepath.Glob(filepath.Join(filepath.Dir(path), "*.json"))
	for _, p := range old {
		// only a name the hook writes (sessionPath's), a file of its own
		if !sessionLatestName.MatchString(filepath.Base(p)) {
			continue
		}
		if info, err := os.Lstat(p); err == nil && info.Mode().IsRegular() && time.Since(info.ModTime()) > hookSessionTTL {
			_ = os.Remove(p)
		}
	}
}

// noteSessionRequest notes the person's request: the session's turn is on it now (root "" outside every project).
func noteSessionRequest(sessionID, root, turn string) {
	noteSessionRequestKeyed(sessionID, root, turn, "")
}

// noteSessionRequestKeyed is noteSessionRequest for an agent that names the message that began the turn (key). The subagents that
// have not ended stay: a request does not end what runs in the background.
func noteSessionRequestKeyed(sessionID, root, turn, key string) {
	now := time.Now().UTC().Round(0)
	updateSessionLatest(sessionID, func(latest *sessionLatest, _ bool) bool {
		*latest = sessionLatest{Root: root, Turn: turn, InTurn: turnFlag(true), Subagents: latest.Subagents, EventAt: now, Key: key}
		return true
	})
}

// isPlainRequest says whether a message of the person is a request: not empty, not the notice of finished work, not a slash command.
func isPlainRequest(prompt string) bool {
	p := strings.TrimSpace(prompt)
	return p != "" && !isNotification(p) && !isSlashCommand(p)
}

// eventAt is when the hook of the event began; a test that gives no time gets now.
func (in *hookInput) eventAt() time.Time {
	if in.at.IsZero() {
		return time.Now().UTC().Round(0)
	}
	return in.at
}

// sessionActivity notes an event of the session that is not a Stop: the agent works, so the turn is open again if a Stop had closed it,
// the time of the event is kept, and a subagent that begins or ends is kept or dropped. A Stop closes the turn it began at and no event
// came after (sessionTurnEvent), so the events say that a turn goes on whatever Stop was first: another Stop hook that asked for more,
// the report of a subagent that woke the agent, a tool call. The events of a subagent are the session's own work too. A session with no
// state is given none.
func sessionActivity(event string, in *hookInput) {
	if in.SessionID == "" || event == "stop" {
		return
	}
	at := in.eventAt()
	updateSessionLatest(in.SessionID, func(latest *sessionLatest, known bool) bool {
		if !known {
			return false
		}
		changed := false
		if latest.InTurn == nil || !*latest.InTurn {
			latest.InTurn, changed = turnFlag(true), true
		}
		if at.After(latest.EventAt) {
			latest.EventAt, changed = at, true
		}
		// The message the turn is of, said before the request hook's slow work: a hook the agent stops for its time never writes the
		// request, and the Stop of this message would be taken for the Stop of the request before (it names another message).
		if event == "user-prompt-submit" && in.AgentID == "" && in.TurnKey != "" && latest.Key != in.TurnKey {
			latest.Key, changed = in.TurnKey, true
		}
		_, open := latest.Subagents[in.AgentID]
		switch {
		case in.AgentID == "":
		case event == "subagent-stop":
			if open {
				delete(latest.Subagents, in.AgentID)
				changed = true
			}
		case !open:
			// A subagent begins at its SubagentStart, and at any other event that carries its id: one the agent continued (Claude Code's
			// SendMessage) is not known to have a SubagentStart, and its own tool calls are then the only thing that says it works.
			// Only its end closes it, so a late event of one that ended opens it again, and forget asks again, which is the safe side.
			if latest.Subagents == nil {
				latest.Subagents = map[string]time.Time{}
			}
			latest.Subagents[in.AgentID], changed = at, true
		}
		return changed
	})
}

// sessionTurnEvent changes the state of the session's turn by an event, wherever the agent's shell stands. An event that is not a Stop
// opens the turn (sessionActivity), except the message of the person that is a request: the request hook notes it (noteSessionRequest).
// A Stop that lets the turn end ends the request it saw (turn, read when the Stop came; a request that came since is left as it is),
// unless an event came after the Stop began or the Stop names the message of another turn (in.TurnKey). A completion notice reopens the
// session's last request (the agent works on). A slash command, or a message with no text, is no request and changes nothing of the
// request, but the agent works on it until its Stop: only InTurn says so. A Stop a check stopped, and the Stop of a subagent, change
// nothing.
func sessionTurnEvent(event string, in *hookInput, turn string, blocked bool) {
	if in.SessionID == "" {
		return
	}
	if event != "stop" && !(event == "user-prompt-submit" && in.AgentID == "" && isPlainRequest(in.Prompt)) {
		sessionActivity(event, in)
	}
	if in.AgentID != "" {
		return
	}
	switch {
	case event == "stop" && !blocked:
		at := in.eventAt()
		updateSessionLatest(in.SessionID, func(latest *sessionLatest, known bool) bool {
			if !known || latest.Turn != turn || latest.EventAt.After(at) || in.TurnKey != "" && in.TurnKey != latest.Key {
				return false
			}
			changed := !latest.Ended || latest.InTurn == nil || *latest.InTurn || !latest.ClosedAt.Equal(at)
			latest.Ended, latest.InTurn, latest.ClosedAt = true, turnFlag(false), at
			return changed
		})
	case event == "user-prompt-submit" && isNotification(in.Prompt):
		updateSessionLatest(in.SessionID, func(latest *sessionLatest, known bool) bool {
			if !known {
				return false
			}
			changed := latest.Ended || latest.InTurn == nil || !*latest.InTurn
			latest.Ended, latest.InTurn = false, turnFlag(true)
			return changed
		})
	}
}

// sessionTurnOnExit changes the state of the session's turn by an event the hook left before it changed it (a project it could not
// prepare, a folder that is gone): a request is noted as one outside every project (no request of a project is handed over or
// reopened), a Stop ends the turn (it stopped nothing), a notice reopens the last request.
func sessionTurnOnExit(event string, in *hookInput, stopTurn string) {
	if in.stateNoted || in.AgentID != "" || in.SessionID == "" {
		return
	}
	if event == "user-prompt-submit" && isPlainRequest(in.Prompt) {
		noteSessionRequestKeyed(in.SessionID, "", "", in.TurnKey)
		return
	}
	sessionTurnEvent(event, in, stopTurn, false)
}

// sessionTurnAt is the turn the state names now ("" for a session without one): what a Stop that comes now ends.
func sessionTurnAt(sessionID string) string {
	latest, known, err := readSessionLatest(sessionID)
	if err != nil || !known {
		return ""
	}
	return latest.Turn
}

// takeOver gives a new work the references of the interrupted one and returns what the reply of prepare says about it (carried);
// nil when there is nothing.
func takeOver(w *workState, h *workHandover) map[string]any {
	if h == nil {
		return nil
	}
	for ref, article := range h.Articles {
		w.Articles[ref] = article
	}
	for ref, id := range h.Sources {
		w.Sources[ref] = id
	}
	for key, item := range h.Wrote {
		w.Saved[key] = item
	}
	w.ObservedBase = h.Observed
	articles, sources := sortedRefs(h.Articles), sortedRefs(h.Sources)
	sortRefs(articles)
	sortRefs(sources)
	if len(articles) == 0 && len(sources) == 0 {
		return nil
	}
	return map[string]any{"articles": articles, "sources": sources,
		"next_action": "The person's message came while the earlier request was still being worked on. These references of that work are valid with this receipt: cite the pages and the saved sources without reading them again, and edit the pages that work did not edit. A page that work edited is read again before another edit (prepare with this receipt and query lesson:<id>). Its result, plan and reviews were not taken over: record with this receipt."}
}

// preparedArticleRef names a page revision in a preparation: the reference the work already has for it (taken over from an
// interrupted request), or the next number.
func preparedArticleRef(w *workState, id, revision string) string {
	refs := sortedRefs(w.Articles)
	sort.Strings(refs)
	for _, ref := range refs {
		if old := w.Articles[ref]; old.ID == id && old.Revision == revision && strings.HasPrefix(ref, "knowledge") {
			return ref
		}
	}
	return "knowledge" + strconv.Itoa(len(w.Articles)+1)
}
