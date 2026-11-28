package notebook

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// CountWorkResultsSince counts the work results (the source of kind WorkResult) saved after since. A zero since counts them all.
//
// A project keeps thousands of sources, one file each and never rewritten, so a file is read only when its modification time is
// later than since; the title and observed_at of what is read then decide. A file that cannot be read is not counted: this is a
// count for a notice, not a check of the sources.
func (b Book) CountWorkResultsSince(since time.Time) (int, error) {
	return b.CountWorkResultsUntil(since, time.Time{})
}

// ErrWorkCountTooLong is what CountWorkResultsUntil returns when the deadline passed before every source was looked at: a count of part of
// them is no count, so none is given.
var ErrWorkCountTooLong = errors.New("the sources are too many to count within the time given")

// CountWorkResultsUntil is CountWorkResultsSince that gives up at deadline (a zero time is none): it returns ErrWorkCountTooLong, and
// no count, when the deadline has passed before the last source was looked at. A hook counts with it so that a project with very many
// sources costs the agent the notice that is counted for, never the rules the same hook brings.
func (b Book) CountWorkResultsUntil(since, deadline time.Time) (int, error) {
	dir, err := b.directory(false, "sources")
	if os.IsNotExist(err) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, err
	}
	n := 0
	for i, e := range entries {
		if !deadline.IsZero() && i%64 == 0 && time.Now().After(deadline) {
			return 0, ErrWorkCountTooLong
		}
		id, ok := strings.CutSuffix(e.Name(), ".json")
		if !ok || !idPattern.MatchString(id) {
			continue
		}
		info, err := e.Info()
		if err != nil || !info.Mode().IsRegular() || !info.ModTime().After(since) {
			continue
		}
		data, err := readSourceRecord(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		// only what decides the count, so the content of a large source is skipped rather than kept
		var head struct {
			Kind       string    `json:"kind"`
			Title      string    `json:"title"`
			ObservedAt time.Time `json:"observed_at"`
		}
		if json.Unmarshal(data, &head) != nil {
			continue
		}
		if kind, ok := WorkSourceOf(Observation{Kind: head.Kind, Title: head.Title}); ok && kind == WorkResult() && head.ObservedAt.After(since) {
			n++
		}
	}
	return n, nil
}
