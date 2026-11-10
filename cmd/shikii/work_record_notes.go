package main

import (
	"strconv"

	"github.com/hayaenfj/shikii/internal/notebook"
)

func (b *workRecordBatch) recordNotes(noteRefs []string) ([]string, bool) {
	w, req, book := b.w, b.req, b.book
	resolveSources, save, commit, add, halt, result := b.resolve, b.save, b.commit, b.add, b.halt, b.result
	noteSources := []string{}
	for i, n := range req.Notes {
		// Notes that differ only in their sources share one obligation (the signature leaves sources out). When an earlier note of
		// this batch was published and retired it, this note reserves its own again, so a draft left waiting stays an obligation.
		if w.Reasoning != nil && noteRefs[i] != "" && w.Reasoning.RequiredNotes[noteRefs[i]] == nil {
			ref, normalized, e := reserveRequiredNote(book, w, n, save, commit)
			if e != nil {
				add(workItem{Ref: ref, Kind: "note", Error: e.Error()})
				if halt() {
					return noteSources, true
				}
				continue
			}
			noteRefs[i], n = ref, normalized
		}
		w.ActiveNote = noteRefs[i]
		sources := append([]string{}, w.Pending[n.Ref].Sources...)
		resolveNote := func(refs []string) ([]string, error) {
			ids, e := resolveSources(refs)
			if e == nil {
				sources = append(sources, ids...)
				if w.Reasoning != nil {
					if entry := w.Reasoning.RequiredNotes[noteRefs[i]]; entry != nil {
						if entry.Sources == nil {
							entry.Sources = map[string]string{}
						}
						for j, ref := range refs {
							entry.Sources[ref] = ids[j]
						}
					}
				}
			}
			return ids, e
		}
		item, pending, e := recordWorkNote(book, w, n, resolveNote, save, commit)
		w.ActiveNote = ""
		if e == nil && pending == nil && item.ID != "" && w.Reasoning != nil {
			entry := w.Reasoning.RequiredNotes[noteRefs[i]]
			if entry != nil {
				if err := commit(func() error { _, err := book.Ingest(notebook.Draft{Sources: []string{entry.Source}}); return err }); err != nil {
					e = err
				} else {
					finishRequiredPublication(w, entry)
					delete(w.Reasoning.RequiredNotes, noteRefs[i])
				}
			}
		}
		if e == nil && pending == nil && item.ID != "" {
			noteSources = append(noteSources, sources...)
		}
		if item.Ref == "" {
			item.Ref = "note" + strconv.Itoa(i+1)
		}
		if e != nil {
			item.Error = e.Error()
			if noteRefs[i] != "" {
				item.Ref = noteRefs[i]
			}
		}
		if pending != nil {
			result.Pending = append(result.Pending, pending)
			result.Complete = false
		} else {
			add(item)
		}
		if halt() {
			return noteSources, true
		}
	}
	return noteSources, false
}
