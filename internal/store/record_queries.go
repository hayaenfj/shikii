package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// SpecIDs returns the ids of specs that have records or labels.
func (s Store) SpecIDs() ([]string, error) {
	seen := map[string]bool{}
	for _, sub := range []string{"records", "labels"} {
		files, err := filepath.Glob(filepath.Join(s.Dir, sub, "*.jsonl"))
		if err != nil {
			return nil, err
		}
		for _, f := range files {
			id := strings.TrimSuffix(filepath.Base(f), ".jsonl")
			if id == "guards" { // the guard records (records/guards.jsonl) are not decisions (audit R3)
				continue
			}
			seen[id] = true
		}
	}
	out := make([]string, 0, len(seen))
	for id := range seen {
		out = append(out, id)
	}
	sort.Strings(out)
	return out, nil
}

// Counts returns the spec's record count and every label (not narrowed by key). For inventory.
func (s Store) Counts(specID string) (records int, labels []Label, err error) {
	err = s.scanJSONL(s.recordsPath(specID), func([]byte) error { records++; return nil })
	if err != nil && !os.IsNotExist(err) {
		return 0, nil, err
	}
	err = s.scanJSONL(s.labelsPath(specID), func(line []byte) error {
		var l Label
		if json.Unmarshal(line, &l) == nil {
			labels = append(labels, l)
		}
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		return 0, nil, err
	}
	return records, labels, nil
}

// InputsByHash maps input hash → text from records that still have their text.
// Material for re-asking the same input with a new (spec_hash, model) after the wording or model changed.
func (s Store) InputsByHash(specID string) (map[string]StoredInput, error) {
	out := map[string]StoredInput{}
	take := func(hash string, inputs map[string]string, tainted bool) {
		if hash == "" || len(inputs) == 0 {
			return
		}
		cur, ok := out[hash]
		if !ok {
			out[hash] = StoredInput{Inputs: inputs, Tainted: tainted}
			return
		}
		if tainted && !cur.Tainted { // text recorded as tainted even once is never sent out afterwards
			cur.Tainted = true
			out[hash] = cur
		}
	}
	err := s.scanJSONL(s.recordsPath(specID), func(line []byte) error {
		var r Record
		if json.Unmarshal(line, &r) == nil {
			take(r.InputsHash, r.Inputs, r.Tainted)
			if r.Minimal != nil {
				take(r.Minimal.InputsHash, r.Inputs, r.Tainted) // the minimal step's hash also reaches the same text (Render ignores extra inputs)
			}
		}
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	// labels calibrated from a file have no record, so also look at the text on the label side
	err = s.scanJSONL(s.labelsPath(specID), func(line []byte) error {
		var l Label
		if json.Unmarshal(line, &l) == nil {
			take(l.InputsHash, l.Inputs, l.Tainted)
		}
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	return out, nil
}

// LatestPerInput keeps only the newest label per input (inputs_hash).
// If one input has several truths (a file row, a later correction by a person, a second answer to the same trace), the newer one wins.
// calibrate / check / drift all go through this (so they count the same way). Labels without inputs_hash are kept as is.
func LatestPerInput(labels []Label) []Label {
	latest := map[string]Label{}
	order := []string{}
	var out []Label
	for _, l := range labels {
		if l.InputsHash == "" {
			out = append(out, l)
			continue
		}
		cur, ok := latest[l.InputsHash]
		if !ok {
			order = append(order, l.InputsHash)
			latest[l.InputsHash] = l
			continue
		}
		// a person's truth always beats a machine truth (context / judge). Within the same kind, the newer one wins (audit round 4, item 2)
		switch {
		case HumanSource(l.Source) && !HumanSource(cur.Source):
			latest[l.InputsHash] = l
		case !HumanSource(l.Source) && HumanSource(cur.Source):
		case l.At.After(cur.At):
			latest[l.InputsHash] = l
		}
	}
	for _, h := range order {
		out = append(out, latest[h])
	}
	return out
}

// Records returns the spec's records newest first, limit of them (all if limit <= 0). For showing the queue.
func (s Store) Records(specID string, limit int) ([]Record, error) {
	var all []Record
	err := s.scanJSONL(s.recordsPath(specID), func(line []byte) error {
		var r Record
		if json.Unmarshal(line, &r) == nil {
			all = append(all, r)
		}
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].At.After(all[j].At) })
	if limit > 0 && len(all) > limit {
		all = all[:limit]
	}
	return all, nil
}

// LabeledTraces maps trace_id → label (the newest if several). To see whether a record has a truth.
func (s Store) LabeledTraces(specID string) (map[string]Label, error) {
	out := map[string]Label{}
	err := s.scanJSONL(s.labelsPath(specID), func(line []byte) error {
		var l Label
		if json.Unmarshal(line, &l) != nil || l.TraceID == "" {
			return nil
		}
		if cur, ok := out[l.TraceID]; !ok || (HumanSource(l.Source) && !HumanSource(cur.Source)) || (HumanSource(l.Source) == HumanSource(cur.Source) && l.At.After(cur.At)) {
			out[l.TraceID] = l
		}
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	return out, nil
}

// LastRecord is the record appended last for the spec. So you do not have to note the trace_id when labeling by hand.
func (s Store) LastRecord(specID string) (*Record, error) {
	var last *Record
	err := s.scanJSONL(s.recordsPath(specID), func(line []byte) error {
		var r Record
		if json.Unmarshal(line, &r) == nil {
			last = &r
		}
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	if last == nil {
		return nil, fmt.Errorf("no records for spec %s", specID)
	}
	return last, nil
}

// GCResult counts what GC kept and removed (or would remove).
type GCResult struct {
	Removed      int // old records without labels (removed with apply, otherwise the number that would be removed)
	KeptLabeled  int // kept regardless of age because they have a label
	KeptRecent   int // kept because they are newer than cutoff
	KeptUnparsed int // lines that do not parse as JSON. Unreadable lines are never thrown away
}
