package main

import "errors"

const workSavedLimit = 128

// workSavedCapacity counts saved replies and publication recovery reservations.
// Existing replies and reserved recoveries remain usable at the limit.
// The caller holds the work lock until all checked keys have been stored.
func workSavedCapacity(w *workState, keys ...string) error {
	if w == nil {
		return errors.New("the prepared recording metadata is missing; prepare again")
	}
	occupied := map[string]bool{}
	for key := range w.Saved {
		occupied[key] = true
	}
	for key := range w.Expected {
		occupied[key] = true
	}
	if r := w.Reasoning; r != nil {
		for ref, trial := range r.Trials {
			if trial.Source == "" {
				occupied["trial:"+ref] = true
			}
			if trial.Result != nil && trial.ResultSource == "" {
				occupied["trial_result:"+ref] = true
			}
		}
		for ref, comparison := range r.Comparisons {
			if comparison.Source == "" {
				occupied["comparison:"+ref] = true
			}
		}
	}
	added := map[string]bool{}
	for _, key := range keys {
		if !occupied[key] {
			added[key] = true
		}
	}
	if len(added) == 0 {
		return nil
	}
	if len(occupied)+len(added) > workSavedLimit {
		return errors.New("this work has reached its saved-item limit; prepare again for new content")
	}
	return nil
}
