package main

import (
	"sort"

	"github.com/hayaenfj/shikii/internal/notebook"
)

// Only an existing review can be refreshed by an explicit current full read.
// This does not assign another upgrade article or complete a semantic review.
func prepareMaintenanceRefresh(book notebook.Book, w *workState, entry map[string]any) error {
	id, _ := entry["id"].(string)
	refs := []string{}
	for ref, old := range w.Maintenance {
		if old.PageID == id {
			refs = append(refs, ref)
		}
	}
	if len(refs) == 0 {
		return nil
	}
	keys, err := book.MaintenanceKeys()
	if err != nil {
		return err
	}
	task, ok := keys[id]
	if !ok || task.Revision != entry["revision"] {
		return nil
	}
	sort.Strings(refs)
	reviews := []workProcedureReview{}
	for _, ref := range refs {
		old := w.Maintenance[ref]
		if old.Revision != task.Revision || old.Key != task.Key {
			reviews = append(reviews, workProcedureReview{MaintenanceTask: task, Ref: ref})
		}
	}
	if len(reviews) > 0 {
		entry["maintenance_reviews"] = reviews
	}
	return nil
}

// Apply only after response sizing. An omitted body cannot supply fresh review
// authority, even when its ID and current revision remain visible.
func retainMaintenanceRefresh(w *workState, entries []any) {
	for _, raw := range entries {
		entry := raw.(map[string]any)
		if entry["body_included"] != true {
			delete(entry, "maintenance_reviews")
			continue
		}
		reviews, _ := entry["maintenance_reviews"].([]workProcedureReview)
		for _, review := range reviews {
			if old, ok := w.Maintenance[review.Ref]; ok && old.PageID == review.PageID {
				w.Maintenance[review.Ref] = notebook.MaintenanceReview{PageID: review.PageID, Revision: review.Revision, Key: review.Key}
			}
		}
	}
}
