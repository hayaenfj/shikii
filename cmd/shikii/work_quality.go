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

// Each optional quality report contributes its actual immutable check sources.
// A cached reading from another task is a backlog, not this report's evidence.
type workQualityEvidence struct {
	Sources []notebook.Source
	Err     error
}

func workReadingQualityEvidence(book notebook.Book, w *workState, reading *workReadingFeedback, resolve func([]string) ([]string, error)) workQualityEvidence {
	if reading == nil {
		return workQualityEvidence{}
	}
	if reading.Ref != "" {
		saved := w.Reasoning.Readings[reading.Ref]
		if saved == nil {
			return workQualityEvidence{Err: errors.New("unknown reading execution reference")}
		}
		if saved.Details.Work != workHash([]string{w.Reasoning.SessionID, w.Reasoning.TurnID})[:16] || w.Reasoning.Reading == nil || saved.Details.Case != w.Reasoning.Reading.Case {
			return workQualityEvidence{}
		}
		if reading.Check != "" || reading.Sufficient != nil || len(reading.Evidence) != 0 {
			return workQualityEvidence{Err: errors.New("reading recovery takes only its original reference")}
		}
		_, e := book.ReadingEvidence(saved.Details)
		return workQualityEvidence{Sources: saved.Details.Evidence, Err: e}
	}
	details, e := prepareWorkReadingDetails(book, w, *reading, resolve)
	return workQualityEvidence{Sources: details.Evidence, Err: e}
}

func mergeWorkQualityEvidence(primary []notebook.Source, additional workQualityEvidence, grade *bool) ([]notebook.Source, error) {
	// A checked negative must still restore the procedure if auxiliary work fails.
	if grade != nil && !*grade {
		return primary, nil
	}
	if additional.Err != nil {
		return nil, additional.Err
	}
	out := append([]notebook.Source(nil), primary...)
	for _, source := range additional.Sources {
		found := false
		for _, saved := range out {
			found = found || source == saved
		}
		if !found {
			out = append(out, source)
		}
	}
	return out, nil
}

func excludeWorkQuality(w *workState, p *workProcedureProgress, source string, grade *bool) {
	status := "ungraded"
	if grade != nil {
		status = "ineligible"
	}
	p.Pending = nil
	p.Indexed = source
	p.View = &notebook.ProcedureStatus{Status: status, Policy: p.Trace.Config.ID, Ineligible: true, QualityNotice: "The report is saved; its accompanying reading check is unavailable. It cannot validate a procedure. Report a new actual check when available."}
	if w.Result != nil && w.Result.Source == source && grade != nil {
		w.Result.Unresolved = true
	}
	delete(w.Sources, "procedure")
}
