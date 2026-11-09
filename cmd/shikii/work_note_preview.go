package main

import (
	"github.com/hayaenfj/shikii/internal/notebook"
	"github.com/hayaenfj/shikii/internal/store"
)

func checkpointWorkNotePreview(w *workState, n workNote, key, publicationIntent string, preview notebook.Result, save func() error) error {
	w.Expected[key] = store.KnowledgeReference{ID: preview.Page.ID, Revision: preview.Page.Revision}
	if origin := w.PendingOrigins[n.Ref]; origin != "" {
		w.Expected[origin] = w.Expected[key]
	}
	bindRequiredPublication(w, w.Expected[key], publicationIntent, key, w.PendingOrigins[n.Ref])
	if err := save(); err != nil {
		return err
	}
	if err := workAfterWrite("note-preview"); err != nil {
		return err
	}
	return nil
}
