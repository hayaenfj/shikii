package store

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// memoryAssessmentRefusal says what a refused assessment lacks, so the agent that sent it sends it again whole instead of
// dropping what it had (measured with a real Codex, 2026-11-17: after a refusal that named nothing it sent the result alone, and
// it did so again after one that also said which assessments may leave both out). It only says what to add.
func memoryAssessmentRefusal(a MemoryAssessment) string {
	const again = " to the same entry and send it again"
	const evidence = "evidence (the check result, or what you did, that shows it)"
	// A text that is too long is named with its size and the limit, with the evidence the entry also lacks: one refusal says
	// everything to change, and the agent keeps the rest of the entry.
	long := []string{}
	if n := utf8.RuneCountInString(a.Decision); n > AssessmentTextLimit {
		long = append(long, fmt.Sprintf("decision (%d characters)", n))
	}
	if n := utf8.RuneCountInString(a.Evidence); n > AssessmentTextLimit {
		long = append(long, fmt.Sprintf("evidence (%d characters)", n))
	}
	if len(long) > 0 {
		text := fmt.Sprintf("decision and evidence of a memory assessment take at most %d characters each: shorten %s in the same entry", AssessmentTextLimit, strings.Join(long, " and "))
		if strings.TrimSpace(a.Evidence) == "" {
			text += ", add " + evidence
		}
		return text + " and send it again"
	}
	switch {
	case strings.TrimSpace(a.Evidence) == "":
		return "a memory assessment needs evidence: add " + evidence + again
	case strings.TrimSpace(a.Decision) == "":
		return "a memory assessment needs decision: add decision (what you decided or did about the lesson)" + again
	}
	return "a provisional memory evaluation needs grounds for the target, the judgment and the result. Use unknown when unclear"
}

func validMemoryAssessment(a MemoryAssessment) bool {
	if !validCorrectionResult(a.CorrectionResult) || (a.CorrectionResult != "" && a.CorrectionResult != "unknown" && strings.TrimSpace(a.Decision) == "") {
		return false
	}
	if !textTrace.MatchString(a.SourceID) || a.Key == "" || len(a.Key) > 80 || strings.TrimSpace(a.Evidence) == "" || utf8.RuneCountInString(a.Decision) > AssessmentTextLimit || utf8.RuneCountInString(a.Evidence) > AssessmentTextLimit {
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
