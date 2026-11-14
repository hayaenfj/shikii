package main

import (
	"github.com/hayaenfj/shikii/internal/notebook"
)

func finishWorkRecordReply(book notebook.Book, w *workState, req workRequest, result *workResult, stampReview func() error) {
	if e := stampReview(); e != nil {
		result.Complete = false
		result.Errors = append(result.Errors, workItem{Kind: "context", Ref: "result-review", Error: e.Error()})
	}
	result.Reasoning = currentReasoningView(book, w.Reasoning)
	if req.Result != nil && req.Result.Ref != "" {
		pending := w.Reasoning.PendingResults[req.Result.Ref]
		if pending != nil {
			status := "saved-pending"
			if pending.Complete && result.Complete {
				status = "saved"
			}
			result.Storage = map[string]any{"status": status, "result_source": req.Result.Ref, "binding_source": pending.Source}
			if status != "saved" {
				result.NextAction = "Retry result with only the pending ref and actual evidence refs; do not repeat its original body."
			}
		}
		if pending == nil && w.Result != nil && w.Result.Ready && w.Result.Source == req.Result.Ref {
			workStorageState(w, result)
		}
	} else {
		workStorageState(w, result)
	}
	if result.ReviewComplete != nil && !*result.ReviewComplete {
		next := "The batch save and article review are separate. Read each procedure_reviews[].article, check its sources and conditions, then record maintenance:[{ref,reason}] with this receipt. Further pending articles follow in batches of two; an edit alone does not complete review."
		if len(result.ProcedureReviews) == 0 {
			next = "Article review remains incomplete. Resolve the reported maintenance error, then retry with this receipt."
		}
		if result.NextAction != "" {
			result.NextAction += " "
		}
		result.NextAction += next
	}
}
