package main

import (
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/hayaenfj/shikii"
	"github.com/hayaenfj/shikii/internal/notebook"
)

func prepareWorkObservation(w *workState, o workObservation) (notebook.Observation, string, error) {
	value, key, _, err := checkWorkSource(w, o)
	return value, key, err
}

// checkWorkSource checks a source before it is saved and says which it is: the agent's (or the person's) observation, refused under
// a title of a work source, or a work source shikii writes (o.source, or the kind its capture memo kept when a capture that failed
// once is finished).
func checkWorkSource(w *workState, o workObservation) (notebook.Observation, string, notebook.WorkSource, error) {
	var value notebook.Observation
	if !o.source.IsZero() {
		o.Kind, o.Title = "agent", o.source.Title()
	}
	if o.Kind == "" {
		o.Kind = "agent"
	}
	if o.Reference == "" {
		o.Reference = "work record"
	}
	if o.Version == "" {
		o.Version = "recorded by shikii " + shikii.Version
	}
	if len(o.Content) > 64<<10 {
		return value, "", notebook.WorkSource{}, errors.New("an observation must fit 64 KiB; retain a long report in handoff and cite its relevant evidence")
	}
	value = notebook.Observation{Kind: o.Kind, Title: o.Title, Content: o.Content, Reference: o.Reference, Version: o.Version, ObservedAt: time.Now().UTC()}
	// the key holds the kind: an observation of the agent with the very fields of a report the work wrote takes no capture of the
	// report (audit M of 0.31.0: it was handed the report's id, and a result cited it). An observation is always checked as one.
	key := workHash(o)
	source := o.source
	if !source.IsZero() {
		key = workHash(struct {
			Observation workObservation
			Source      notebook.WorkSource
		}{o, source})
	}
	check := value.ValidateObserved
	if !source.IsZero() {
		check = value.Validate
	}
	if err := check(); err != nil {
		return value, "", notebook.WorkSource{}, err
	}
	if _, exists := w.Captures[key]; !exists && len(w.Captures) >= 32 {
		return value, key, source, errors.New("this work has reached its observation limit; prepare again")
	}
	return value, key, source, nil
}

func captureWorkObservation(book notebook.Book, w *workState, o workObservation, save func() error, commit workCommitter) (string, string, error) {
	value, key, source, err := checkWorkSource(w, o)
	if err != nil {
		return "", "", err
	}

	memo, ok := w.Captures[key]
	if !ok {
		normalized := workObservation{Kind: value.Kind, Title: value.Title, Content: value.Content, Reference: value.Reference, Version: value.Version}
		index := w.ObservedBase + 1
		for _, previous := range w.Captures {
			if n, err := strconv.Atoi(strings.TrimPrefix(previous.Ref, "observed")); err == nil && n >= index {
				index = n + 1
			}
		}
		memo = workCapture{Original: &normalized, Ref: "observed" + strconv.Itoa(index), At: value.ObservedAt, Version: value.Version, Source: source}
		w.Captures[key] = memo
		if err := save(); err != nil {
			return memo.Ref, "", err
		}
	}
	if memo.ID != "" {
		return memo.Ref, memo.ID, nil
	}
	value.ObservedAt, value.Version = memo.At, memo.Version
	var captured notebook.Observation
	err = commit(func() error {
		var e error
		if !memo.Source.IsZero() {
			captured, e = book.CaptureWork(memo.Source, value.Content, value.Reference, value.Version, value.ObservedAt)
		} else {
			captured, e = book.CaptureObserved(value)
		}
		return e
	})
	if err != nil {
		return memo.Ref, "", err
	}
	if err = workAfterWrite("observation"); err != nil {
		return memo.Ref, captured.ID, err
	}
	memo.ID = captured.ID
	memo.Original, memo.Archive = nil, ""
	w.Captures[key] = memo
	w.Sources[memo.Ref] = memo.ID
	if !memo.Source.IsZero() && !memo.Source.Citable() {
		markReport(w, memo.ID)
	}
	if err = save(); err != nil {
		return memo.Ref, memo.ID, err
	}
	return memo.Ref, memo.ID, nil
}
