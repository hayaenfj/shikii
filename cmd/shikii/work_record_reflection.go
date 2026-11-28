package main

import (
	"fmt"
	"strings"

	"github.com/hayaenfj/shikii/internal/notebook"
	"github.com/hayaenfj/shikii/internal/store"
)

func (b *workRecordBatch) recordReflection(noteSources []string) {
	root, scope, req, w, book := b.root, b.scope, b.req, b.w, b.book
	resolveSources, save, commit, add, result := b.resolve, b.save, b.commit, b.add, b.result
	reflection := reflectRequest{Operation: "submit", viewer: workViewer(w, b.token.Session)}
	if len(noteSources) > 0 {
		reflection.Wiki = append(reflection.Wiki, notebook.Draft{Sources: uniqueWorkStrings(noteSources)})
	}
	for _, r := range req.Reviews {
		binding, ok := w.Reviews[r.Ref]
		if !ok {
			add(workItem{Ref: r.Ref, Kind: "review", Error: unknownPreparedRef("review", r.Ref, sortedRefs(w.Reviews), "pending[].reviews[]")})
			continue
		}
		reflection.Reviews = append(reflection.Reviews, reviewRequest{SourceID: binding.Source, TraceID: binding.Trace, Truth: r.Answer, Reason: r.Reason})
	}
	var own []turnLesson        // the lessons delivered during this request, read once when a memory names one by its key
	var earlier map[string]bool // the keys of lessons the session was given in earlier requests and has assessed, read once when needed
	for _, m := range req.Memory {
		if binding, ok := w.Memory[m.Ref]; ok {
			reflection.Memory = append(reflection.Memory, memoryAssessmentRequest{SourceID: binding.Source, Key: binding.Key, Result: m.Result, Decision: m.Decision, Evidence: m.Evidence, CorrectionResult: m.CorrectionResult})
			continue
		}
		if !deliveredKey(m.Ref) {
			add(workItem{Ref: m.Ref, Kind: "memory", Error: unknownPreparedRef("memory", m.Ref, sortedRefs(w.Memory), "pending[].memory[]")})
			continue
		}
		// a lesson of this request is assessed here by its key: every delivery of it in the request (the same lesson can come before
		// the request and again right before a command or a file)
		if own == nil {
			var err error
			if own, err = turnLessons(b.root, b.scope, b.w, b.token); err != nil {
				add(workItem{Ref: m.Ref, Kind: "memory", Error: err.Error()})
				continue
			}
		}
		found := false
		for _, l := range own {
			if l.key == m.Ref {
				found = true
				reflection.Memory = append(reflection.Memory, memoryAssessmentRequest{SourceID: l.delivery, Key: l.key, Result: m.Result, Decision: m.Decision, Evidence: m.Evidence, CorrectionResult: m.CorrectionResult, inRequest: true})
			}
		}
		if found {
			continue
		}
		if earlier == nil {
			var err error
			if earlier, err = earlierAssessedLessons(b.root, b.scope, b.w, b.token); err != nil {
				add(workItem{Ref: m.Ref, Kind: "memory", Error: err.Error()})
				continue
			}
		}
		if earlier[m.Ref] {
			// the record of that request already assessed it, and an assessment is made once: the entry is left out, and the record
			// stays complete (a refusal here left the result of the record without its ending review, and the agent sent another)
			add(workItem{Ref: m.Ref, Kind: "memory", Warnings: []string{alreadyAssessedLesson}})
			continue
		}
		add(workItem{Ref: m.Ref, Kind: "memory", Error: notThisRequestsLesson(m.Ref, own, sortedRefs(w.Memory))})
	}
	sourceReviews := []string{}
	for _, ref := range req.Reviewed {
		if strings.HasPrefix(ref, "change") || strings.HasPrefix(ref, "knowledge") {
			item := workItem{Ref: ref, Kind: "collective_review"}
			key := "collective-review:" + ref
			if _, ok := w.Saved[key]; !ok {
				if e := workSavedCapacity(w, key); e != nil {
					item.Error = e.Error()
				} else if e := reviewCollective(book, w, ref); e != nil {
					item.Error = e.Error()
				} else {
					w.Saved[key] = item
					if e := save(); e != nil {
						item.Error = e.Error()
					}
				}
			}
			add(item)
		} else {
			sourceReviews = append(sourceReviews, ref)
		}
	}
	if len(sourceReviews) > 0 {
		sources, e := resolveSources(sourceReviews)
		if e != nil {
			add(workItem{Ref: "reviewed", Kind: "sources", Error: e.Error()})
		} else {
			reflection.Wiki = append(reflection.Wiki, notebook.Draft{Sources: sources})
		}
	}
	for _, m := range req.Maintenance {
		review, ok := w.Maintenance[m.Ref]
		if !ok {
			add(workItem{Ref: m.Ref, Kind: "maintenance", Error: "unknown prepared maintenance reference"})
			continue
		}
		if m.Blocked {
			// A page the agent cannot check is declared blocked, not reviewed: the batch moves on past it.
			if e := book.BlockMemoryUpgrade(review.PageID, review.Revision, review.Key, m.Reason); e != nil {
				add(workItem{Ref: m.Ref, Kind: "maintenance", Error: e.Error()})
			}
			continue
		}
		review.Reason = m.Reason
		reflection.Maintenance = append(reflection.Maintenance, review)
	}
	if len(reflection.Reviews)+len(reflection.Memory)+len(reflection.Wiki)+len(reflection.Maintenance) > 0 {
		// Automatically checking a note's remainder must stay available at capacity.
		// It is a fresh check, not a separately requested durable assessment batch.
		persist := len(req.Reviews)+len(req.Memory)+len(req.Reviewed)+len(req.Maintenance) > 0
		key := "reflection:" + workHash(reflection)
		if prior, ok := w.Saved[key]; ok && persist {
			result.Reflection = w.Replies[key].Reflection
			add(prior)
		} else {
			var value any
			var e error
			if persist {
				e = workSavedCapacity(w, key)
			}
			if e == nil {
				value, e = runReflectWithCommit(root, scope, reflection, commit)
			}
			item := workItem{Ref: "reflection", Kind: "reflection"}
			if e != nil {
				item.Error = e.Error()
			} else {
				result.Reflection = value
				receipt := value.(reflectResult)
				if !receipt.Complete || receipt.LeftError != "" || len(receipt.Left) > 0 {
					item.Error = "some prepared reflection items remain; inspect reflection and resend corrected content"
				} else if persist {
					w.Saved[key] = item
					w.Replies[key] = workReply{Reflection: &receipt}
					if e = save(); e != nil {
						item.Error = e.Error()
					}
				}
			}
			add(item)
		}
	}
}

// turnLesson is one lesson delivered during a request to the session's own agent: the delivery's id and the quote's key.
type turnLesson struct {
	delivery, key string
	assessed      bool
}

// workLessonsToAssess names, in a record's reply, the lessons delivered during its request that are not assessed yet.
type workLessonsToAssess struct {
	Refs []string `json:"refs"`
	How  string   `json:"how"`
}

// lessonsToAssessMax is how many keys a reply names (a request receives two lessons at its start and one per tool call).
const lessonsToAssessMax = 16

// Measured with real agents (Codex 0.169.0, Claude Code; 2026-11-17): a reply with complete:true and lessons_to_assess ended the
// request without the assessment. Agents are told to check next_action, so the record that assesses them is the reply's next step.
const lessonsToAssessNext = "Before you answer, send another work record of this request with only its IDs and memory for each ref of lessons_to_assess."

const lessonsToAssessHow = "Lessons delivered during this request that you have not assessed. Only you saw whether they were used: send memory:[{ref,result,decision,evidence}] with ref one of these keys in a work record of this request (result helpful, unused, harmful, or unknown when you cannot tell; decision and evidence are text, not lists)."

// deliveredKey reports whether a memory reference has the form of a delivered quote's key, not of a prepared ref (memoryN).
func deliveredKey(ref string) bool {
	return strings.HasPrefix(ref, "lesson:") || strings.HasPrefix(ref, "decision:") || strings.HasPrefix(ref, "feedback:")
}

// turnLessons lists the lessons delivered during this record's request to the session's own agent, in the order delivered: before
// the request (the delivery carries the turn's id) and right before a command or a file (its parent is the turn and it names
// the tool). A lesson delivered to a subagent is that subagent's to assess: its delivery has the turn as parent too, with the
// subagent's id, or with no tool when the host sent no id at the subagent's start. A preparation made without a hook has no
// request of a session.
func turnLessons(root, scope string, w *workState, token workToken) ([]turnLesson, error) {
	if w.Standalone || token.Turn == "" {
		return []turnLesson{}, nil
	}
	report, err := (store.Store{Dir: root}).MemoryReport(scope)
	if err != nil {
		return nil, err
	}
	out := []turnLesson{}
	for i := len(report.All) - 1; i >= 0; i-- { // the report lists the newest delivery first
		o := report.All[i]
		if o.Agent == "" && (o.SourceID == token.Turn || o.Parent == token.Turn && o.Tool != "") {
			out = append(out, turnLesson{delivery: o.SourceID, key: o.Quote.Key, assessed: o.Assessment != nil})
		}
	}
	return out, nil
}

// alreadyAssessedLesson is what the reply says of a memory entry that names a lesson the session was given in an earlier request and
// has assessed: the entry is not saved, and nothing is asked of the agent.
const alreadyAssessedLesson = "not saved: this lesson was delivered to you in an earlier request of this session and is already assessed; an assessment is made once and stays as saved"

// earlierAssessedLessons is the set of keys of the lessons the session's own agent was given in the session's earlier requests, every
// delivery of which is assessed. The session of a delivery is the one the conversation of its request was recorded for (a delivery
// carries no session but a subagent's), so a request whose conversation is not recorded for this session counts for none. A key that
// has any delivery still to assess in an earlier request is not in the set: the prepared ref of that delivery is its way, and it is
// refused as before. So is a lesson another session or a subagent was given, which is that one's to assess.
func earlierAssessedLessons(root, scope string, w *workState, token workToken) (map[string]bool, error) {
	assessed := map[string]bool{}
	if w.Standalone || token.Turn == "" {
		return assessed, nil
	}
	st := store.Store{Dir: root}
	var feedback []store.Feedback
	report, err := st.MemoryReportWithFeedback(scope, func() ([]store.Feedback, error) {
		var err error
		feedback, err = st.RecentFeedback(scope)
		return feedback, err
	})
	if err != nil {
		return nil, err
	}
	requests := map[string]bool{} // the earlier requests of this session
	for _, f := range feedback {
		if f.Session == token.Session && f.ID != token.Turn {
			requests[f.ID] = true
		}
	}
	waiting := map[string]bool{}
	for _, o := range report.All {
		turn := ""
		switch {
		case o.Agent != "": // a subagent's lesson is its own to assess
			continue
		case o.Parent == "": // delivered before the request: the delivery carries the turn's id
			turn = o.SourceID
		case o.Tool != "": // delivered right before a command or a file: the turn is its parent
			turn = o.Parent
		default: // a subagent's whose id the host did not send
			continue
		}
		if !requests[turn] {
			continue
		}
		if o.Assessment == nil {
			waiting[o.Quote.Key] = true
		} else {
			assessed[o.Quote.Key] = true
		}
	}
	for key := range waiting {
		delete(assessed, key)
	}
	return assessed, nil
}

// notThisRequestsLesson refuses a key that names no lesson delivered to the session's own agent during this request, with the
// keys the record can take and, for an earlier request's lesson, the prepared refs.
func notThisRequestsLesson(ref string, own []turnLesson, prepared []string) string {
	text := fmt.Sprintf("unknown memory reference %q: not a lesson delivered to you during this request", ref)
	keys, seen := []string{}, map[string]bool{}
	for _, l := range own {
		if !seen[l.key] {
			seen[l.key] = true
			keys = append(keys, l.key)
		}
	}
	if len(keys) > 0 {
		text += "; the lessons of this request are " + listRefs(keys)
	} else {
		text += " (none was)"
	}
	if len(prepared) > 0 {
		text += "; a lesson of an earlier request is assessed by its ref of pending[].memory[] in the reply of prepare: " + listRefs(prepared)
	}
	return text + ". A lesson delivered to a subagent is assessed by that subagent"
}

// lessonsLeft puts in the reply the lessons of this request that are still not assessed after this record, so the agent that
// just did the work can assess them in the same request: a session that sends no further request is never asked again. It runs
// when the reply is finished, so a record that stopped before its reflection (a refused note, a failed save) lists them too.
func lessonsLeft(root, scope string, w *workState, token workToken, result *workResult) {
	own, err := turnLessons(root, scope, w, token)
	if err != nil {
		hookLog("work record: lessons of the request: " + err.Error())
		return
	}
	refs, seen := []string{}, map[string]bool{}
	for _, l := range own {
		if !l.assessed && !seen[l.key] && len(refs) < lessonsToAssessMax {
			seen[l.key] = true
			refs = append(refs, l.key)
		}
	}
	if len(refs) > 0 {
		result.LessonsToAssess = &workLessonsToAssess{Refs: refs, How: lessonsToAssessHow}
	}
}
