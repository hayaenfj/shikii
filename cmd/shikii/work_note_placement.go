package main

import (
	"strconv"

	"github.com/hayaenfj/shikii/internal/notebook"
	"github.com/hayaenfj/shikii/internal/store"
)

func placeNewWorkNote(book notebook.Book, w *workState, n workNote, d notebook.Draft, item workItem, key string, save func() error, commit workCommitter) (workItem, any, error) {
	pendingKey := key
	alreadyReviewed := n.Ref != "" && n.Target == "new"
	if !alreadyReviewed {
		placed, e := placeDraftsWithCommit(book.Root, book.Scope, []notebook.Draft{d}, commit)
		if e != nil {
			return item, nil, e
		}
		ambiguous := false
		for _, p := range placed.Placements[0].Candidates {
			ambiguous = ambiguous || !p.SameTopic.Calibrated || p.SameTopic.Band != "discard"
		}
		if ambiguous {
			ref := w.PendingKeys[pendingKey]
			if ref == "" {
				ref, e = nextWorkPendingReference(w)
				if e != nil {
					return item, nil, e
				}
				w.PendingKeys[pendingKey] = ref
				w.Pending[ref] = d
				w.PendingOrigins[ref] = key
			}
			// The active obligation can be absent (recordNotes reserves it again when an earlier note of the batch retired a
			// shared one). Placement must never dereference a retired obligation; Pending and PendingOrigins still hold the draft.
			if required := activeRequiredNote(w); required != nil {
				required.Pending = ref
				required.PendingWork = w.Nonce
			}
			candidates := []any{}
			for i, c := range placed.Placements[0].Candidates {
				if i == 3 {
					break
				}
				alias := ""
				for known, p := range w.Articles {
					if p.ID == c.PageID && p.Revision == c.Revision {
						alias = known
						break
					}
				}
				if alias == "" {
					alias = "candidate" + strconv.Itoa(len(w.Articles)+1)
				}
				w.Articles[alias] = store.KnowledgeReference{ID: c.PageID, Revision: c.Revision}
				view, e := book.Query("lesson:" + c.PageID + "@" + c.Revision)
				if e != nil {
					return item, nil, e
				}
				candidates = append(candidates, map[string]any{"ref": alias, "article": view.Lessons, "same_topic": c.SameTopic, "conflict": c.Conflict})
			}
			if e = save(); e != nil {
				return item, nil, e
			}
			return item, map[string]any{"ref": ref, "title": d.Title, "candidates": candidates, "next_action": "Read the candidates. Record this note's ref with target new, or a candidate ref and exact edits; no metadata needs to be rebuilt."}, nil
		}
	}
	return item, nil, nil
}
