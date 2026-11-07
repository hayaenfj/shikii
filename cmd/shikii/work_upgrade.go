package main

import (
	"errors"
	"fmt"

	"github.com/hayaenfj/shikii/internal/notebook"
	"github.com/hayaenfj/shikii/internal/store"
)

const memoryUpgradeNext = "Review each supplied page against its original sources and current behavior. Save necessary corrections through ordinary notes/patches; then record maintenance using the latest returned ref. Retain valid knowledge with a checked reason. Never turn internal agent grades into required human questions. Fetch the next batch with prepare:{receipt,query:memory-upgrade}; continue until complete, or report the actual blocked sources. At the receipt's article limit, prepare a new context; durable progress is retained. Reading alone does not complete review."

func prepareMemoryUpgrade(book notebook.Book, w *workState, quoted map[string]string, includeComplete bool) (map[string]any, []store.KnowledgeReference, error) {
	if projectInitVersion(book.Root) == 0 {
		return nil, nil, nil
	}
	u, err := book.MemoryUpgrade()
	if err != nil {
		return nil, nil, err
	}
	if u.Status == "complete" && !includeComplete {
		return nil, nil, nil
	}
	if !includeComplete {
		heads := []any{}
		for _, task := range u.Tasks {
			heads = append(heads, map[string]any{"id": task.PageID, "revision": task.Revision})
		}
		return map[string]any{"epoch": u.Epoch, "status": u.Status, "message": u.Message, "total": u.Total, "reviewed": u.Reviewed, "pending_pages": heads, "unavailable_sources": u.Blocked, "next_action": "At a break in the task, prepare this receipt with query memory-upgrade to review existing knowledge in bounded batches. " + memoryUpgradeNext}, nil, nil
	}
	pages := []any{}
	read := []store.KnowledgeReference{}
	for _, task := range u.Tasks {
		v, err := book.Query("lesson:" + task.PageID + "@" + task.Revision)
		if err != nil || len(v.Lessons) != 1 {
			return nil, nil, errors.New("the memory upgrade page changed; prepare its current batch again")
		}
		p := v.Lessons[0]
		ref, err := workArticleRef(w, p)
		if err != nil {
			return nil, nil, err
		}
		quoted[ref] = planningArticleSource(p)
		read = append(read, store.KnowledgeReference{ID: p.ID, Revision: p.Revision})
		mref := "upgrade-" + task.PageID
		w.Maintenance[mref] = notebook.MaintenanceReview{PageID: p.ID, Revision: p.Revision, Key: task.Key}
		pages = append(pages, map[string]any{"ref": ref, "article": p, "orientation": notebook.Orient(p, notebook.MemoryUpgradePolicy, 1200), "task": task, "maintenance_ref": mref, "body_included": true})
	}
	value := map[string]any{"epoch": u.Epoch, "status": u.Status, "message": u.Message, "total": u.Total, "reviewed": u.Reviewed, "pages": pages, "unavailable_sources": u.Blocked, "policy": u.Policy}
	if u.Status != "complete" {
		value["next_action"] = memoryUpgradeNext
	}
	if w.Reasoning.Reading == nil {
		w.Reasoning.Reading = &workReadingTrace{}
	}
	// Maintenance is a different reading task and cannot teach a task's depth.
	w.Reasoning.Reading.Mixed = true
	w.Reasoning.Reading.Bytes += workContextSize(value)
	return value, read, nil
}

func memoryUpgradeHook(root, scope string, s *hookSession) (hookOut, string, error) {
	u, err := (notebook.Book{Root: root, Scope: scope}).MemoryUpgrade()
	if err != nil {
		return hookOut{}, "", err
	}
	if u.Total == 0 {
		return hookOut{}, "", nil
	}
	key := fmt.Sprintf("%s:%s:%d/%d", u.Epoch, u.Status, u.Reviewed, u.Total)
	out := hookOut{}
	if u.Status != "complete" && s.MemoryUpgrade != key {
		out.Context = "shikii: Upgrading memory… At a break in work, prepare the current receipt with query memory-upgrade. Follow its bounded review tasks and retain original sources, preferences and history."
	}
	if s.MemoryUpgrade != key {
		out.System = fmt.Sprintf("shikii: %s (%d/%d)", u.Message, u.Reviewed, u.Total)
	}
	return out, key, nil
}
