package main

import (
	"errors"

	"github.com/hayaenfj/shikii/internal/notebook"
)

func finishWorkSources(book notebook.Book, sources []string) error {
	report, err := book.Lint()
	if err != nil {
		return err
	}
	if len(report.Issues) > 0 {
		return errors.New("the article was saved but wiki lint reports unresolved issues; inspect wiki lint before retrying")
	}
	_, err = book.Ingest(notebook.Draft{Sources: sources})
	return err
}

func uniqueWorkStrings(values []string) []string {
	result := []string{}
	seen := map[string]bool{}
	for _, v := range values {
		if !seen[v] {
			result = append(result, v)
			seen[v] = true
		}
	}
	return result
}

// Failed-ref repairs do not introduce a new work report. This recognizes only
// the existing API's recovery forms, never new evidence or a fresh decision.
func onlyWorkRecovery(req workRequest) bool {
	if len(req.Observations)+len(req.Outcomes)+len(req.Discoveries) > 0 || req.Reasoning != nil || req.Agreed != nil || req.Open != nil || req.ContinuesPrevious != nil {
		return false
	}
	if req.Result != nil && req.Result.Ref == "" || req.Reading != nil && req.Reading.Ref == "" || req.Trial != nil && (req.Trial.Ref == "" || req.Trial.Result != nil) || req.Comparison != nil && req.Comparison.Ref == "" {
		return false
	}
	for _, note := range req.Notes {
		if note.Ref == "" {
			return false
		}
	}
	return true
}
