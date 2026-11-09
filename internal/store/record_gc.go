package store

import (
	"bufio"
	"encoding/json"
	"os"
	"time"
)

// GC removes the spec's records that "have at older than before and no label at all".
// Labels are calibration material, so records with labels are kept forever. The label files are not touched.
// With apply false it only counts and does not rewrite. With apply it writes into the same directory, then replaces by rename (a crash midway does not break the original file).
func (s Store) GC(specID string, before time.Time, apply bool) (GCResult, error) {
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
			return nil
		}
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
	if err := parent.Rename(tmpName, name); err != nil {
		return discard(err)
	}
	return res, nil
}
