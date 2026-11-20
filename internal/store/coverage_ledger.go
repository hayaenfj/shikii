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

// readCoverage reads the valid lines of the ledger, oldest first: the last 4MB, without a line cut before its newline. A broken
// line, a line over its limit, an unknown outcome and an empty or unnormalized area are dropped.
func (s Store) readCoverage(path string) ([]CoverageRow, error) {
	f, start, err := s.openRecentHistory(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, feedbackReadMax))
	if err != nil {
		return nil, err
	}
	if start > 0 { // the tail starts inside a line
		cut := bytes.IndexByte(data, '\n')
		if cut < 0 {
			return nil, nil
		}
		data = data[cut+1:]
	}
	var rows []CoverageRow
	for {
		end := bytes.IndexByte(data, '\n')
		if end < 0 {
			return rows, nil // what follows the last newline is a line cut before its end
		}
		line := data[:end+1]
		data = data[end+1:]
		var row CoverageRow
		if len(line) > coverageLineMax || json.Unmarshal(line, &row) != nil || !validCoverageRow(row) {
			continue
		}
		rows = append(rows, row)
	}
}

// OpenCoverage returns the areas the ledger holds open, newest first: the latest line of an area is an open one.
func (s Store) OpenCoverage(scope string) ([]CoverageRow, error) {
	rows, err := s.readCoverage(s.coveragePath(scope))
	if err != nil {
		return nil, err
	}
	latest := map[string]int{}
	for i, r := range rows {
		latest[r.Area] = i
	}
	open := []CoverageRow{}
	for i := len(rows) - 1; i >= 0; i-- {
		if rows[i].Outcome == CoverageOpen && latest[rows[i].Area] == i {
			open = append(open, rows[i])
		}
	}
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
func (s Store) RecordCoverage(scope, source string, rows []CoverageRow, handover CoverageHandover) ([]string, error) {
	if !textTrace.MatchString(source) {
		return nil, errors.New("a coverage row needs the id of the saved result it came from")
	}
	path := s.coveragePath(scope)
	at := time.Now().UTC()
	var handed []string
	err := s.withJSONLAppendLock(path, func() error {
		handed = nil
		ledger, err := s.readCoverage(path)
		if err != nil {
			return err
		}
		latest := map[string]CoverageRow{}
		newest := map[string]int{} // the index in ledger of the latest line of an area
		wrote := map[string]bool{} // the areas this result has a line for already
		for i, r := range ledger {
			latest[r.Area], newest[r.Area] = r, i
			if r.Source == source {
				wrote[r.Area] = true
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
			latest[row.Area], wrote[row.Area] = row, true
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
			r := ledger[i]
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
			return nil
		}
		return s.appendJSONLBytesLocked(path, out)
	})
	if err != nil {
		return nil, err
	}
	return handed, nil
}
