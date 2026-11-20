package store

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// The coverage ledger is a project's append-only record of what work left unexamined. A result may list the areas it examined
// (reported, clear, excluded) and the areas it did not (open); an open area stays on the ledger, and later preparations hand it
// over, until a later result writes the same area as reported, clear or excluded.
//
// records/coverage/<store name>.jsonl, one line per row: {at, area, outcome, reason?, origin?, session?, source}. source is the id of
// the saved result the row came from. An area is open when its latest line is an open one.
//
// A row is written by one of two origins. A row the agent wrote in result.coverage has no origin. A row made from the text of
// result.remaining (CoverageFromRemaining) carries the session of the work that wrote it, and is closed without a later result naming
// the same area: the work that was handed it closes it when it records a result, and so does the next result of the work's own
// session (RecordCoverage, CoverageHandover).
const (
	CoverageOpen     = "open"     // not examined; needs follow-up
	CoverageReported = "reported" // examined, a problem was found and reported
	CoverageClear    = "clear"    // examined, no problem
	CoverageExcluded = "excluded" // left out on purpose, with a reason

	CoverageFromRemaining = "remaining" // origin of a row made from result.remaining; a row the agent wrote has no origin

	CoverageAreaMax   = 160 // bytes of an area, before and after it is normalized
	CoverageReasonMax = 300 // bytes of a reason

	// coverageLineMax is the longest line (with its newline) a row may take: the reader skips longer ones and the writer refuses them.
	// An area and a reason at their limits, each byte escaped to six, are still under it.
	coverageLineMax = 4 << 10

	// coverageCarryZone is the slack between what the reader takes (the last feedbackReadMax bytes) and where RecordCoverage keeps
	// the open rows it carries forward: after a call that writes, every open row sits in the newest feedbackReadMax-coverageCarryZone
	// bytes of the file. The zone is what lines appended after that call (by a call that carries nothing, or by a version of the
	// program that does not carry) may add before the oldest open row would leave the bytes the reader takes.
	coverageCarryZone = 1 << 20
)

// CoverageRow is one line of the ledger. The caller of RecordCoverage fills Area, Outcome, Reason, Origin and Session; the store
// fills At and Source. Session is CoverageSession of the session of the work that wrote the row: it is kept for a row made from
// remaining only.
type CoverageRow struct {
	At      time.Time `json:"at"`
	Area    string    `json:"area"`
	Outcome string    `json:"outcome"`
	Reason  string    `json:"reason,omitempty"`
	Origin  string    `json:"origin,omitempty"`
	Session string    `json:"session,omitempty"`
	Source  string    `json:"source"`
}

// coverageSession is the form of a row's session: 16 lower case hex digits.
var coverageSession = regexp.MustCompile(`^[0-9a-f]{16}$`)

// CoverageSession is what a row keeps of the session of its work: the first 16 hex digits of the sha256 of the session id.
func CoverageSession(sessionID string) string {
	sum := sha256.Sum256([]byte(sessionID))
	return hex.EncodeToString(sum[:8])
}

// CoverageHandover tells RecordCoverage which open rows made from remaining the recorded result takes over. A row is taken over
// when it was written by the work's own session (remaining is what is left now, so a later result of the session replaces it), or
// when its area is one the work's preparation listed (a row of another session that this work was handed).
type CoverageHandover struct {
	Session string   // CoverageSession of the work's session; empty takes over no row by session
	Shown   []string // the areas the work's preparation listed
}

func (s Store) coveragePath(scope string) string {
	return s.recordPath("coverage", ".jsonl", scope)
}

// CoverageOutcome reports whether outcome is one of the four the ledger knows. It is an allow-list: the reader drops every other value.
func CoverageOutcome(outcome string) bool {
	switch outcome {
	case CoverageOpen, CoverageReported, CoverageClear, CoverageExcluded:
		return true
	}
	return false
}

// NormalizeCoverageArea returns the area without white space at its ends and with each run of white space inside it as one space,
// and whether the result is an area the ledger keeps: 1 to CoverageAreaMax bytes of UTF-8 on one line (no control character). Two
// areas are the same area when their normalized forms are equal.
func NormalizeCoverageArea(area string) (string, bool) {
	if !utf8.ValidString(area) || strings.IndexFunc(area, unicode.IsControl) >= 0 {
		return "", false
	}
	normalized := strings.Join(strings.Fields(area), " ")
	return normalized, normalized != "" && len(normalized) <= CoverageAreaMax
}

// validCoverageRow is what the reader and the writer both accept: a known outcome, an area already in its normalized form, a reason
// within its bound, an origin that is none or remaining, a session that is none or 16 hex digits, and the id of a source.
func validCoverageRow(r CoverageRow) bool {
	area, ok := NormalizeCoverageArea(r.Area)
	return ok && area == r.Area && CoverageOutcome(r.Outcome) && utf8.ValidString(r.Reason) && len(r.Reason) <= CoverageReasonMax &&
		(r.Origin == "" || r.Origin == CoverageFromRemaining) && (r.Session == "" || coverageSession.MatchString(r.Session)) &&
		textTrace.MatchString(r.Source) && !r.At.IsZero()
}

// coverageLine is a valid line of the ledger and where it begins in the file.
type coverageLine struct {
	row    CoverageRow
	offset int64
}

// readCoverage reads the valid lines of the ledger, oldest first: the last 4MB, without a line cut before its newline, and the size
// of the file. A broken line, a line over its limit, an unknown outcome and an empty or unnormalized area are dropped. When the
// last 4MB begins inside a line, that line is not taken; when it begins exactly at the head of a line (the byte before it is a
// newline), the line is whole and is taken.
func (s Store) readCoverage(path string) ([]coverageLine, int64, error) {
	f, start, err := s.openRecentHistory(path)
	if os.IsNotExist(err) {
		return nil, 0, nil
	}
	if err != nil {
		return nil, 0, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, feedbackReadMax))
	if err != nil {
		return nil, 0, err
	}
	size := start + int64(len(data))
	offset := start
	if start > 0 {
		var before [1]byte
		if _, err := f.ReadAt(before[:], start-1); err != nil {
			return nil, 0, err
		}
		if before[0] != '\n' { // the tail starts inside a line
			cut := bytes.IndexByte(data, '\n')
			if cut < 0 {
				return nil, size, nil
			}
			data, offset = data[cut+1:], offset+int64(cut)+1
		}
	}
	var lines []coverageLine
	for {
		end := bytes.IndexByte(data, '\n')
		if end < 0 {
			return lines, size, nil // what follows the last newline is a line cut before its end
		}
		line := data[:end+1]
		at := offset
		data, offset = data[end+1:], offset+int64(end)+1
		var row CoverageRow
		if len(line) > coverageLineMax || json.Unmarshal(line, &row) != nil || !validCoverageRow(row) {
			continue
		}
		lines = append(lines, coverageLine{row, at})
	}
}

// OpenCoverage returns the areas the ledger holds open, newest first: the latest line of an area is an open one. Newest is by the
// time a row was first written (At), the latest line first among rows of one time: a row that RecordCoverage carried forward keeps
// its time, and its line is later in the file than the lines of rows that were written after it.
func (s Store) OpenCoverage(scope string) ([]CoverageRow, error) {
	lines, _, err := s.readCoverage(s.coveragePath(scope))
	if err != nil {
		return nil, err
	}
	latest := map[string]int{}
	for i, l := range lines {
		latest[l.row.Area] = i
	}
	open := []CoverageRow{}
	for i := len(lines) - 1; i >= 0; i-- {
		if lines[i].row.Outcome == CoverageOpen && latest[lines[i].row.Area] == i {
			open = append(open, lines[i].row)
		}
	}
	slices.SortStableFunc(open, func(a, b CoverageRow) int { return b.At.Compare(a.At) }) // stable: the rows of one call share a time and keep their order
	return open, nil
}

// RecordCoverage writes what the ledger keeps of the result saved as source, and returns the areas it handed over. It writes a row
// whose outcome is open, and a row that closes an area the ledger holds open (reported, clear or excluded); a closing row for an area
// that is not open is not written (the result's own saved source holds it). A row is not written again when the ledger already has a
// line of the same area from the same source, so the same record sent again adds nothing, and an old result sent again cannot reopen
// an area a later result closed. A row made from remaining is not written over an open row the agent wrote (no origin) for the same
// area: that row closes only when a later result records the area itself, and it would otherwise become a row that closes by itself.
//
// The result also closes the open rows made from remaining that it takes over (CoverageHandover), as excluded rows that name its
// source. It does not close the area of a row it writes itself (the same left-over said again stays one open area, now from this
// source), and it takes nothing over when the ledger already has a line from this source: a record sent again changes nothing, and
// an old result sent again does not close what a later one opened. The lock is held from the read to the append. A row that is not a
// valid row refuses the whole call before anything is written.
//
// The check of a record sent again sees only the lines this call reads, the newest 4MB: a result whose lines are further back would
// write its rows again. The caller that can remember which results it recorded does not send them twice (the work layer keeps the
// sources it handled).
//
// A call that appends a line of its own also writes the open rows that are about to leave the bytes the reader takes, again as
// they were read, before its own lines (coverageCarry): the reader takes only the last 4MB, and an open row never closes by itself,
// so a row that later lines pushed out of those 4MB would go off the open list although no result closed it. A call that writes
// nothing carries nothing, so a record sent again still adds no line. What stays limited: the open rows together must fit in what
// is read, and lines appended by a version of the program that does not carry are not carried by it.
func (s Store) RecordCoverage(scope, source string, rows []CoverageRow, handover CoverageHandover) ([]string, error) {
	if !textTrace.MatchString(source) {
		return nil, errors.New("a coverage row needs the id of the saved result it came from")
	}
	path := s.coveragePath(scope)
	at := time.Now().UTC()
	var handed []string
	err := s.withJSONLAppendLock(path, func() error {
		handed = nil
		ledger, size, err := s.readCoverage(path)
		if err != nil {
			return err
		}
		latest := map[string]CoverageRow{}
		newest := map[string]int{}    // the index in ledger of the latest line of an area
		wrote := map[string]bool{}    // the areas this result has a line for already
		appended := map[string]bool{} // the areas this call writes a line for
		for i, l := range ledger {
			latest[l.row.Area], newest[l.row.Area] = l.row, i
			if l.row.Source == source {
				wrote[l.row.Area] = true
			}
		}
		recorded := len(wrote) > 0
		var out []byte
		add := func(row CoverageRow) error {
			line, err := json.Marshal(row)
			if err != nil {
				return err
			}
			if len(line)+1 > coverageLineMax {
				return errors.New("a coverage row is too long to be read back")
			}
			out = append(append(out, line...), '\n')
			latest[row.Area], wrote[row.Area], appended[row.Area] = row, true, true
			return nil
		}
		for _, row := range rows {
			area, _ := NormalizeCoverageArea(row.Area)
			row = CoverageRow{At: at, Area: area, Outcome: row.Outcome, Reason: row.Reason, Origin: row.Origin, Session: row.Session, Source: source}
			if !validCoverageRow(row) {
				return errors.New("a coverage row needs an area (1-160 bytes on one line), an outcome of open, reported, clear or excluded, a reason of at most 300 bytes, an origin of none or remaining, and a session of none or 16 hex digits")
			}
			if wrote[area] {
				continue // the same record sent again; a result writes an area once, even when a later result has closed it since
			}
			prior, seen := latest[area]
			if row.Outcome != CoverageOpen && (!seen || prior.Outcome != CoverageOpen) {
				continue // nothing open to close
			}
			if row.Outcome == CoverageOpen && row.Origin == CoverageFromRemaining && seen && prior.Outcome == CoverageOpen && prior.Origin == "" {
				continue // the agent's own open row stays as it is: a remaining must not turn it into a row that closes by itself
			}
			if err := add(row); err != nil {
				return err
			}
		}
		for i := len(ledger) - 1; i >= 0 && !recorded; i-- { // newest first, as the open list is read
			r := ledger[i].row
			taken := handover.Session != "" && r.Session == handover.Session || slices.Contains(handover.Shown, r.Area)
			if !taken || newest[r.Area] != i || r.Outcome != CoverageOpen || r.Origin != CoverageFromRemaining || latest[r.Area].Source == source {
				continue // not taken over, closed since, not made from remaining, or written by this result itself just now
			}
			if err := add(CoverageRow{At: at, Area: r.Area, Outcome: CoverageExcluded, Reason: "handed over to the work that recorded " + source, Source: source}); err != nil {
				return err
			}
			handed = append(handed, r.Area)
		}
		if len(out) == 0 {
			return nil // a record sent again writes nothing, and carries nothing
		}
		carry := coverageCarry(ledger, newest, appended, size, len(out))
		return s.appendJSONLBytesLocked(path, append(carry, out...))
	})
	if err != nil {
		return nil, err
	}
	return handed, nil
}

// coverageCarry is the lines that RecordCoverage writes ahead of its own, so that every open row sits in the newest
// feedbackReadMax-coverageCarryZone bytes of the file once the call has appended. size is the size of the file before the append and
// added the bytes of the call's own lines. The rows that were read are walked oldest first, with where each line begins: a row
// before the first one that is already inside those bytes (counting what has been carried so far, which moves the point forward:
// one pass is enough) is carried when it is the latest line of its area, the area is open, and the call writes no line of its own
// for the area. A row is written as it was read, with its time, its source, its origin and its session, so it is still the row
// that its session's handover takes over and the open list still puts it where it was first written. A row that would be too long
// to be read back is left, and does not fail the record. The newline that the append puts before its lines when the file does not
// end in one is not counted: it is far inside coverageCarryZone.
func coverageCarry(ledger []coverageLine, newest map[string]int, appended map[string]bool, size int64, added int) []byte {
	keep := int64(feedbackReadMax - coverageCarryZone)
	var carry []byte
	for i, l := range ledger {
		if l.offset >= size+int64(added)+int64(len(carry))-keep {
			break // this row and every later one is inside the newest bytes
		}
		if newest[l.row.Area] != i || l.row.Outcome != CoverageOpen || appended[l.row.Area] {
			continue
		}
		line, err := json.Marshal(l.row)
		if err != nil || len(line)+1 > coverageLineMax {
			continue
		}
		carry = append(append(carry, line...), '\n')
	}
	return carry
}
