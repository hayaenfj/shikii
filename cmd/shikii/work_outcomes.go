package main

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/hayaenfj/shikii/internal/notebook"
)

func recordWorkDiscovery(book notebook.Book, w *workState, token workToken, d workDiscovery, save func() error, commit workCommitter) (workItem, error) {
	item := workItem{Kind: "discovery"}
	key := "discovery:" + workHash(d)
	if old, ok := w.Saved[key]; ok {
		return old, nil
	}
	p, ok := w.Articles[d.Ref]
	if !ok {
		return item, errors.New("discovery needs a prepared article reference")
	}
	validated := notebook.Discovery{LookupID: p.ID, PageID: p.ID, Revision: p.Revision, Context: d.Context, Decision: d.Decision, Evidence: d.Evidence, CaseID: d.CaseID}
	// A turn, not a new preparation nonce, identifies the actual work. This
	// separates later uses of the same article without resetting first evidence.
	validated.WorkID = workHash([]string{token.Session, token.Turn})[:16]
	if err := validated.Validate(); err != nil {
		return item, err
	}
	if err := workSavedCapacity(w, key); err != nil {
		return item, err
	}
	// Preflight definite rejections before reserving recovery capacity. The
	// final publication repeats the same checks while holding the page lock.
	query := "lesson:" + p.ID + "@" + p.Revision
	already := false
	err := commit(func() error {
		v, e := book.Query(query)
		if e != nil {
			return e
		}
		if len(v.Lessons) != 1 {
			return fmt.Errorf("%w: the prepared discovery revision is unavailable", notebook.ErrDiscoveryRejected)
		}
		lookup, e := book.RecordLookup(query, v.Lessons)
		if e != nil {
			return e
		}
		validated.LookupID = lookup.ID
		already, e = book.CheckDiscovery(validated)
		return e
	})

	if err != nil {
		return item, releaseRejectedWorkReservation(w, key, err, save)
	}
	if !already {
		// Keep this slot if storage fails and publication is uncertain.
		w.Expected[key] = p
		if err = save(); err != nil {
			return item, err
		}
		err = commit(func() error { return book.Discover(validated) })
		if err != nil {
			return item, releaseRejectedWorkReservation(w, key, err, save)
		}
	}
	item.ID = p.ID
	item.Revision = p.Revision
	w.Saved[key] = item
	delete(w.Expected, key)
	return item, save()
}

func recordWorkOutcome(book notebook.Book, w *workState, o workOutcome, resolve func([]string) ([]string, error), save func() error, commit workCommitter) (workItem, error) {
	item := workItem{Kind: "outcome"}
	p, ok := w.Articles[o.Ref]
	if !ok {
		return item, errors.New("outcome needs a prepared howto reference")
	}
	details := notebook.OutcomeDetails{PageID: p.ID, Revision: p.Revision, Conditions: o.Conditions, Action: o.Action, Failure: o.Failure, Correction: o.Correction, Result: o.Result}
	if err := details.ValidateContent(); err != nil {
		return item, err
	}
	if len(o.Sources) == 0 || len(o.Sources) > 4 {
		return item, errors.New("an outcome needs one to four captured-observation references")
	}
	// an outcome is evidence a later result can cite: what it cites must be observed, as for a trial. An outcome citing the
	// work's own result made a result with nothing observed sufficient (audit of 0.31.0)
	w.citingEvidence = true
	defer func() { w.citingEvidence = false }()
	if err := selfEvidence(book, w, "", o.Sources); err != nil {
		return item, err
	}
	ids, err := resolve(o.Sources)
	if err != nil {
		return item, err
	}
	refs, err := book.OutcomeSources(ids)
	if err != nil {
		return item, err
	}
	details.Evidence = refs
	if err = details.Validate(); err != nil {
		return item, err
	}
	key := "outcome:" + workHash(details)
	if old, ok := w.Saved[key]; ok {
		return old, nil
	}
	if err = workSavedCapacity(w, key); err != nil {
		return item, err
	}
	content, _ := json.Marshal(details)
	observation := workObservation{Kind: "agent", Title: "Procedure execution with cited evidence", Content: string(content)}
	if _, _, err = prepareWorkObservation(w, observation); err != nil {
		return item, err
	}
	// Check the live kind/revision before capturing an execution snapshot.
	live, err := book.LiveWhere(func(page notebook.Page) bool { return page.ID == p.ID })
	if err != nil {
		return item, err
	}
	_, recovering := w.Expected[key]
	if !recovering && (len(live) != 1 || live[0].Kind != "howto" || live[0].Revision != p.Revision) {
		return item, errors.New("the outcome needs the same live howto revision; read it again")
	}
	w.Expected[key] = p
	if err = save(); err != nil {
		return item, err
	}
	_, id, err := captureWorkObservation(book, w, observation, save, commit)
	item.ID = id
	if err != nil {
		return item, err
	}
	snapshot, err := book.OutcomeSources([]string{id})
	if err != nil {
		return item, err
	}
	err = commit(func() error {
		return book.RecordOutcome(notebook.ProcedureOutcome{OutcomeDetails: details, Snapshot: snapshot[0]})
	})
	if err != nil {
		return item, releaseRejectedWorkReservation(w, key, err, save)
	}
	if err = workAfterWrite("outcome"); err != nil {
		return item, err
	}
	w.Saved[key] = item
	delete(w.Expected, key)
	return item, save()
}

// Rejection and uncertain storage failures have different recovery needs.
// Both discovery and procedure writers use this same reservation lifecycle.
func releaseRejectedWorkReservation(w *workState, key string, err error, save func() error) error {
	if errors.Is(err, notebook.ErrDiscoveryRejected) || errors.Is(err, notebook.ErrOutcomeRejected) {
		if _, reserved := w.Expected[key]; reserved {
			delete(w.Expected, key)
			return errors.Join(err, save())
		}
	}
	return err
}
