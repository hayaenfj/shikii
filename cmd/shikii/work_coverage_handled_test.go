package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// coverageLedgerReadBytes is how much of the end of the ledger the store reads (feedbackReadMax in internal/store).
const coverageLedgerReadBytes = 4 << 20

// ledgerFile is the path of the project's coverage ledger of a flow (the one file records/coverage holds).
func (f coverageFlow) ledgerFile() string {
	f.t.Helper()
	files, err := filepath.Glob(filepath.Join(f.b.Root, "records", "coverage", "*.jsonl"))
	if err != nil || len(files) != 1 {
		f.t.Fatalf("the project has one ledger file: %v %v", files, err)
	}
	return files[0]
}

// fillLedger appends more than bytes of raw valid lines of one area that ends closed, in a single write: the lines of the results
// that came after, as many as it takes to push the earlier lines out of the 4MB the ledger reads.
func (f coverageFlow) fillLedger(bytes int) {
	f.t.Helper()
	pair := `{"at":"2026-01-01T00:00:00Z","area":"filler","outcome":"open","source":"00000000000000f6"}` + "\n" +
		`{"at":"2026-01-01T00:00:01Z","area":"filler","outcome":"excluded","reason":"filler","source":"00000000000000f6"}` + "\n"
	file, err := os.OpenFile(f.ledgerFile(), os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		f.t.Fatal(err)
	}
	defer file.Close()
	if _, err := file.WriteString(strings.Repeat(pair, bytes/len(pair)+1)); err != nil {
		f.t.Fatal(err)
	}
}

func (f coverageFlow) ledgerSize() int64 {
	f.t.Helper()
	info, err := os.Stat(f.ledgerFile())
	if err != nil {
		f.t.Fatal(err)
	}
	return info.Size()
}

// A result the work already handled never goes to the ledger again. The store decides "this result already wrote this area" from the
// lines it reads, the newest 4MB; when the result's own lines have left that window (about 8,000 later results), a record sent
// again would write its rows again, and an open row would come back for an area a later result closed. The work remembers the
// results it handled (CoverageHandled), so it does not ask the store at all. The reply of the record sent again says what it said
// inside the window: the rows the result sent, the areas it opened, and nothing handed over.
func TestWorkCoverageAResultTheWorkHandledIsNotWrittenAgainWhenItsLinesAreOutOfTheWindow(t *testing.T) {
	f := newCoverageFlow(t)
	first := f.prepare()
	result := coverageResult(coverageRow("area X", "open"))
	f.record(first, result)
	f.record(f.prepare(), coverageResult(coverageRow("area X", "reported", "new1"))) // a later result closes it
	if got := openAreasOf(t, f.prepare()); len(got) != 0 {
		t.Fatalf("the fixture: area X is closed: %q", got)
	}
	inside := f.record(first, result) // the control: sent again with its lines inside the window
	if reply := coverageReplyOf(t, inside); reply["rows"] != float64(1) || !slices.Equal(reply["open"].([]any), []any{"area X"}) || reply["handed_over"] != nil {
		t.Fatalf("the control: a record sent again says what it sent and opened: %v", reply)
	}
	f.fillLedger(coverageLedgerReadBytes + 1<<20)
	before := f.ledgerSize()
	if before <= coverageLedgerReadBytes {
		t.Fatalf("the fixture: the ledger is not over 4MB (%d bytes)", before)
	}
	outside := f.record(first, result) // byte for byte the same request, its lines now far outside the 4MB the ledger reads
	if outside["complete"] != true {
		t.Fatalf("the record sent again is complete: %v", outside)
	}
	if fmt.Sprint(coverageReplyOf(t, outside)) != fmt.Sprint(coverageReplyOf(t, inside)) {
		t.Fatalf("the reply of a record sent again does not change with the size of the ledger: %v, inside the window it said %v", outside["coverage"], inside["coverage"])
	}
	if after := f.ledgerSize(); after != before {
		t.Errorf("the record sent again wrote %d bytes to the ledger", after-before)
	}
	if got := openAreasOf(t, f.prepare()); len(got) != 0 {
		t.Errorf("the area a later result closed is open again after a record was sent again: %q", got)
	}
}

// The row made from the remaining of a result is not written again either. A result that left something in remaining and sent no
// coverage row is handled by the work once: its row is given to the ledger, a later work that is handed the row closes it, and the
// lines of both leave the 4MB the ledger reads. A record sent again after that must not open the area again.
func TestWorkCoverageARemainingTheWorkGaveTheLedgerIsNotWrittenAgainWhenItsLinesAreOutOfTheWindow(t *testing.T) {
	f := newCoverageFlow(t)
	const area = "The import path was not read."
	first := f.prepare()
	result := withRemaining(coverageResult(), area) // remaining only: no coverage row
	f.record(first, result)
	second := f.prepare()
	if listedRow(second, area) == nil {
		t.Fatalf("the fixture: the next preparation is handed the row: %v", second["coverage"])
	}
	closing := f.record(second, workCompletion{Changed: "Read the import path.", Evidence: []string{"new1"}})
	if got := handedOverOf(closing); !slices.Equal(got, []any{area}) {
		t.Fatalf("the fixture: the work that was handed the row closes it by recording: %v", got)
	}
	if got := openAreasOf(t, f.prepare()); len(got) != 0 {
		t.Fatalf("the fixture: the area is closed: %q", got)
	}
	inside := f.record(first, result) // the control: sent again with its lines inside the window
	if reply := coverageReplyOf(t, inside); reply["rows"] != float64(0) || !slices.Equal(reply["open"].([]any), []any{area}) || reply["handed_over"] != nil {
		t.Fatalf("the control: a record sent again says what it opened: %v", reply)
	}
	f.fillLedger(coverageLedgerReadBytes + 1<<20)
	before := f.ledgerSize()
	outside := f.record(first, result) // byte for byte the same request, its lines now far outside the 4MB the ledger reads
	if outside["complete"] != true {
		t.Fatalf("the record sent again is complete: %v", outside)
	}
	if fmt.Sprint(coverageReplyOf(t, outside)) != fmt.Sprint(coverageReplyOf(t, inside)) {
		t.Errorf("the reply of a record sent again does not change with the size of the ledger: %v, inside the window it said %v", outside["coverage"], inside["coverage"])
	}
	if after := f.ledgerSize(); after != before {
		t.Errorf("the record sent again wrote %d bytes to the ledger", after-before)
	}
	if got := openAreasOf(t, f.prepare()); len(got) != 0 {
		t.Errorf("the area a later work closed is open again after a record was sent again: %q", got)
	}
}

// The work remembers the result whose row of remaining it gave to the ledger (CoverageLeft), once the record is saved, and only that
// result: not one that sent coverage rows and no remaining, and the state of a work that remembers nothing has no such field.
func TestWorkCoverageLeftIsTheResultsWhoseRemainingWasGivenToTheLedger(t *testing.T) {
	if data, err := json.Marshal(workState{}); err != nil || strings.Contains(string(data), "coverage_left") {
		t.Fatalf("an empty work state has no coverage_left: %s %v", data, err)
	}
	f := newCoverageFlow(t)
	prepared := f.prepare()
	if f.state(prepared).CoverageLeft != nil {
		t.Fatalf("a prepared work has given nothing: %q", f.state(prepared).CoverageLeft)
	}
	f.record(prepared, coverageResult(coverageRow("export filters", "open"))) // coverage rows and no remaining
	if got := f.state(prepared).CoverageLeft; got != nil {
		t.Fatalf("a result with no remaining gave the ledger no row of it: %q", got)
	}
	f.record(prepared, withRemaining(coverageResult(), "alpha left"))
	if got := f.state(prepared).CoverageLeft; !slices.Equal(got, []string{f.state(prepared).Result.Source}) {
		t.Fatalf("a result whose remaining was given to the ledger is remembered once saved: %q", got)
	}
}

// A result that was repaired by ref while the receipt held a later one is handled, but its remaining was never given to the ledger:
// it is not remembered as having left a row, and the row is written when the result is recorded in full later.
func TestWorkCoverageARepairByRefDoesNotRememberARemainingRowItNeverWrote(t *testing.T) {
	f := newCoverageFlow(t)
	prepared := f.prepare()
	older := workCompletion{Changed: "Older audit.", Remaining: "alpha left", Evidence: []string{"new1"}}
	olderSource := f.waits(prepared, older)
	f.record(prepared, withRemaining(workCompletion{Changed: "Newer audit.", Evidence: []string{"new1"}}, "beta left"))
	newerSource := f.state(prepared).Result.Source
	f.repairs(prepared, olderSource)
	if got := f.state(prepared).CoverageLeft; !slices.Equal(got, []string{newerSource}) {
		t.Fatalf("the repaired older result wrote no row of its remaining, so only the later result is remembered: %q", got)
	}
	f.record(prepared, older)
	if got := f.state(prepared).CoverageLeft; !slices.Equal(got, []string{olderSource, newerSource}) {
		t.Fatalf("recorded in full, the older result gives its row and is remembered: %q", got)
	}
	if got := openAreasOf(t, f.prepare()); !slices.Equal(got, []string{"alpha left", "beta left"}) {
		t.Fatalf("the older remaining opens beside the later one: %q", got)
	}
}

// A ledger that could not be written does not make the work remember that its remaining was given: the same record sent again
// writes the row, and only then is the source remembered.
func TestWorkCoverageALedgerThatCouldNotBeWrittenDoesNotRememberItsRemainingRow(t *testing.T) {
	f := newCoverageFlow(t)
	prepared := f.prepare()
	folder, aside := filepath.Join(f.b.Root, "records", "coverage"), filepath.Join(f.b.Root, "records", "coverage-aside")
	if err := os.MkdirAll(folder, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(folder, aside); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(folder, []byte("a file where the folder of the ledger goes"), 0o600); err != nil {
		t.Fatal(err)
	}
	result := withRemaining(workCompletion{Changed: "Read the import path.", Evidence: []string{"new1"}}, "The totals were not read.")
	if failed := f.record(prepared, result); failed["complete"] == true {
		t.Fatalf("the record names the ledger it could not write: %v", failed)
	}
	if got := f.state(prepared).CoverageLeft; len(got) != 0 {
		t.Fatalf("a remaining that could not be written is not remembered as given: %q", got)
	}
	if err := os.Remove(folder); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(aside, folder); err != nil {
		t.Fatal(err)
	}
	if again := f.record(prepared, result); again["complete"] != true {
		t.Fatalf("the same record sent again is complete: %v", again)
	}
	if got := openAreasOf(t, f.prepare()); !slices.Equal(got, []string{"The totals were not read."}) {
		t.Fatalf("the remaining is written by the record sent again: %q", got)
	}
	if got := f.state(prepared).CoverageLeft; !slices.Equal(got, []string{f.state(prepared).Result.Source}) {
		t.Fatalf("remembered once the ledger was written: %q", got)
	}
}

// recordWorkCoverage tells the record that the state holds something unsaved when it remembers that a remaining row was given, also
// when the source was remembered as handled before (a result repaired by ref, then recorded in full), and not again for a source it
// remembers.
func TestWorkCoverageMarksTheStateUnsavedWhenOnlyTheRemainingRowIsRemembered(t *testing.T) {
	f := newCoverageFlow(t)
	prepared := f.prepare()
	f.record(prepared, withRemaining(coverageResult(), "alpha left"))
	w := f.state(prepared)
	source, session := w.Result.Source, f.session(prepared)
	w.CoverageLeft = nil // handled is remembered, the row of the remaining is not
	marks, reply := 0, workResult{}
	add := func(item workItem) { t.Fatalf("nothing is refused: %v", item) }
	unsaved := func() { marks++ }
	recordWorkCoverage(f.b.Root, f.b, w, &workCompletion{Ref: source}, session, &reply, add, unsaved)
	if marks != 1 || !slices.Equal(w.CoverageLeft, []string{source}) || !slices.Equal(w.CoverageHandled, []string{source}) {
		t.Fatalf("the remaining row is remembered and the record is told once: %d, %q", marks, w.CoverageLeft)
	}
	recordWorkCoverage(f.b.Root, f.b, w, &workCompletion{Ref: source}, session, &reply, add, unsaved)
	if marks != 1 {
		t.Fatalf("a source that is remembered is not told again: %d", marks)
	}
}
