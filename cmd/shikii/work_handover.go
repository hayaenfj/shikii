package main

import (
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

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

// interruptedHandover is what the request a new message replaces hands over: nothing when the agent finished it (a Stop let the
// turn end; a Stop a check stopped does not, the agent works on) or it belongs to another project. A request that was itself interrupted before it prepared passes on what it was handed.
func interruptedHandover(s *hookSession, scope string) *workHandover {
	if s.Scope != scope || s.Planning == nil || s.TurnID == "" || s.EndedTurn == s.TurnID {
		return nil
	}
	if s.Planning.Work != nil {
		return handoverOf(s.Planning.Work)
	}
	return s.Planning.Handover
}

// turnElsewhere records what an event says about the agent's turn in the other projects that hold a record of this session. The
// hook runs where the agent's shell stands: outside every project, or in another project than the one whose request the agent
// works on (it changed folder). A Stop that stopped nothing ends the turn there too: without the record, the next request in that
// project would take over the references of a request that was finished. A completion notice means the agent works on: the end is
// taken back, as hookUserPrompt does in the project itself. here is the project the event itself is handled in ("" outside every
// project). Only a record that exists and has a turn is touched; a Stop with the id of a subagent says nothing of the request.
func turnElsewhere(event string, in *hookInput, here string, blocked bool) {
	var ends bool
	switch {
	case in.AgentID != "" || in.SessionID == "":
		return
	case event == "stop" && !blocked:
		ends = true
	case event == "user-prompt-submit" && isNotification(in.Prompt):
		ends = false
	default:
		return
	}
	records, readable := sessionTurns(in.SessionID)
	if !ends {
		// the background work belongs to the request the session worked on last: only that one goes on. A notice that came in B
		// reopened a request of A that had ended before B's started (audit of 0.31.0). With a record of the session that cannot be
		// read, which request came last is not known: none is reopened.
		latest := latestTurn(records, here)
		if !readable || latest < 0 {
			return
		}
		records = records[latest : latest+1]
	}
	for _, r := range records {
		if r.root == here || ends == (r.s.EndedTurn == r.s.TurnID) {
			continue
		}
		if err := updateSession(r.root, in.SessionID, func(s *hookSession) {
			switch {
			case s.TurnID == "":
			case ends:
				s.EndedTurn = s.TurnID
			case s.EndedTurn == s.TurnID:
				s.EndedTurn = ""
			}
		}); err != nil {
			hookLog(fmt.Sprintf("session %s: the turn's state was not recorded in %s (%v)", in.SessionID, r.path, err))
		}
	}
}

// sessionTurn is the record of one project that holds a turn of the session.
type sessionTurn struct {
	root, path string
	s          *hookSession
}

// sessionTurns reads the records of the session that hold a turn, in every registered project (the hook registers the project it
// runs in before it handles the event). A record that is not there reads as one without a turn. readable is false when a record that
// is there cannot be read.
func sessionTurns(sessionID string) (turns []sessionTurn, readable bool) {
	common, err := agentHome()
	if err != nil {
		return nil, true
	}
	readable = true
	for _, p := range registeredProjects(common) {
		root := filepath.Join(p.Path, ".shikii")
		s, err := readSession(root, sessionID)
		if err != nil {
			hookLog(fmt.Sprintf("session %s: unreadable in %s (%v)", sessionID, p.Path, err))
			readable = false
			continue
		}
		if s.TurnID != "" {
			turns = append(turns, sessionTurn{root, p.Path, s})
		}
	}
	return turns, readable
}

// latestTurn is the index of the turn whose request came last (-1 for none): the time the person's message came (HeadAt). The time of
// the record (At) moves with every write, so a Stop or a delivery in an older request's project made it the newer (audit of 0.31.0).
// The same time goes to here.
func latestTurn(turns []sessionTurn, here string) int {
	latest := -1
	for i, t := range turns {
		if latest < 0 || t.s.HeadAt.After(turns[latest].s.HeadAt) || (t.s.HeadAt.Equal(turns[latest].s.HeadAt) && t.root == here) {
			latest = i
		}
	}
	return latest
}

// noticeGoesOnHere reports whether a completion notice that came in here belongs to here's own request: the one the session worked
// on last. A notice that came in A after the request in B reopened A's request, ended before B's (audit of 0.31.0).
func noticeGoesOnHere(sessionID, here string) bool {
	turns, readable := sessionTurns(sessionID)
	latest := latestTurn(turns, here)
	return readable && latest >= 0 && turns[latest].root == here
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
