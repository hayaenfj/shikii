package main

import (
	"fmt"
	"strings"

	"github.com/hayaenfj/shikii/internal/notebook"
	"github.com/hayaenfj/shikii/internal/store"
)

func recordWorkNote(book notebook.Book, w *workState, n workNote, resolve func([]string) ([]string, error), save func() error, commit workCommitter) (workItem, any, error) {
	item := workItem{Ref: n.Ref, Kind: "note"}
	d, err := prepareWorkNoteDraft(w, n, resolve)
	if err != nil {
		return item, nil, err
	}
	signature := workHash(struct {
		Draft  notebook.Draft
		Target string
		Edits  []notebook.TextEdit
	}{d, n.Target, n.Edits})
	key := "note:" + signature
	if prior, ok := w.Saved[key]; ok {
		return prior, nil, nil
	}
	keys := []string{key}
	if origin := w.PendingOrigins[n.Ref]; origin != "" {
		keys = append(keys, origin)
	}
	if e := workSavedCapacity(w, keys...); e != nil {
		return item, nil, e
	}
	publicationIntent := requiredPublicationIntent(w, n, d)
	d.ChangeReason = strings.TrimSpace(d.ChangeReason + " [work " + w.Nonce[:8] + " " + signature[:12] + "]")
	// Recovery after a published article but a failed checkpoint uses the exact
	// expected revision, never a newly read page as permission to overwrite it.
	expected, hasExpected := w.Expected[key]
	if required := activeRequiredNote(w); required != nil && required.Expected != nil {
		publication := required.Expected
		live, err := book.LiveWhere(func(p notebook.Page) bool { return p.ID == publication.ID })
		if err != nil {
			return item, nil, err
		}
		if len(live) == 1 && live[0].Revision == publication.Revision && publication.Intent == publicationIntent {
			expected = store.KnowledgeReference{ID: publication.ID, Revision: publication.Revision}
			hasExpected = true
		} else {
			unwritten := false
			if len(live) == 0 && publication.Intent == publicationIntent {
				unwritten, err = requiredPublicationUnwritten(book, publication)
				if err != nil {
					return item, nil, err
				}
			}
			// A never-written preview can retry its same create. Published,
			// changed or missing history still requires the current original target.
			if !unwritten && w.Articles[n.Target].ID != publication.ID {
				return item, nil, fmt.Errorf("the original publication changed, is unavailable or differs from this repair; read article %s and repair that current target", publication.ID)
			}
			hasExpected = false
		}
	}
	if hasExpected {
		live, err := book.LiveWhere(func(p notebook.Page) bool { return p.ID == expected.ID })
		if err != nil {
			return item, nil, err
		}
		if len(live) == 1 && live[0].Revision == expected.Revision {
			if err = commit(func() error { return finishWorkSources(book, d.Sources) }); err != nil {
				return workItem{Kind: "note", ID: expected.ID, Revision: expected.Revision}, nil, err
			}
			item.ID, item.Revision = expected.ID, expected.Revision
			w.Saved[key] = item
			if origin := w.PendingOrigins[n.Ref]; origin != "" {
				w.Saved[origin] = item
			}
			return item, nil, save()
		}
	}
	return publishWorkNote(book, w, n, d, item, key, publicationIntent, save, commit)
}
