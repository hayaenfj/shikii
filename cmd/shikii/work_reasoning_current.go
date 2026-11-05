package main

import (
	"errors"
	"strings"

	"github.com/hayaenfj/shikii/internal/notebook"
)

// Current belongs to the hook turn, not to the lifetime of a preparation.
func resolveCurrentWorkSource(book notebook.Book, w *workState, save func() error, commit workCommitter) (string, error) {
	r := w.Reasoning
	if r == nil {
		return "", errors.New("the reasoning context is missing; prepare again")
	}
	current := r.CurrentSource
	if current == "" {
		// Retain a source made before the stable turn binding was added.
		for _, entry := range []*reasoningEntry{r.Start, r.Latest, r.Finish} {
			if entry == nil {
				continue
			}
			for _, ref := range entry.Evidence {
				if strings.HasPrefix(ref, "source:") {
					id := strings.TrimPrefix(ref, "source:")
					if matchesCurrentWorkSource(book, id, w.Current) {
						current = id
						break
					}
				}
			}
			if current != "" {
				break
			}
		}
	}
	if current != "" {
		if !matchesCurrentWorkSource(book, current, w.Current) {
			return "", errors.New("the saved current request source is unavailable or does not match this turn")
		}
	} else {
		_, source, err := captureWorkObservation(book, w, workObservation{Kind: "human", Title: "Current request quoted during work", Content: w.Current, Reference: "current request recorded by the hook"}, save, commit)
		if err != nil {
			return "", err
		}
		current = source
	}
	changed := r.CurrentSource != current || w.Sources["current"] != current
	r.CurrentSource = current
	w.Sources["current"] = current
	if changed {
		if err := save(); err != nil {
			return "", err
		}
	}
	return current, nil
}

func matchesCurrentWorkSource(book notebook.Book, id, content string) bool {
	source := prepareWorkSource(book, id)
	if !source.Complete {
		return false
	}
	observations, _ := source.Material["observations"].([]notebook.Observation)
	return len(observations) == 1 && observations[0].ID == id && observations[0].Kind == "human" && observations[0].Title == "Current request quoted during work" && observations[0].Reference == "current request recorded by the hook" && observations[0].Content == content
}
