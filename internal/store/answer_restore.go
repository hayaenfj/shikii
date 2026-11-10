package store

import (
	"encoding/json"
	"errors"
	"time"
)

// AnswerRestore is the person's undo, from the page, of what the AI decided from the assessments of quoted past answers: the
// rejection of one answer (TraceID) or the stop of quoting past answers in the project (Stop). The assessments up to At no longer
// count for what was undone; later ones count as before. The undos are appended to records/answer-restores/<store name>.jsonl under
// the JSONL lock and never removed. Like the assessments they are kept per store (the project's own records), so an undo in one
// project never reaches another; Scope records the project path it was made at and is not used to read them (a moved folder keeps
// its undos, as it keeps its assessments).
type AnswerRestore struct {
	Scope   string    `json:"scope"`
	TraceID string    `json:"trace_id,omitempty"`
	Stop    bool      `json:"stop,omitempty"`
	At      time.Time `json:"at"`
}

// valid: a time, and exactly one of an answer and the stop.
func (r AnswerRestore) valid() bool {
	return !r.At.IsZero() && (r.TraceID != "") != r.Stop && len(r.TraceID) <= 128
}

func (s Store) answerRestoresPath(scope string) string {
	return s.recordPath("answer-restores", ".jsonl", scope)
}

// AppendAnswerRestore keeps one undo of the person in the project scope.
func (s Store) AppendAnswerRestore(scope string, r AnswerRestore) error {
	r.Scope = scope
	if scope == "" || !r.valid() {
		return errors.New("an undo needs the project, a time, and either one answer or the stop of quoting past answers")
	}
	return s.appendJSONL(s.answerRestoresPath(scope), r)
}

// answerRestores reads the undos of the project. A line that cannot be read, or names neither an answer nor the stop, undoes
// nothing; a missing or unreadable file means nothing was undone.
func (s Store) answerRestores(scope string) []AnswerRestore {
	out := []AnswerRestore{}
	_ = s.scanJSONL(s.answerRestoresPath(scope), func(line []byte) error { // as every other reader: a long line is skipped, not the rest
		var r AnswerRestore
		if json.Unmarshal(line, &r) == nil && r.valid() {
			out = append(out, r)
		}
		return nil
	})
	return out
}
