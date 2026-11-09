package main

import (
	"strconv"

	"github.com/hayaenfj/shikii/internal/notebook"
)

func (b *workRecordBatch) prepareProcedureReviews() {
	req, w, book, result, save, add := b.req, b.w, b.book, b.result, b.save, b.add
	if len(req.Outcomes)+len(req.Notes) > 0 {
		ids := []string{}
		for _, o := range req.Outcomes {
			if p, ok := w.Articles[o.Ref]; ok {
				ids = append(ids, p.ID)
			}
		}
		for _, item := range result.Saved {
			if item.Kind == "note" && item.ID != "" {
				ids = append(ids, item.ID)
			}
		}
		// Empty IDs would ask for every procedure, including unrelated ones.
		if len(ids) == 0 {
			return
		}
		tasks, e := book.OutcomeMaintenance(ids)
		if e != nil {
			add(workItem{Ref: "procedure_reviews", Kind: "maintenance", Error: e.Error()})
		} else {
			for _, task := range tasks {
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
				result.ProcedureReviews = append(result.ProcedureReviews, workProcedureReview{MaintenanceTask: task, Ref: ref})
			}
			if len(tasks) > 0 {
				if e = save(); e != nil {
					add(workItem{Ref: "procedure_reviews", Kind: "maintenance", Error: e.Error()})
				}
			}
		}
	}

}
