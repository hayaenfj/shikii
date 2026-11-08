package main

import (
	"encoding/json"
	"errors"
	"github.com/hayaenfj/shikii/internal/notebook"
)

// Compare the immutable grades behind recovery refs as well as fresh input.
// Backlogs from another task retain their original grades without being paired
// with the current task's reading. Reject contradictions before any side effect.
func validateWorkQualityAgreement(book notebook.Book, w *workState, req workRequest) error {
	if req.Result == nil || req.Reading == nil {
		return nil
	}
	resultQuality := req.Result.Sufficient
	if req.Result.Ref != "" {
		var original *workCompletionRef
		if p := w.Reasoning.PendingResults[req.Result.Ref]; p != nil && p.Turn == w.Reasoning.TurnID {
			original = p.Result
		}
		if w.Result != nil && w.Result.Source == req.Result.Ref {
			original = w.Result
		}
		if original == nil {
			return nil
		}
		material, e := book.ReadSources([]string{original.Source})
		if e != nil {
			return e
		}
		observations := material["observations"].([]notebook.Observation)
		var report workCompletion
		if len(observations) != 1 || observations[0].Kind != "agent" || observations[0].Title != "Work result and remaining conditions" || json.Unmarshal([]byte(observations[0].Content), &report) != nil || workHash(report) != original.Signature {
			return errors.New("the saved result quality changed or is unavailable")
		}
		resultQuality = report.Sufficient
	}
	readingQuality := req.Reading.Sufficient
	if req.Reading.Ref != "" {
		reading := w.Reasoning.Readings[req.Reading.Ref]
		if reading == nil || reading.Details.Work != workHash([]string{w.Reasoning.SessionID, w.Reasoning.TurnID})[:16] || w.Reasoning.Reading == nil || reading.Details.Case != w.Reasoning.Reading.Case {
			return nil
		}
		grade := reading.Details.Sufficient
		readingQuality = &grade
	}
	if resultQuality != nil && readingQuality != nil && *resultQuality != *readingQuality {
		return errors.New("result and reading quality disagree; retain the actual checked result")
	}
	return nil
}
