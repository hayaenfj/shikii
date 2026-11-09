package main

import (
	"errors"
	"fmt"
	"reflect"
	"strings"

	"github.com/hayaenfj/shikii/internal/notebook"
)

func prepareWorkNoteDraft(w *workState, n workNote, resolve func([]string) ([]string, error)) (notebook.Draft, error) {
	d := n.Draft
	if n.Ref == "" && n.Target != "" && n.Target != "new" {
		allowed := d
		allowed.Sources = nil
		allowed.Passages = nil
		allowed.Learning = nil
		allowed.ChangeReason = ""
		if !reflect.DeepEqual(allowed, notebook.Draft{}) {
			return d, errors.New("an existing article accepts sources, change_reason, exact edits, interpreted passages and learning; use wiki operations for structural changes")
		}
	}
	if (n.Target == "" || n.Target == "new") && len(n.Edits) > 0 {
		return d, errors.New("exact edits require a prepared existing article target; a new note uses its draft content")
	}

	if n.Ref != "" {
		prior, ok := w.Pending[n.Ref]
		if !ok {
			return d, errors.New("unknown pending note reference")
		}
		if n.Target == "" {
			return d, errors.New("resolving a pending note needs target new or a prepared article reference")
		}
		if !reflect.DeepEqual(n.Draft, notebook.Draft{}) {
			return d, errors.New("resolve a pending note with ref, target and edits only")
		}
		if n.Target != "new" {
			structural := prior
			structural.Title, structural.Lesson, structural.AppliesTo = "", "", ""
			structural.ChangeReason, structural.Kind, structural.Scene = "", "", ""
			structural.Sources, structural.Body, structural.Rationale = nil, nil, nil
			if len(structural.Related) == 0 {
				structural.Related = nil
			}
			if !reflect.DeepEqual(structural, notebook.Draft{}) {
				return d, errors.New("the pending note contains structural metadata that exact edits cannot apply; use wiki ingest or target new to retain it")
			}
		}
		d = prior
	} else {
		if (n.Target == "" || n.Target == "new") && strings.TrimSpace(d.Lesson) == "" {
			return d, errors.New("a new note needs a lesson; use reviewed for sources without lasting knowledge")
		}
		if d.ID != "" || len(d.Replaces) > 0 {
			return d, errors.New("use a prepared target and exact edits; supersession remains available through wiki ingest")
		}
		sources, err := resolve(d.Sources)
		if err != nil {
			return d, err
		}
		d.Sources = sources
		if d.Learning != nil {
			for i := range d.Learning.Connections {
				c := &d.Learning.Connections[i]
				if ref, ok := w.Articles[c.ID]; ok {
					c.ID, c.Revision = ref.ID, ref.Revision
				}
			}
			if q := d.Learning.Probe; q != nil {
				for i := range q.Results {
					ids, err := resolve(q.Results[i].Sources)
					if err != nil {
						return d, err
					}
					q.Results[i].Sources = ids
				}
			}
			if q := d.Learning.Practice; q != nil {
				for i := range q.Steps {
					ids, err := resolve(q.Steps[i].Sources)
					if err != nil {
						return d, err
					}
					q.Steps[i].Sources = ids
				}
			}
		}
		related := []string{}
		for _, ref := range d.Related {
			p, ok := w.Articles[ref]
			if !ok {
				return d, fmt.Errorf("unknown related article %q", ref)
			}
			related = append(related, p.ID)
		}
		d.Related = related
		if d.Correction != nil {
			sources, err := resolve([]string{d.Correction.SourceID})
			if err != nil {
				return d, err
			}
			c := *d.Correction
			c.SourceID = sources[0]
			d.Correction = &c
		}
	}
	return d, nil
}
