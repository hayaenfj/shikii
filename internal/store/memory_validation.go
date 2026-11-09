package store

import (
	"strings"
)

func validMemoryAssessment(a MemoryAssessment) bool {
	if !validCorrectionResult(a.CorrectionResult) || (a.CorrectionResult != "" && a.CorrectionResult != "unknown" && strings.TrimSpace(a.Decision) == "") {
		return false
	}
	if !textTrace.MatchString(a.SourceID) || a.Key == "" || len(a.Key) > 80 || strings.TrimSpace(a.Evidence) == "" || len(a.Decision) > feedbackTextMax || len(a.Evidence) > feedbackTextMax {
		return false
	}
	switch a.Result {
	case "helpful", "harmful", "unused":
		return strings.TrimSpace(a.Decision) != ""
	case "unknown":
		return true
	}
	return false
}
