package main

import (
	"fmt"
	"os"
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

// endTurnElsewhere records the end of the turn for a Stop that ran while the agent's shell stood outside every project. Such a
// Stop checks nothing and says nothing, so the turn does end; without the record the next request in the project would take
// over the references of a request that was finished. Only the session's own record in a registered, active project is touched.
func endTurnElsewhere(sessionID string) {
	common, err := agentHome()
	if err != nil || sessionID == "" {
		return
	}
	for _, p := range registeredProjects(common) {
		root := filepath.Join(p.Path, ".shikii")
		if active, err := projectActive(root, p.Path); err != nil || !active {
			continue
		}
		if _, err := os.Stat(sessionPath(root, sessionID)); err != nil {
			continue
		}
		if s := loadSession(root, sessionID); s.TurnID == "" || s.EndedTurn == s.TurnID {
			continue
		}
		if err := updateSession(root, sessionID, func(s *hookSession) {
			if s.TurnID != "" {
				s.EndedTurn = s.TurnID
			}
		}); err != nil {
			hookLog(fmt.Sprintf("session %s: the end of the turn was not recorded (%v)", sessionID, err))
		}
	}
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
