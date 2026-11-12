package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
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
}

func sessionLatestPath(common, sessionID string) string {
	return filepath.Join(common, "latest", filepath.Base(sessionPath("", sessionID)))
}

// readSessionLatest reads the state of the session's turn. known is false for a session from before it was written.
func readSessionLatest(sessionID string) (latest sessionLatest, known bool, err error) {
	common, err := agentHome()
	if err != nil {
		return latest, false, err
	}
	b, err := os.ReadFile(sessionLatestPath(common, sessionID))
	if errors.Is(err, os.ErrNotExist) {
		return latest, false, nil
	}
	if err == nil {
		err = json.Unmarshal(b, &latest)
	}
	return latest, err == nil, err
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
		if info, err := os.Lstat(p); err == nil && info.Mode().IsRegular() && time.Since(info.ModTime()) > hookSessionTTL {
			_ = os.Remove(p)
		}
	}
}

// noteSessionRequest notes the person's request: the session's turn is on it now (root "" outside every project).
func noteSessionRequest(sessionID, root, turn string) {
	updateSessionLatest(sessionID, func(latest *sessionLatest, _ bool) bool {
		*latest = sessionLatest{Root: root, Turn: turn}
		return true
	})
}

// sessionTurnEvent changes the state of the session's turn by an event, wherever the agent's shell stands: a Stop that lets the
// turn end ends the request it saw (turn, read when the Stop came; a request that came since is left as it is), and a completion
// notice reopens the session's last request (the agent works on). A Stop a check stopped, and an event of a subagent, change
// nothing.
func sessionTurnEvent(event string, in *hookInput, turn string, blocked bool) {
	if in.AgentID != "" || in.SessionID == "" {
		return
	}
	switch {
	case event == "stop" && !blocked:
		updateSessionLatest(in.SessionID, func(latest *sessionLatest, known bool) bool {
			if !known || latest.Turn != turn || latest.Ended {
				return false
			}
			latest.Ended = true
			return true
		})
	case event == "user-prompt-submit" && isNotification(in.Prompt):
		updateSessionLatest(in.SessionID, func(latest *sessionLatest, known bool) bool {
			if !known || !latest.Ended {
				return false
			}
			latest.Ended = false
			return true
		})
	}
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
