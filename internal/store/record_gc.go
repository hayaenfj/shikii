package store

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// beforeGCReplace lets tests check writer exclusion at the replacement boundary.
var beforeGCReplace = func() {}

// GC removes the spec's records that "have at older than before and no label at all".
// Labels are calibration material, so records with labels are kept forever. The label files are not touched.
// With apply false it only counts and does not rewrite. With apply it writes into the same directory, then replaces by rename (a crash midway does not break the original file).
func (s Store) GC(specID string, before time.Time, apply bool) (GCResult, error) {
	if !apply {
		return s.gcSnapshot(specID, before, false)
	}
	// A missing history remains a no-op, including in an empty store.
	parent, name, err := s.parentRoot(s.recordsPath(specID), false)
	if err == nil {
		_, err = parent.Lstat(name)
		parent.Close()
	}
	if os.IsNotExist(err) {
		return GCResult{}, nil
	}
	if err != nil {
		return GCResult{}, err
	}
	var res GCResult
	// Serialize the label snapshot and replacement with both writers. All GC
	// calls acquire labels before records; appenders hold only their own lock.
	err = s.withJSONLAppendLock(s.labelsPath(specID), func() error {
		return s.withJSONLAppendLock(s.recordsPath(specID), func() error {
			var err error
			res, err = s.gcSnapshot(specID, before, true)
			return err
		})
	})
	return res, err
}

func (s Store) gcSnapshot(specID string, before time.Time, apply bool) (GCResult, error) {
	var res GCResult
	labeled := map[string]bool{}
	err := s.scanJSONL(s.labelsPath(specID), func(line []byte) error {
		var l Label
		if json.Unmarshal(line, &l) == nil && l.TraceID != "" {
			labeled[l.TraceID] = true
		}
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		return res, err
	}
	var keep [][]byte
	var removed []Record // removed records: their original text goes with them
	kept := map[string]bool{}
	err = s.scanJSONL(s.recordsPath(specID), func(line []byte) error {
		var r Record
		if json.Unmarshal(line, &r) != nil || r.TraceID == "" {
			res.KeptUnparsed++
			keep = append(keep, append([]byte(nil), line...))
			return nil
		}
		switch {
		case labeled[r.TraceID]:
			res.KeptLabeled++
		case !r.At.Before(before):
			res.KeptRecent++
		default:
			res.Removed++
			removed = append(removed, Record{TraceID: r.TraceID, InputsHash: r.InputsHash})
			return nil
		}
		kept[r.TraceID] = true
		keep = append(keep, append([]byte(nil), line...))
		return nil
	})
	if err != nil {
		if os.IsNotExist(err) {
			return res, nil
		}
		return res, err
	}
	if !apply || res.Removed == 0 {
		return res, nil
	}
	path := s.recordsPath(specID)
	parent, name, err := s.parentRoot(path, false)
	if err != nil {
		return res, err
	}
	defer parent.Close()
	tmpName := name + ".gc-" + NewTraceID()
	tmp, err := parent.OpenFile(tmpName, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return res, err
	}
	discard := func(e error) (GCResult, error) {
		tmp.Close()
		parent.Remove(tmpName)
		return res, e
	}
	w := bufio.NewWriter(tmp)
	for _, line := range keep {
		if _, err := w.Write(append(line, '\n')); err != nil {
			return discard(err)
		}
	}
	if err := w.Flush(); err != nil {
		return discard(err)
	}
	if err := tmp.Close(); err != nil {
		return discard(err)
	}
	beforeGCReplace()
	if err := parent.Rename(tmpName, name); err != nil {
		return discard(err)
	}
	s.removeRecordTexts(specID, removed, kept)
	return res, nil
}

// removeRecordTexts deletes the original text (records/text/<trace_id>.json) of records GC just removed from the question
// specID. The folder is shared by every question and a text is named by its trace alone, so a text stays while any decision
// under its trace is left: a kept line of this question, or a line of another question (whatever its inputs). It goes with the
// last of them, whichever question is collected last. The records are already gone, so a text that cannot be removed is left,
// not reported.
func (s Store) removeRecordTexts(specID string, removed []Record, kept map[string]bool) {
	if len(removed) == 0 {
		return
	}
	dir, _, err := s.parentRoot(filepath.Join(s.Dir, "records", "text", "entry"), false)
	if err != nil {
		return // no original text was ever kept
	}
	defer dir.Close()
	candidates := map[string]bool{}
	for _, rec := range removed {
		trace := rec.TraceID
		if kept[trace] || !textTrace.MatchString(trace) {
			continue
		}
		if info, err := dir.Lstat(trace + ".json"); err == nil && info.Mode().IsRegular() {
			candidates[trace] = true
		}
	}
	if len(candidates) == 0 {
		return
	}
	// another question's decision under the same trace keeps the text; when the other questions cannot be read, nothing is removed
	others, err := s.tracesInOtherSpecs(specID, candidates)
	if err != nil {
		return
	}
	for trace := range candidates {
		if !others[trace] {
			_ = dir.Remove(trace + ".json")
		}
	}
}

// tracesInOtherSpecs reports which of the traces have a decision line in the records of a question other than specID.
func (s Store) tracesInOtherSpecs(specID string, traces map[string]bool) (map[string]bool, error) {
	found := map[string]bool{}
	entries, err := os.ReadDir(filepath.Join(s.Dir, "records"))
	if err != nil {
		return nil, err
	}
	own := filepath.Base(s.recordsPath(specID))
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".jsonl") || name == own || name == "guards.jsonl" {
			continue
		}
		err := s.scanJSONL(filepath.Join(s.Dir, "records", name), func(line []byte) error {
			var r struct {
				TraceID string `json:"trace_id"`
			}
			if json.Unmarshal(line, &r) == nil && traces[r.TraceID] {
				found[r.TraceID] = true
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return found, nil
}
