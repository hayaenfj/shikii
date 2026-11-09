package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
)

// AppendLabel appends a label.
func (s Store) AppendLabel(l Label) error {
	return s.AppendLabels([]Label{l})
}

// AppendLabels appends several labels (same spec) in one write. When one answer goes to two steps, only one of them can never be left behind.
func (s Store) AppendLabels(ls []Label) error {
	if len(ls) == 0 {
		return nil
	}
	var buf []byte
	for _, l := range ls {
		if l.TraceID == "" || l.SpecID == "" || l.SpecHash == "" || l.Model == "" {
			return errors.New("label needs trace_id, spec_id, spec_hash and model")
		}
		if l.SpecID != ls[0].SpecID {
			return errors.New("labels in one append must belong to one spec")
		}
		b, err := json.Marshal(l)
		if err != nil {
			return err
		}
		if len(b) > MaxLineBytes {
			return fmt.Errorf("record too large: %d bytes > %d (inputs must stay under the line limit)", len(b), MaxLineBytes)
		}
		buf = append(append(buf, b...), '\n')
	}
	path := s.labelsPath(ls[0].SpecID)
	f, err := s.openFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(buf)
	return err
}

// Labels returns only labels matching (spec_hash, model). Labels for old wording or other models are not used for calibration.
func (s Store) Labels(specID, specHash, model string) ([]Label, error) {
	var out []Label
	err := s.scanJSONL(s.labelsPath(specID), func(line []byte) error {
		var l Label
		if err := json.Unmarshal(line, &l); err != nil {
			return nil
		}
		if l.SpecHash == specHash && l.Model == model {
			out = append(out, l)
		}
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	return out, nil
}
