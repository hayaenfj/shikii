package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// writeAnswerAssessment places one assessment of a quoted past answer as SaveMemoryAssessment leaves it, at the time at.
func writeAnswerAssessment(t *testing.T, s Store, scope, name, trace, result string, at time.Time) {
	t.Helper()
	dir := s.recordPath("memory-reviews", "", scope)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(MemoryAssessment{SourceID: name, Key: "decision:" + trace, Result: result, Decision: "test", Evidence: "test", At: at})
	if err := os.WriteFile(filepath.Join(dir, name+".json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
}

// writeAnswerRestoreLines writes the person's undos as raw lines, broken ones included.
func writeAnswerRestoreLines(t *testing.T, s Store, scope string, lines ...string) {
	t.Helper()
	path := s.recordPath("answer-restores", ".jsonl", scope)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	text := ""
	for _, l := range lines {
		text += l + "\n"
	}
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
}

// An undo names exactly one of an answer and the stop, in a project, at a time; anything else is refused and not written. The
// latest undo of an answer is what AnswerEffects reports for it, and the store of another project knows none of them.
func TestAnswerRestoresAreKeptPerProjectAndNameOneTarget(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	scope := "/tmp/project"
	first := time.Date(2026, 11, 11, 9, 0, 0, 0, time.UTC)
	for name, bad := range map[string]struct {
		scope string
		r     AnswerRestore
	}{
		"no project":  {"", AnswerRestore{TraceID: "0000000000000001", At: first}},
		"no target":   {scope, AnswerRestore{At: first}},
		"two targets": {scope, AnswerRestore{TraceID: "0000000000000001", Stop: true, At: first}},
		"no time":     {scope, AnswerRestore{Stop: true}},
	} {
		if err := s.AppendAnswerRestore(bad.scope, bad.r); err == nil {
			t.Errorf("%s: the undo is refused", name)
		}
	}
	if _, err := os.Stat(s.answerRestoresPath(scope)); !os.IsNotExist(err) {
		t.Fatalf("a refused undo writes nothing: %v", err)
	}
	for _, r := range []AnswerRestore{{TraceID: "0000000000000001", At: first}, {TraceID: "0000000000000001", At: first.Add(time.Hour)}, {Stop: true, At: first.Add(2 * time.Hour)}} {
		if err := s.AppendAnswerRestore(scope, r); err != nil {
			t.Fatal(err)
		}
	}
	e := s.AnswerEffects(scope)
	if !e.Restored["0000000000000001"].Equal(first.Add(time.Hour)) || !e.StopRestored.Equal(first.Add(2*time.Hour)) || len(e.Restored) != 1 {
		t.Fatalf("the latest undo of the answer and of the stop are read back: %+v", e)
	}
	other := Store{Dir: t.TempDir()}
	if e := other.AnswerEffects(scope); len(e.Restored) != 0 || !e.StopRestored.IsZero() {
		t.Fatalf("another project's store knows none of the undos: %+v", e)
	}
}

// After the person undoes the stop of quoting past answers, only the assessments after the undo count toward the project's total
// (the stop's count). A broken line, a line without a time and a line naming neither an answer nor the stop undo nothing.
func TestAnswerEffectsCountOnlyAssessmentsAfterTheStopWasUndone(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	scope := "/tmp/project"
	undo := time.Date(2026, 11, 11, 9, 0, 0, 0, time.UTC)
	writeAnswerAssessment(t, s, scope, "a1", "0000000000000001", "unused", undo.Add(-2*time.Hour))
	writeAnswerAssessment(t, s, scope, "a2", "0000000000000002", "unused", undo.Add(-time.Hour))
	writeAnswerAssessment(t, s, scope, "a3", "0000000000000003", "unused", undo.Add(time.Hour))
	if total := s.AnswerEffects(scope).Total; total.Unused != 3 {
		t.Fatalf("precondition: without an undo every assessment counts: %+v", total)
	}
	writeAnswerRestoreLines(t, s, scope,
		`{"scope":"/tmp/project","stop":true,"at":"`+undo.Format(time.RFC3339Nano)+`"}`,
		`{"scope":"/tmp/project","stop":true,"at":`,                                          // broken
		`{"scope":"/tmp/project","stop":true}`,                                               // no time
		`{"scope":"/tmp/project","at":"`+undo.Add(3*time.Hour).Format(time.RFC3339Nano)+`"}`, // no target
	)
	if total := s.AnswerEffects(scope).Total; total.Unused != 1 || total.Helpful != 0 {
		t.Fatalf("after the undo of the stop only the later assessment counts: %+v", total)
	}
}
