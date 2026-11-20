package main

import (
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
