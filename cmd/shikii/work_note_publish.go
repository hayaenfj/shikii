package main

import (
	"errors"

	"github.com/hayaenfj/shikii/internal/notebook"
)

func publishWorkNote(book notebook.Book, w *workState, n workNote, d notebook.Draft, item workItem, key, publicationIntent string, save func() error, commit workCommitter) (workItem, any, error) {
	var preview notebook.Result
	var err error
	if n.Target != "" && n.Target != "new" {
		target, ok := w.Articles[n.Target]
		if !ok {
			return item, nil, errors.New("unknown prepared article target")
		}
		patch := notebook.Patch{ID: target.ID, Revision: target.Revision, Sources: d.Sources, ChangeReason: d.ChangeReason, Edits: n.Edits, Passages: d.Passages, Learning: d.Learning}
		preview, err = book.PreviewPatch(patch)
		if err != nil {
			return item, nil, err
		}
		if err = checkpointWorkNotePreview(w, n, key, publicationIntent, preview, save); err != nil {
			return item, nil, err
		}
		var value notebook.Result
		err = commit(func() error { var e error; value, e = book.Patch(patch); return e })
		if value.Page != nil {
			item.ID, item.Revision, item.Warnings = value.Page.ID, value.Page.Revision, value.Warnings
		}
	} else {
		if placedItem, pending, err := placeNewWorkNote(book, w, n, d, item, key, save, commit); err != nil || pending != nil {
			return placedItem, pending, err
		}
		d.CreateOnly = true
		preview, err = book.PreviewCreate(d)
		if err != nil {
			return item, nil, err
		}
		if err = checkpointWorkNotePreview(w, n, key, publicationIntent, preview, save); err != nil {
			return item, nil, err
		}
		var value notebook.Result
		err = commit(func() error {
			var e error
			value, e = ingestWithPreparedOutcome(book.Root, book.Scope, d, preview.Page)
			return e
		})
		if value.Page != nil {
			item.ID, item.Revision = value.Page.ID, value.Page.Revision
		}
	}
	if err != nil {
		return item, nil, err
	}
	if err = workAfterWrite("note"); err != nil {
		return item, nil, err
	}
	if err = commit(func() error { return finishWorkSources(book, d.Sources) }); err != nil {
		return item, nil, err
	}
	w.Saved[key] = item
	if origin := w.PendingOrigins[n.Ref]; origin != "" {
		w.Saved[origin] = item
	}
	return item, nil, save()
}
