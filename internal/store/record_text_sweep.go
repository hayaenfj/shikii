package store

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/hayaenfj/shikii/internal/rootfile"
)

// textSweepMinAge keeps the texts of decisions being written: a decision's original text may be saved just before its line.
const textSweepMinAge = time.Hour

// SweepRecordTexts finds the original texts (records/text/<trace_id>.json) whose trace has no decision line left in any
// question's records, older than before (and than an hour), and removes them when apply is set. GC removes a text with the last
// decision of its trace; a run that met a line naming no decision kept them, and nothing names those traces again, so they are
// swept here. While any records file holds a line that names no decision (not JSON, or no trace_id), nothing is removed and
// blocked is true: that line may belong to any of them. It never follows a link out of the store.
func (s Store) SweepRecordTexts(before time.Time, apply bool) (removed int, blocked bool, err error) {
	if limit := time.Now().Add(-textSweepMinAge); before.After(limit) {
		before = limit
	}
	dir, _, err := s.parentRoot(filepath.Join(s.Dir, "records", "text", "entry"), false)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return 0, false, nil // no original text was ever kept
		}
		return 0, false, err
	}
	defer dir.Close()
	f, err := rootfile.OpenNoFollow(dir, ".", os.O_RDONLY, 0)
	if err != nil {
		return 0, false, err
	}
	entries, err := f.ReadDir(-1)
	f.Close()
	if err != nil {
		return 0, false, err
	}
	candidates := map[string]bool{}
	for _, e := range entries {
		trace, ok := strings.CutSuffix(e.Name(), ".json")
		if !ok || !textTrace.MatchString(trace) || !e.Type().IsRegular() {
			continue
		}
		if info, err := e.Info(); err == nil && info.ModTime().Before(before) {
			candidates[trace] = true
		}
	}
	if len(candidates) == 0 {
		return 0, false, nil
	}
	used, err := s.decisionTraces()
	if errors.Is(err, errUnknownTrace) {
		return 0, true, nil
	}
	if err != nil {
		return 0, false, err
	}
	for trace := range candidates {
		if used[trace] {
			continue
		}
		if apply {
			if err := dir.Remove(trace + ".json"); err != nil {
				continue // the record of a decision is gone already: a text that cannot be removed is left
			}
		}
		removed++
	}
	return removed, false, nil
}

// decisionTraces reads the traces of every decision line in every question's records (the stop checks' records are not decisions).
// A line that names no trace is errUnknownTrace.
func (s Store) decisionTraces() (map[string]bool, error) {
	found := map[string]bool{}
	entries, err := os.ReadDir(filepath.Join(s.Dir, "records"))
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".jsonl") || name == "guards.jsonl" {
			continue
		}
		err := s.scanJSONL(filepath.Join(s.Dir, "records", name), func(line []byte) error {
			var r struct {
				TraceID string `json:"trace_id"`
			}
			if json.Unmarshal(line, &r) != nil || r.TraceID == "" {
				if len(bytes.TrimSpace(line)) == 0 {
					return nil
				}
				return errUnknownTrace
			}
			found[r.TraceID] = true
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return found, nil
}
