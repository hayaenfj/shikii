package notebook

import (
	"encoding/json"
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
	for _, e := range entries {
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
