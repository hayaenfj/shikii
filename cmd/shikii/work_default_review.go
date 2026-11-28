package main

import "encoding/json"

// The ending review shikii writes itself when an agent records a result and no review of its own (finishWorkCompletion). Its four
// sentences are fixed: they say that the report is kept in its source and that saving it establishes nothing about success. The
// review is saved whole, with these sentences, in the history and in its immutable source. Only a record's reply leaves them out
// (recordLatest), and it finds the review by comparing the saved sentences with these constants, so a review the agent wrote is
// never mistaken for it, and an older record is read the same way. The skill's references/recording.md quotes them, and a test compares.
const (
	defaultReviewFocus      = "Review the saved work result"
	defaultReviewReason     = "The agent report and its remaining conditions are retained in the result source; saving does not establish success."
	defaultReviewNextAction = "Use the saved result and unresolved conditions for the next task."
	defaultReviewResult     = "The work report is saved; see its source for changes, checks and remaining conditions."
)

// reasoningSavedNext is what reasoning.next says once the ending review is saved and nothing is pending. A record's reply leaves
// out the sentences of the review shikii wrote only while this sentence, which says that recording alone is no proof, is in the same
// reply; with any other next the reply keeps them.
const reasoningSavedNext = "The review is saved. New observations, actual uses, outcomes or wiki edits require an updated finish; recording alone is not proof of improvement."

// defaultEndingReview is the review of a saved report: retained, or unresolved when the report left a condition open.
func defaultEndingReview(evidence []string, unresolved bool) workReasoning {
	r := workReasoning{Phase: "finish", Focus: defaultReviewFocus, Assessment: "retain", Reason: defaultReviewReason, Evidence: evidence, NextAction: defaultReviewNextAction, Result: defaultReviewResult}
	if unresolved {
		r.Assessment = "unresolved"
	}
	return r
}

// isDefaultReview says whether the four sentences of an entry are exactly the fixed ones. Nothing else counts: no flag is saved with
// the review, so a changed character in any of the four makes it the agent's.
func isDefaultReview(e *reasoningEntry) bool {
	return e != nil && e.Focus == defaultReviewFocus && e.Reason == defaultReviewReason && e.NextAction == defaultReviewNextAction && e.Result == defaultReviewResult
}

// recordReasoning is the reasoning status of a record's reply. It is reasoningStatus, except that latest is written by recordLatest.
// Work prepare and the other replies keep reasoningStatus and its latest as they are.
type recordReasoning struct {
	reasoningStatus
	Latest json.RawMessage `json:"latest,omitempty"` // shadows reasoningStatus.Latest, which is not written here
}

func recordReasoningView(v reasoningStatus) recordReasoning {
	return recordReasoning{reasoningStatus: v, Latest: recordLatest(v)}
}

// recordLatest writes the latest review whole, except when it is the one shikii wrote and the reply still says that recording is no
// proof (reasoningSavedNext): then its four fixed sentences give way to "default":true and every other field stays as it is.
func recordLatest(v reasoningStatus) json.RawMessage {
	if v.Latest == nil {
		return nil
	}
	whole, err := json.Marshal(v.Latest)
	if err != nil || !isDefaultReview(v.Latest) || v.Next != reasoningSavedNext {
		return whole
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(whole, &fields) != nil {
		return whole
	}
	for _, name := range []string{"focus", "reason", "next_action", "result"} {
		delete(fields, name)
	}
	fields["default"] = json.RawMessage("true")
	left, err := json.Marshal(fields)
	if err != nil {
		return whole
	}
	return left
}
