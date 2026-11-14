package main

import (
	"slices"
)

func (b *workRecordBatch) recordResult(qualityEvidence workQualityEvidence, bindingScope string, local map[string]string) (reportSource string, stopped bool) {
	req, w, book := b.req, b.w, b.book
	resolveSources, save, commit, add, halt := b.resolve, b.save, b.commit, b.add, b.halt
	if req.Result != nil {
		var item workItem
		var e error
		if req.Result.Ref != "" {
			item, e = repairWorkResult(book, w, *req.Result, qualityEvidence, resolveSources, save, commit)
		} else {
			item, e = captureWorkCompletion(book, w, *req.Result, resolveSources, save, commit)
		}
		if e != nil {
			item.Error = e.Error()
		}
		if w.Result != nil && req.Result.Ref == "" && item.ID == w.Result.Source && w.Result.Signature == workHash(*req.Result) {
			// The submitted report covers its accepted batch even while its
			// original evidence binding is still unresolved.
			reportSource = w.Result.Source
		} else if e == nil && w.Result != nil && w.Result.Ready && bindingScope == w.Result.Source {
			// A binding repair covers only its requested missing proof, never
			// earlier unrelated evidence or other new work in this request.
			repair := req
			repair.Observations = nil
			repair.Reading = nil
			covered := onlyWorkRecovery(repair)
			for _, id := range local {
				used := slices.Contains(w.Result.Evidence, "source:"+id)
				if req.Reading != nil && req.Reading.Ref == "" {
					for _, ref := range req.Reading.Evidence {
						used = used || local[ref] == id || w.Sources[ref] == id
					}
				}
				covered = covered && used
			}
			if covered {
				reportSource = w.Result.Source
			}
		}
		add(item)
		if halt() {
			return reportSource, true
		}
	}
	return reportSource, false
}

// The original report is captured first; its evidence must cover the completed
// batch too. bindWorkEvidence follows only revisions this receipt itself wrote.
func (b *workRecordBatch) refreshResultEvidence() bool {
	if b.req.Result == nil || b.req.Result.Ref != "" || len(b.req.Notes) == 0 || !b.result.Complete || b.w.Result == nil || !b.w.Result.Ready {
		return false
	}
	err := selfEvidence(b.book, b.w, forResult, b.w.Result.Source, b.req.Result.Evidence)
	if err == nil {
		var bound []string
		bound, err = bindWorkEvidence(b.book, b.w, b.req.Result.Evidence, b.resolve)
		if err == nil && !slices.Equal(bound, b.w.Result.Evidence) {
			b.w.Result.Evidence = bound
			err = b.save()
		}
	}
	if err != nil {
		b.add(workItem{Kind: "result", Ref: "result", Error: err.Error()})
	}
	return b.halt()
}
