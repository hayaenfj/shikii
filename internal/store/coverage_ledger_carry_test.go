package store

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// coverageFiller is the source of the raw lines that fill a ledger in these tests: lines of one area that ends closed, so they never
// show in the open list and never need to be carried.
const coverageFiller = "00000000000000f6"

// coverageKeep is where a call that writes leaves every open row: inside the newest this many bytes of the file.
const coverageKeep = feedbackReadMax - coverageCarryZone

func ledgerSize(t *testing.T, s Store) int {
	t.Helper()
	info, err := os.Stat(s.coveragePath(coverageScope))
	if os.IsNotExist(err) {
		return 0
	}
	if err != nil {
		t.Fatal(err)
	}
	return int(info.Size())
}

// fillCoverageLedger appends raw valid lines, as other writers would have, until the file holds at least size bytes. The lines are
// pairs (open, then excluded) of one area, so the area is closed and a result that fills nothing of its own stays easy to read. It
// appends in one write: a test never loops thousands of RecordCoverage calls to make a big ledger.
func fillCoverageLedger(t *testing.T, s Store, size int) {
	t.Helper()
	have := ledgerSize(t, s)
	if have >= size {
		return
	}
	pair := `{"at":"2026-01-01T00:00:00Z","area":"filler","outcome":"open","source":"` + coverageFiller + `"}` + "\n" +
		`{"at":"2026-01-01T00:00:01Z","area":"filler","outcome":"excluded","reason":"filler","source":"` + coverageFiller + `"}` + "\n"
	path := s.coveragePath(coverageScope)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString(strings.Repeat(pair, (size-have)/len(pair)+1)); err != nil {
		t.Fatal(err)
	}
}

type ledgerLine struct {
	offset int
	text   string
}

// ledgerLinesOf is the lines of the file that are about one area, oldest first, with where each begins.
func ledgerLinesOf(t *testing.T, s Store, area string) []ledgerLine {
	t.Helper()
	data, err := os.ReadFile(s.coveragePath(coverageScope))
	if err != nil {
		t.Fatal(err)
	}
	needle := []byte(`"area":"` + area + `"`)
	var lines []ledgerLine
	for offset := 0; offset < len(data); {
		end := bytes.IndexByte(data[offset:], '\n')
		if end < 0 {
			break
		}
		if line := data[offset : offset+end]; bytes.Contains(line, needle) {
			lines = append(lines, ledgerLine{offset, string(line)})
		}
		offset += end + 1
	}
	return lines
}

func openCoverageRows(t *testing.T, s Store) []CoverageRow {
	t.Helper()
	rows, err := s.OpenCoverage(coverageScope)
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

// A reader takes only the last 4MB of the ledger, and an open row never closes by itself. Before, an open row that later lines
// pushed more than 4MB from the end went off the open list although no result had closed it (audit of 0.38.0, A-5). A call that
// writes now writes the open rows that are about to leave the newest feedbackReadMax-coverageCarryZone bytes again, as they were
// read, so every open row sits where the next reader sees it; a row that a result closed is never carried, and is not carried by the
// call that closes it.
func TestCoverageLedgerCarriesAnOpenRowForwardBeforeItLeavesTheRead(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	const x = "export filters"
	record(t, s, coverageA, []CoverageRow{{Area: x, Outcome: CoverageOpen, Reason: "ran out of time"}}, CoverageHandover{}) // the agent's own row: no origin, no session
	original := ledgerLinesOf(t, s, x)
	if len(original) != 1 {
		t.Fatalf("the agent's row is one line: %v", original)
	}
	// carried says that the latest line of x is the line as it was written, inside the newest bytes the call keeps open rows in
	carried := func(round string, lines int) {
		t.Helper()
		got := ledgerLinesOf(t, s, x)
		if len(got) != lines {
			t.Fatalf("%s: the ledger has %d lines of the area, want %d (a call that writes carries a row once, not every time)", round, len(got), lines)
		}
		last := got[len(got)-1]
		if last.offset < ledgerSize(t, s)-coverageKeep {
			t.Fatalf("%s: the latest line of the open area begins %d bytes from the end of the file, over the %d bytes a call that writes keeps it in", round, ledgerSize(t, s)-last.offset, coverageKeep)
		}
		if last.text != original[0].text {
			t.Fatalf("%s: the carried line is not the row as it was written:\n%s\n%s", round, last.text, original[0].text)
		}
	}
	// round 1: later lines of other writers fill 3MB, and one call that writes carries the row from the head of the file
	fillCoverageLedger(t, s, 3<<20+200<<10)
	record(t, s, coverageB, []CoverageRow{{Area: "other one", Outcome: CoverageOpen}}, CoverageHandover{})
	carried("round 1", 2)
	// round 2: the file passes 6MB, so without the carry the row would be 6MB from the end, and it is still listed
	fillCoverageLedger(t, s, ledgerSize(t, s)+3<<20+512<<10)
	record(t, s, coverageC, []CoverageRow{{Area: "other two", Outcome: CoverageOpen}}, CoverageHandover{})
	carried("round 2", 3)
	if size := ledgerSize(t, s); size <= feedbackReadMax+len(original[0].text) {
		t.Fatalf("the fixture is small (%d bytes): the first line is not more than 4MB from the end", size)
	}
	got := openAreas(t, s)
	if !slices.Contains(got, x) || len(got) != 3 || got[2] != x {
		t.Fatalf("the open row the file's 4MB tail no longer holds is listed, as the oldest: %q", got)
	}
	// a later result closes the area: it is not listed, and the call that closes it writes its one line and carries nothing of it
	fillCoverageLedger(t, s, ledgerSize(t, s)+3<<20+512<<10)
	record(t, s, coverageD, []CoverageRow{{Area: x, Outcome: CoverageClear}}, CoverageHandover{})
	if got := openAreas(t, s); slices.Contains(got, x) || len(got) != 2 {
		t.Fatalf("the area a result closed is still listed: %q", got)
	}
	if n := len(ledgerLinesOf(t, s, x)); n != 4 {
		t.Fatalf("the closing call wrote %d lines of the area, want its one closing line beside the 3 that were there", n-3)
	}
	// and a closed area is not carried by later calls, however far its lines are from the end
	fillCoverageLedger(t, s, ledgerSize(t, s)+3<<20+512<<10)
	record(t, s, "00000000000000e5", []CoverageRow{{Area: "other three", Outcome: CoverageOpen}}, CoverageHandover{})
	if n := len(ledgerLinesOf(t, s, x)); n != 4 {
		t.Fatalf("a closed area was carried: %d lines, want 4", n)
	}
	if got := openAreas(t, s); len(got) != 3 || slices.Contains(got, x) {
		t.Fatalf("the other open areas are carried and still listed: %q", got)
	}
}

// A carried row is the row as it was read: the origin and the session of a row made from remaining survive it, so the handover of
// the session that wrote it still takes it over, and an open row the agent wrote (no origin) is still no row a handover closes.
func TestCoverageLedgerCarriedRowsKeepTheirOriginAndSessionForTheHandover(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	record(t, s, coverageA, []CoverageRow{remainingRow("left over", sessionX), {Area: "agent row", Outcome: CoverageOpen}}, CoverageHandover{})
	for _, source := range []string{coverageB, coverageC} { // two rounds: the first lines are then 7MB from the end of the file
		fillCoverageLedger(t, s, ledgerSize(t, s)+3<<20+512<<10)
		record(t, s, source, []CoverageRow{{Area: "later " + source, Outcome: CoverageOpen}}, CoverageHandover{})
	}
	if got := openAreas(t, s); !slices.Contains(got, "left over") || !slices.Contains(got, "agent row") {
		t.Fatalf("both rows are carried and listed: %q", got)
	}
	handed := record(t, s, coverageD, nil, CoverageHandover{Session: sessionX, Shown: []string{"agent row"}})
	if strings.Join(handed, "|") != "left over" {
		t.Fatalf("the row made from remaining is still taken over by its session, and the agent's own row is not: %q", handed)
	}
	if got := openAreas(t, s); slices.Contains(got, "left over") || !slices.Contains(got, "agent row") {
		t.Fatalf("the handed over row is closed and the agent's row stays open: %q", got)
	}
}

// The same record sent again adds no line, also when an open row sits where a call that writes would carry it: a call that writes
// nothing carries nothing, or a record sent again would change the ledger.
func TestCoverageLedgerSentAgainAddsNothingEvenWhenAnOpenRowIsDueToBeCarried(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	const x = "export filters"
	record(t, s, coverageA, []CoverageRow{{Area: x, Outcome: CoverageOpen}}, CoverageHandover{})
	fillCoverageLedger(t, s, 3<<20+200<<10)
	again := []CoverageRow{{Area: "other one", Outcome: CoverageOpen}}
	record(t, s, coverageB, again, CoverageHandover{})
	fillCoverageLedger(t, s, ledgerSize(t, s)+3<<20+512<<10)
	last := ledgerLinesOf(t, s, x)
	if len(last) != 2 || last[1].offset >= ledgerSize(t, s)-coverageKeep {
		t.Fatalf("the fixture has the open row outside the newest %d bytes: %v", coverageKeep, last)
	}
	lines := ledgerLines(t, s)
	record(t, s, coverageB, again, CoverageHandover{})
	if n := ledgerLines(t, s); n != lines {
		t.Fatalf("the record sent again added %d lines", n-lines)
	}
	// the control: a result that writes a line of its own carries the row, in the same ledger
	record(t, s, coverageC, []CoverageRow{{Area: "other two", Outcome: CoverageOpen}}, CoverageHandover{})
	if n := ledgerLines(t, s); n < lines+3 {
		t.Fatalf("a result that writes carries the open rows that are due: %d lines, want at least %d (its own, and the 2 rows due)", n, lines+3)
	}
}

// The open list is newest first by when a row was first written, not by where its line is: a carried copy keeps its time and sits
// later in the file. The rows of one call share a time and keep the order of their lines.
func TestCoverageLedgerOpenListStaysNewestFirstWhenARowIsCarriedLater(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	const w, x, x2, y = "closed later", "old one", "old two", "written after the old ones"
	record(t, s, coverageA, []CoverageRow{{Area: w, Outcome: CoverageOpen}, {Area: x, Outcome: CoverageOpen}, {Area: x2, Outcome: CoverageOpen}}, CoverageHandover{})
	fillCoverageLedger(t, s, 1<<20)
	record(t, s, coverageB, []CoverageRow{{Area: y, Outcome: CoverageOpen}}, CoverageHandover{})
	if rows := openCoverageRows(t, s); len(rows) != 4 || !rows[0].At.After(rows[1].At) {
		t.Fatalf("the fixture: the row written later has a later time: %+v", rows)
	}
	fillCoverageLedger(t, s, 3<<20+512<<10)
	// this result writes only the line that closes w: the old rows are carried behind y, and y (inside the newest bytes) is not
	record(t, s, coverageC, []CoverageRow{{Area: w, Outcome: CoverageClear}}, CoverageHandover{})
	if one, two, later := ledgerLinesOf(t, s, x), ledgerLinesOf(t, s, x2), ledgerLinesOf(t, s, y); len(one) != 2 || len(two) != 2 || len(later) != 1 || one[1].offset < later[0].offset || two[1].offset < later[0].offset {
		t.Fatalf("the fixture: the old rows are carried to behind the row written after them (%v %v %v)", one, two, later)
	}
	if got := openAreas(t, s); strings.Join(got, "|") != strings.Join([]string{y, x2, x}, "|") {
		t.Fatalf("the open list is newest first by time, and by position among the rows of one call: %q", got)
	}
}

// When the last 4MB begins exactly at the head of a line (the byte before it is a newline), that line is whole and is read. The
// line a tail begins inside of is still dropped (TestCoverageLedgerReadsItsTailWithoutTheLineItStartsIn).
func TestCoverageLedgerReadsTheLineTheTailBeginsAtTheHeadOf(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	row := func(area string) string {
		return `{"at":"2026-11-20T00:00:00Z","area":"` + area + `","outcome":"open","source":"` + coverageB + `"}` + "\n"
	}
	head, first, last := row("before the tail"), row("at the head of the tail"), row("inside the tail")
	filler := strings.Repeat("x", feedbackReadMax-len(first)-len(last)-1) + "\n" // one line over the limit of a line
	if err := os.MkdirAll(filepath.Dir(s.coveragePath(coverageScope)), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.coveragePath(coverageScope), []byte(head+first+filler+last), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := openAreas(t, s); strings.Join(got, "|") != "inside the tail|at the head of the tail" {
		t.Fatalf("the tail begins at the head of a row, which is read; the row before the tail is not: %q", got)
	}
}
