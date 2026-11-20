package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	coverageScope = "/synthetic/project"
	coverageA     = "00000000000000a1"
	coverageB     = "00000000000000b2"
	coverageC     = "00000000000000c3"
	coverageD     = "00000000000000d4"
)

func openAreas(t *testing.T, s Store) []string {
	t.Helper()
	rows, err := s.OpenCoverage(coverageScope)
	if err != nil {
		t.Fatal(err)
	}
	areas := []string{}
	for _, r := range rows {
		areas = append(areas, r.Area)
	}
	return areas
}

func ledgerLines(t *testing.T, s Store) int {
	t.Helper()
	data, err := os.ReadFile(s.coveragePath(coverageScope))
	if os.IsNotExist(err) {
		return 0
	}
	if err != nil {
		t.Fatal(err)
	}
	return strings.Count(string(data), "\n")
}

// An area is open while its latest line is an open one; a result that closes it as reported, clear or excluded takes it off the
// open list, and the other open areas stay. The open list is newest first.
func TestCoverageLedgerClosesAnOpenAreaAndKeepsTheOthers(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	if _, err := s.RecordCoverage(coverageScope, coverageA, []CoverageRow{{Area: "export filters", Outcome: CoverageOpen, Reason: "ran out of time"}, {Area: "export totals", Outcome: CoverageOpen}}, CoverageHandover{}); err != nil {
		t.Fatal(err)
	}
	if got := openAreas(t, s); strings.Join(got, "|") != "export totals|export filters" {
		t.Fatalf("both areas are open, the newest first: %q", got)
	}
	for _, closing := range []string{CoverageClear, CoverageReported, CoverageExcluded} {
		s := Store{Dir: t.TempDir()}
		must := func(source string, rows ...CoverageRow) {
			t.Helper()
			if _, err := s.RecordCoverage(coverageScope, source, rows, CoverageHandover{}); err != nil {
				t.Fatal(err)
			}
		}
		must(coverageA, CoverageRow{Area: "export filters", Outcome: CoverageOpen}, CoverageRow{Area: "export totals", Outcome: CoverageOpen})
		must(coverageB, CoverageRow{Area: "  export   filters ", Outcome: closing, Reason: "why"}) // the same area once white space is normalized
		if got := openAreas(t, s); strings.Join(got, "|") != "export totals" {
			t.Fatalf("%s closes its area and only that one: %q", closing, got)
		}
		must(coverageC, CoverageRow{Area: "export filters", Outcome: CoverageOpen}) // a later result may open it again
		if got := openAreas(t, s); strings.Join(got, "|") != "export filters|export totals" {
			t.Fatalf("a later open row reopens the area, as the newest: %q", got)
		}
	}
}

// A closing row is written only for an area the ledger holds open: a clear row for an area that was never open stays in the
// result's own saved source and adds nothing to the ledger.
func TestCoverageLedgerDoesNotWriteAClosingRowForAnAreaThatWasNeverOpen(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	if _, err := s.RecordCoverage(coverageScope, coverageA, []CoverageRow{{Area: "never opened", Outcome: CoverageClear}, {Area: "also never opened", Outcome: CoverageExcluded, Reason: "out of scope"}, {Area: "and this", Outcome: CoverageReported}}, CoverageHandover{}); err != nil {
		t.Fatal(err)
	}
	if n := ledgerLines(t, s); n != 0 {
		t.Fatalf("rows for areas that were never open were written: %d lines", n)
	}
	if got := openAreas(t, s); len(got) != 0 {
		t.Fatalf("nothing is open: %q", got)
	}
}

// The same record sent again adds no line, and an old result sent again cannot reopen an area a later result closed.
func TestCoverageLedgerSentAgainAddsNothing(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	rows := []CoverageRow{{Area: "export filters", Outcome: CoverageOpen}}
	for i := range 3 {
		if _, err := s.RecordCoverage(coverageScope, coverageA, rows, CoverageHandover{}); err != nil {
			t.Fatal(err)
		}
		if n := ledgerLines(t, s); n != 1 {
			t.Fatalf("send %d: the ledger has %d lines, want 1", i+1, n)
		}
	}
	if _, err := s.RecordCoverage(coverageScope, coverageB, []CoverageRow{{Area: "export filters", Outcome: CoverageClear}}, CoverageHandover{}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RecordCoverage(coverageScope, coverageB, []CoverageRow{{Area: "export filters", Outcome: CoverageClear}}, CoverageHandover{}); err != nil {
		t.Fatal(err)
	}
	if n := ledgerLines(t, s); n != 2 {
		t.Fatalf("the closing record sent again added a line: %d lines, want 2", n)
	}
	if _, err := s.RecordCoverage(coverageScope, coverageA, rows, CoverageHandover{}); err != nil { // the first result, sent again after the area was closed
		t.Fatal(err)
	}
	if got := openAreas(t, s); len(got) != 0 {
		t.Fatalf("an old result sent again reopened an area a later result closed: %q", got)
	}
	if n := ledgerLines(t, s); n != 2 {
		t.Fatalf("the old result sent again added a line: %d lines, want 2", n)
	}
}

// The reader keeps only what the writer would write (an allow-list): a line with an outcome it does not know, an area that is
// empty, not normalized or too long, a source that is no id, a line that is no JSON, and a line over its limit are dropped, and the
// valid lines around them are read.
func TestCoverageLedgerReaderDropsWhatItDoesNotKnow(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	if _, err := s.RecordCoverage(coverageScope, coverageA, []CoverageRow{{Area: "before the junk", Outcome: CoverageOpen}}, CoverageHandover{}); err != nil {
		t.Fatal(err)
	}
	line := func(area, outcome, source string) string {
		return `{"at":"2026-11-20T00:00:00Z","area":"` + area + `","outcome":"` + outcome + `","source":"` + source + `"}` + "\n"
	}
	junk := line("unknown outcome", "done", coverageB) +
		line("closed by a word it does not know", "open", coverageB) + line("closed by a word it does not know", "resolved", coverageB) +
		line("", "open", coverageB) +
		line(" not  normalized", "open", coverageB) +
		line(strings.Repeat("a", CoverageAreaMax+1), "open", coverageB) +
		line("source is no id", "open", "xyz") +
		`{"at":"2026-11-20T00:00:00Z","area":"no source","outcome":"open"}` + "\n" +
		`{"at":"0001-01-01T00:00:00Z","area":"no time","outcome":"open","source":"` + coverageB + `"}` + "\n" +
		"this is not json\n" +
		`{"at":"2026-11-20T00:00:00Z","area":"long reason","outcome":"open","reason":"` + strings.Repeat("r", CoverageReasonMax+1) + `","source":"` + coverageB + `"}` + "\n" +
		`{"at":"2026-11-20T00:00:00Z","area":"valid fields, line over its limit","outcome":"open","source":"` + coverageB + `","pad":"` + strings.Repeat("p", coverageLineMax) + `"}` + "\n" +
		`{"at":"2026-11-20T00:00:00Z","area":"cut before its newline","outc`
	f, err := os.OpenFile(s.coveragePath(coverageScope), os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(junk); err != nil {
		t.Fatal(err)
	}
	f.Close()
	got := openAreas(t, s)
	if strings.Join(got, "|") != "closed by a word it does not know|before the junk" {
		t.Fatalf("the reader keeps the valid lines and drops the others (an unknown outcome closes nothing): %q", got)
	}
	// the writer still appends after a tail without a newline, and reads what it wrote
	if _, err := s.RecordCoverage(coverageScope, coverageC, []CoverageRow{{Area: "after the junk", Outcome: CoverageOpen}}, CoverageHandover{}); err != nil {
		t.Fatal(err)
	}
	if got := openAreas(t, s); got[0] != "after the junk" || len(got) != 3 {
		t.Fatalf("a row written after the junk is read: %q", got)
	}
}

// A row the ledger cannot keep refuses the whole call before any line is written, and says what a row needs.
func TestCoverageLedgerRefusesAnInvalidRowWithoutWritingTheOthers(t *testing.T) {
	for name, bad := range map[string]CoverageRow{
		"unknown outcome": {Area: "x", Outcome: "done"},
		"blank area":      {Area: "  \t ", Outcome: CoverageOpen},
		"control":         {Area: "a\nb", Outcome: CoverageOpen},
		"long area":       {Area: strings.Repeat("a", CoverageAreaMax+1), Outcome: CoverageOpen},
		"long reason":     {Area: "x", Outcome: CoverageOpen, Reason: strings.Repeat("r", CoverageReasonMax+1)},
		"unknown origin":  {Area: "x", Outcome: CoverageOpen, Origin: "agent"},
		"upper case hex":  {Area: "x", Outcome: CoverageOpen, Origin: CoverageFromRemaining, Session: "0123456789ABCDEF"},
		"short session":   {Area: "x", Outcome: CoverageOpen, Origin: CoverageFromRemaining, Session: "0123456789abcde"},
	} {
		s := Store{Dir: t.TempDir()}
		_, err := s.RecordCoverage(coverageScope, coverageA, []CoverageRow{{Area: "valid", Outcome: CoverageOpen}, bad}, CoverageHandover{})
		if err == nil || !strings.Contains(err.Error(), "needs an area") {
			t.Fatalf("%s: the call is refused with what a row needs: %v", name, err)
		}
		if n := ledgerLines(t, s); n != 0 {
			t.Fatalf("%s: a refused call wrote %d lines", name, n)
		}
	}
	if _, err := (Store{Dir: t.TempDir()}).RecordCoverage(coverageScope, "not-an-id", []CoverageRow{{Area: "x", Outcome: CoverageOpen}}, CoverageHandover{}); err == nil {
		t.Fatal("a source that is no id is accepted")
	}
}

// The ledger is the project's: the file is named by the store name of the scope, in records/coverage.
func TestCoverageLedgerLivesInRecordsCoverage(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	if _, err := s.RecordCoverage(coverageScope, coverageA, []CoverageRow{{Area: "x", Outcome: CoverageOpen}}, CoverageHandover{}); err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(s.Dir, "records", "coverage", StoreName(s.Dir, coverageScope)+".jsonl"); s.coveragePath(coverageScope) != want {
		t.Fatalf("path %s, want %s", s.coveragePath(coverageScope), want)
	}
	if n := ledgerLines(t, s); n != 1 {
		t.Fatalf("%d lines", n)
	}
}

// Only the last 4MB of the ledger is read, and a line the tail starts inside is not taken: here the tail begins exactly where a
// valid row begins (after other bytes of its line), and that row, whose line is cut, is not one of the open areas.
func TestCoverageLedgerReadsItsTailWithoutTheLineItStartsIn(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	row := func(area string) string {
		return `{"at":"2026-11-20T00:00:00Z","area":"` + area + `","outcome":"open","source":"` + coverageB + `"}` + "\n"
	}
	cut, last := row("cut by the tail"), row("inside the tail")
	filler := strings.Repeat("x", feedbackReadMax-len(cut)-len(last)-1) + "\n" // one line over the limit of a line
	if err := os.MkdirAll(filepath.Dir(s.coveragePath(coverageScope)), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.coveragePath(coverageScope), []byte(row("before the tail")+"junk"+cut+filler+last), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := openAreas(t, s); strings.Join(got, "|") != "inside the tail" {
		t.Fatalf("the ledger is read from its last 4MB, from the first whole line in it: %q", got)
	}
}

const (
	sessionX = "0123456789abcdef"
	sessionY = "fedcba9876543210"
)

func remainingRow(area, session string) CoverageRow {
	return CoverageRow{Area: area, Outcome: CoverageOpen, Origin: CoverageFromRemaining, Session: session}
}

func record(t *testing.T, s Store, source string, rows []CoverageRow, h CoverageHandover) []string {
	t.Helper()
	handed, err := s.RecordCoverage(coverageScope, source, rows, h)
	if err != nil {
		t.Fatal(err)
	}
	return handed
}

// The session of a row is the first 16 hex digits of the sha256 of the session id: the digest of "abc" is the published test vector.
func TestCoverageSessionIsTheFirstSixteenHexDigitsOfTheSha256(t *testing.T) {
	if got := CoverageSession("abc"); got != "ba7816bf8f01cfea" {
		t.Fatalf("the session of abc is %q", got)
	}
	if !coverageSession.MatchString(CoverageSession("another session")) {
		t.Fatal("a session the store makes is a session the reader keeps")
	}
}

// The reader keeps a row whose origin is none or remaining and whose session is none or 16 lower case hex digits, and drops every
// other form.
func TestCoverageLedgerReaderKeepsOnlyKnownOriginsAndSessions(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	line := func(area, extra string) string {
		return `{"at":"2026-11-20T00:00:00Z","area":"` + area + `","outcome":"open",` + extra + `"source":"` + coverageA + `"}` + "\n"
	}
	file := line("no origin, no session", "") +
		line("no origin, a session", `"session":"`+sessionX+`",`) +
		line("remaining, a session", `"origin":"remaining","session":"`+sessionX+`",`) +
		line("remaining, no session", `"origin":"remaining",`) +
		line("unknown origin", `"origin":"agent","session":"`+sessionX+`",`) +
		line("origin in upper case", `"origin":"REMAINING",`) +
		line("session in upper case", `"origin":"remaining","session":"0123456789ABCDEF",`) +
		line("session of 15 digits", `"origin":"remaining","session":"0123456789abcde",`) +
		line("session of 17 digits", `"origin":"remaining","session":"0123456789abcdef0",`) +
		line("session that is not hex", `"origin":"remaining","session":"0123456789abcdeg",`) +
		line("session that is a number", `"origin":"remaining","session":123,`)
	if err := os.MkdirAll(filepath.Dir(s.coveragePath(coverageScope)), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.coveragePath(coverageScope), []byte(file), 0o600); err != nil {
		t.Fatal(err)
	}
	got := openAreas(t, s)
	if want := "remaining, no session|remaining, a session|no origin, a session|no origin, no session"; strings.Join(got, "|") != want {
		t.Fatalf("the reader keeps the rows of known origins and sessions: %q, want %q", got, want)
	}
}

// A result takes over the open rows made from remaining that its session wrote and the ones its preparation listed, and no other:
// not a row of another session that it was not handed, not a row the agent wrote (it has no origin, even when it names a
// session), and not a row that is closed. The closing rows are excluded rows that name the result, newest area first.
func TestCoverageLedgerHandoverTakesOverTheRowsOfItsSessionAndTheListedAreas(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	record(t, s, coverageA, []CoverageRow{remainingRow("x one", sessionX), remainingRow("x two", sessionX), {Area: "agent row", Outcome: CoverageOpen, Session: sessionX}, remainingRow("x closed", sessionX)}, CoverageHandover{})
	record(t, s, coverageB, []CoverageRow{{Area: "x closed", Outcome: CoverageClear}, remainingRow("y one", sessionY), remainingRow("y two", sessionY), remainingRow("y three", sessionY)}, CoverageHandover{})
	handed := record(t, s, coverageC, nil, CoverageHandover{Session: sessionX, Shown: []string{"y two", "agent row", "never listed", "x closed"}})
	if strings.Join(handed, "|") != "y two|x two|x one" {
		t.Fatalf("the areas handed over, newest first: %q", handed)
	}
	if got := openAreas(t, s); strings.Join(got, "|") != "y three|y one|agent row" {
		t.Fatalf("the rows it was not handed stay open: %q", got)
	}
	data, err := os.ReadFile(s.coveragePath(coverageScope))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	for i, area := range []string{"y two", "x two", "x one"} {
		var row map[string]any
		if err := json.Unmarshal([]byte(lines[len(lines)-3+i]), &row); err != nil {
			t.Fatal(err)
		}
		if row["area"] != area || row["outcome"] != "excluded" || row["source"] != coverageC || row["reason"] != "handed over to the work that recorded "+coverageC || row["origin"] != nil || row["session"] != nil {
			t.Fatalf("the closing line of %q: %v", area, row)
		}
	}
	// a result with no session and no listed area takes nothing over
	if handed := record(t, s, coverageD, nil, CoverageHandover{}); len(handed) != 0 || len(openAreas(t, s)) != 3 {
		t.Fatalf("no session and no listed area: %q", handed)
	}
}

// A result does not close the area of a row it writes itself: the same left-over said again is one open area, now from the newer
// result and with no closing line; and a result that closes an area by its own row closes it as that row says, once.
func TestCoverageLedgerHandoverKeepsTheAreaTheResultWritesItself(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	record(t, s, coverageA, []CoverageRow{remainingRow("left one", sessionX), remainingRow("left two", sessionX)}, CoverageHandover{})
	handed := record(t, s, coverageB, []CoverageRow{remainingRow("left one", sessionX)}, CoverageHandover{Session: sessionX, Shown: []string{"left one"}})
	rows, err := s.OpenCoverage(coverageScope)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Area != "left one" || rows[0].Source != coverageB || strings.Join(handed, "|") != "left two" || ledgerLines(t, s) != 4 {
		t.Fatalf("the area said again stays open from the newer result and the other one is closed: %+v %q (%d lines)", rows, handed, ledgerLines(t, s))
	}
	handed = record(t, s, coverageC, []CoverageRow{{Area: "left one", Outcome: CoverageClear}}, CoverageHandover{Session: sessionX, Shown: []string{"left one"}})
	if len(handed) != 0 || len(openAreas(t, s)) != 0 || ledgerLines(t, s) != 5 {
		t.Fatalf("a row of the result that closes the area is its only closing line: %q (%d lines)", handed, ledgerLines(t, s))
	}
}

// A result the ledger already has lines of takes nothing over when it is sent again: it would close what a later result of the same
// session opened since.
func TestCoverageLedgerHandoverOfAResultAlreadyRecordedChangesNothing(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	h := CoverageHandover{Session: sessionX}
	record(t, s, coverageA, []CoverageRow{remainingRow("old", sessionX)}, h)
	if handed := record(t, s, coverageB, []CoverageRow{remainingRow("new", sessionX)}, h); strings.Join(handed, "|") != "old" {
		t.Fatalf("the later result closes the earlier one: %q", handed)
	}
	if handed := record(t, s, coverageA, []CoverageRow{remainingRow("old", sessionX)}, h); len(handed) != 0 {
		t.Fatalf("the earlier result sent again closes nothing: %q", handed)
	}
	if got := openAreas(t, s); strings.Join(got, "|") != "new" || ledgerLines(t, s) != 3 {
		t.Fatalf("the later result's area is still open and no line was added: %q (%d lines)", got, ledgerLines(t, s))
	}
}

// A row made from remaining is not written over an open row the agent wrote for the same area: the agent's row stays the latest
// line of the area, so it is not a row that a handover takes over, and only a later result that records the area closes it. A row
// made from remaining over an area that is closed, or over another remaining row, is written as before.
func TestCoverageLedgerRemainingDoesNotReplaceAnAgentsOpenRow(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	h := CoverageHandover{Session: sessionX, Shown: []string{"export filters"}}
	record(t, s, coverageA, []CoverageRow{{Area: "export filters", Outcome: CoverageOpen}}, CoverageHandover{})
	record(t, s, coverageB, []CoverageRow{remainingRow("export filters", sessionX), remainingRow("export totals", sessionX)}, h)
	rows, err := s.OpenCoverage(coverageScope)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].Area != "export totals" || rows[0].Origin != CoverageFromRemaining || rows[1].Area != "export filters" || rows[1].Origin != "" || rows[1].Source != coverageA || ledgerLines(t, s) != 2 {
		t.Fatalf("the agent's row is still the latest line of its area and the other remaining is written: %+v (%d lines)", rows, ledgerLines(t, s))
	}
	if handed := record(t, s, coverageC, nil, h); strings.Join(handed, "|") != "export totals" {
		t.Fatalf("only the row made from remaining is taken over: %q", handed)
	}
	if got := openAreas(t, s); strings.Join(got, "|") != "export filters" {
		t.Fatalf("the agent's row stays open: %q", got)
	}
	// a result that records the area itself closes it, and a remaining may open it again afterwards
	record(t, s, coverageD, []CoverageRow{{Area: "export filters", Outcome: CoverageClear}}, CoverageHandover{})
	record(t, s, "00000000000000e5", []CoverageRow{remainingRow("export filters", sessionX)}, CoverageHandover{})
	if rows, _ := s.OpenCoverage(coverageScope); len(rows) != 1 || rows[0].Origin != CoverageFromRemaining {
		t.Fatalf("a closed area is opened again by a remaining: %+v", rows)
	}
}
