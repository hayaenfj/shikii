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
	var own []turnLesson // the lessons delivered during this request, read once when a memory names one by its key
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
			if own, err = b.turnLessons(); err != nil {
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
		if !found {
			add(workItem{Ref: m.Ref, Kind: "memory", Error: notThisRequestsLesson(m.Ref, own, sortedRefs(w.Memory))})
		}
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
	b.lessonsLeft()
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
// the request (the delivery carries the turn's id) and right before a command or a file (its parent is the turn). A lesson
// delivered to a subagent is that subagent's to assess, and a preparation made without a hook has no request of a session.
func (b *workRecordBatch) turnLessons() ([]turnLesson, error) {
	if b.w.Standalone || b.token.Turn == "" {
		return []turnLesson{}, nil
	}
	report, err := (store.Store{Dir: b.root}).MemoryReport(b.scope)
	if err != nil {
		return nil, err
	}
	out := []turnLesson{}
	for i := len(report.All) - 1; i >= 0; i-- { // the report lists the newest delivery first
		o := report.All[i]
		if o.Agent == "" && (o.SourceID == b.token.Turn || o.Parent == b.token.Turn) {
			out = append(out, turnLesson{delivery: o.SourceID, key: o.Quote.Key, assessed: o.Assessment != nil})
		}
	}
	return out, nil
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
// just did the work can assess them in the same request: a session that sends no further request is never asked again.
func (b *workRecordBatch) lessonsLeft() {
	own, err := b.turnLessons()
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
		b.result.LessonsToAssess = &workLessonsToAssess{Refs: refs, How: lessonsToAssessHow}
	}
}
