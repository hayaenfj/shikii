package main

import (
	"errors"
	"slices"
	"strconv"

	"github.com/hayaenfj/shikii/internal/notebook"
)

func (b *workRecordBatch) prepareProcedureReviews() {
	req, w, book, result, save, add := b.req, b.w, b.book, b.result, b.save, b.add
	ids := []string{}
	for _, o := range req.Outcomes {
		if p, ok := w.Articles[o.Ref]; ok {
			ids = append(ids, p.ID)
		}
	}
	// Keep pending saved-article reviews visible on result-only retries too.
	for _, item := range w.Saved {
		if item.Kind == "note" && item.ID != "" {
			ids = append(ids, item.ID)
		}
	}
	for _, review := range req.Maintenance {
		if old, ok := w.Maintenance[review.Ref]; ok {
			ids = append(ids, old.PageID)
		}
	}
	if len(ids) == 0 {
		return
	}
	slices.Sort(ids)
	ids = slices.Compact(ids)
	complete := false
	result.ReviewComplete = &complete
	tasks, e := book.MaintenanceForPages(ids)
	if e != nil {
		add(workItem{Ref: "procedure_reviews", Kind: "maintenance", Error: e.Error()})
		return
	}
	for _, task := range tasks {
		view, e := book.Query("lesson:" + task.PageID + "@" + task.Revision)
		if e == nil && len(view.Lessons) != 1 {
			e = errors.New("the saved review article is unavailable; prepare with this receipt and its exact lesson revision")
		}
		if e != nil {
			add(workItem{Ref: "procedure_reviews", Kind: "maintenance", Error: e.Error()})
			continue
		}
		ref := ""
		for key, old := range w.Maintenance {
			if old.PageID == task.PageID && old.Revision == task.Revision && old.Key == task.Key {
				ref = key
				break
			}
		}
		if ref == "" {
			ref = "maintenance" + strconv.Itoa(len(w.Maintenance)+1)
			w.Maintenance[ref] = notebook.MaintenanceReview{PageID: task.PageID, Revision: task.Revision, Key: task.Key}
		}
		result.ProcedureReviews = append(result.ProcedureReviews, workProcedureReview{MaintenanceTask: task, Ref: ref, Article: &view.Lessons[0]})
	}
	if len(result.ProcedureReviews) > 0 {
		if e = save(); e != nil {
			add(workItem{Ref: "procedure_reviews", Kind: "maintenance", Error: e.Error()})
		}
	}
	complete = len(tasks) == 0
}
