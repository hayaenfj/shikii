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
	var value notebook.Observation
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
		return value, "", errors.New("an observation must fit 64 KiB; retain a long report in handoff and cite its relevant evidence")
	}
	value = notebook.Observation{Kind: o.Kind, Title: o.Title, Content: o.Content, Reference: o.Reference, Version: o.Version, ObservedAt: time.Now().UTC()}
	if err := value.Validate(); err != nil {
		return value, "", err
	}
	key := workHash(o)
	if _, exists := w.Captures[key]; !exists && len(w.Captures) >= 32 {
		return value, key, errors.New("this work has reached its observation limit; prepare again")
	}
	return value, key, nil
}

func captureWorkObservation(book notebook.Book, w *workState, o workObservation, save func() error, commit workCommitter) (string, string, error) {
	value, key, err := prepareWorkObservation(w, o)
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
		memo = workCapture{Original: &normalized, Ref: "observed" + strconv.Itoa(index), At: value.ObservedAt, Version: value.Version, Report: o.report}
		w.Captures[key] = memo
		if err := save(); err != nil {
			return memo.Ref, "", err
		}
	} else if o.report && !memo.Report {
		memo.Report = true
		w.Captures[key] = memo
	}
	if memo.ID != "" {
		if memo.Report {
			markReport(w, memo.ID)
		}
		return memo.Ref, memo.ID, nil
	}
	value.ObservedAt, value.Version = memo.At, memo.Version
	var captured notebook.Observation
	err = commit(func() error { var e error; captured, e = book.Capture(value); return e })
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
	if memo.Report {
		markReport(w, memo.ID)
	}
	if err = save(); err != nil {
		return memo.Ref, memo.ID, err
	}
	return memo.Ref, memo.ID, nil
}
