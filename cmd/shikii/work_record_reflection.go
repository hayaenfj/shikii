package main

import (
	"strings"

	"github.com/hayaenfj/shikii/internal/notebook"
)

func (b *workRecordBatch) recordReflection(noteSources []string) {
	root, scope, req, w, book := b.root, b.scope, b.req, b.w, b.book
	resolveSources, save, commit, add, result := b.resolve, b.save, b.commit, b.add, b.result
	reflection := reflectRequest{Operation: "submit"}
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
	for _, m := range req.Memory {
		binding, ok := w.Memory[m.Ref]
		if !ok {
			add(workItem{Ref: m.Ref, Kind: "memory", Error: unknownPreparedRef("memory", m.Ref, sortedRefs(w.Memory), "pending[].memories[]")})
			continue
		}
		reflection.Memory = append(reflection.Memory, memoryAssessmentRequest{SourceID: binding.Source, Key: binding.Key, Result: m.Result, Decision: m.Decision, Evidence: m.Evidence, CorrectionResult: m.CorrectionResult})
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
