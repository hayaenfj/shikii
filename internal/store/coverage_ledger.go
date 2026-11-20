package store

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// The coverage ledger is a project's append-only record of what work left unexamined. A result may list the areas it examined
// (reported, clear, excluded) and the areas it did not (open); an open area stays on the ledger, and later preparations hand it
// over, until a later result writes the same area as reported, clear or excluded.
//
// records/coverage/<store name>.jsonl, one line per row: {at, area, outcome, reason?, source}. source is the id of the saved result
// the row came from. An area is open when its latest line is an open one.
const (
	CoverageOpen     = "open"     // not examined; needs follow-up
	CoverageReported = "reported" // examined, a problem was found and reported
	CoverageClear    = "clear"    // examined, no problem
	CoverageExcluded = "excluded" // left out on purpose, with a reason

	CoverageAreaMax   = 160 // bytes of an area, before and after it is normalized
	CoverageReasonMax = 300 // bytes of a reason

	// coverageLineMax is the longest line (with its newline) a row may take: the reader skips longer ones and the writer refuses them.
	// An area and a reason at their limits, each byte escaped to six, are still under it.
	coverageLineMax = 4 << 10
)

// CoverageRow is one line of the ledger. The caller of RecordCoverage fills Area, Outcome and Reason; the store fills At and Source.
type CoverageRow struct {
	At      time.Time `json:"at"`
	Area    string    `json:"area"`
	Outcome string    `json:"outcome"`
	Reason  string    `json:"reason,omitempty"`
	Source  string    `json:"source"`
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
// within its bound and the id of a source.
func validCoverageRow(r CoverageRow) bool {
	area, ok := NormalizeCoverageArea(r.Area)
	return ok && area == r.Area && CoverageOutcome(r.Outcome) && utf8.ValidString(r.Reason) && len(r.Reason) <= CoverageReasonMax && textTrace.MatchString(r.Source) && !r.At.IsZero()
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

// RecordCoverage writes what the ledger keeps of the rows of the result saved as source. It writes a row whose outcome is open, and
// a row that closes an area the ledger holds open (reported, clear or excluded); a closing row for an area that is not open is not
// written (the result's own saved source holds it). A row is not written again when the ledger already has a line of the same area
// from the same source, so the same record sent again adds nothing, and an old result sent again cannot reopen an area a later
// result closed. The lock is held from the read to the append. A row that is not a valid row refuses the whole call before anything
// is written.
func (s Store) RecordCoverage(scope, source string, rows []CoverageRow) error {
	if !textTrace.MatchString(source) {
		return errors.New("a coverage row needs the id of the saved result it came from")
	}
	path := s.coveragePath(scope)
	at := time.Now().UTC()
	return s.withJSONLAppendLock(path, func() error {
		ledger, err := s.readCoverage(path)
		if err != nil {
			return err
		}
		latest := map[string]CoverageRow{}
		wrote := map[string]bool{} // the areas this result has a line for already
		for _, r := range ledger {
			latest[r.Area] = r
			if r.Source == source {
				wrote[r.Area] = true
			}
		}
		var out []byte
		for _, row := range rows {
			area, _ := NormalizeCoverageArea(row.Area)
			row = CoverageRow{At: at, Area: area, Outcome: row.Outcome, Reason: row.Reason, Source: source}
			if !validCoverageRow(row) {
				return errors.New("a coverage row needs an area (1-160 bytes on one line), an outcome of open, reported, clear or excluded, and a reason of at most 300 bytes")
			}
			if wrote[area] {
				continue // the same record sent again; a result writes an area once, even when a later result has closed it since
			}
			prior, seen := latest[area]
			if row.Outcome != CoverageOpen && (!seen || prior.Outcome != CoverageOpen) {
				continue // nothing open to close
			}
			line, err := json.Marshal(row)
			if err != nil {
				return err
			}
			if len(line)+1 > coverageLineMax {
				return errors.New("a coverage row is too long to be read back")
			}
			out = append(append(out, line...), '\n')
			latest[area], wrote[area] = row, true
		}
		if len(out) == 0 {
			return nil
		}
		return s.appendJSONLBytesLocked(path, out)
	})
}
