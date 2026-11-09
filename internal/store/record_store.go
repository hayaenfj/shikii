package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

func (s Store) recordsPath(specID string) string {
	return filepath.Join(s.Dir, "records", specID+".jsonl")
}

func (s Store) labelsPath(specID string) string {
	return filepath.Join(s.Dir, "labels", specID+".jsonl")
}

// AppendRecord appends a decision.
func (s Store) AppendRecord(r Record) error {
	if r.TraceID == "" || r.SpecID == "" {
		return errors.New("record needs trace_id and spec_id")
	}
	return s.appendJSONL(s.recordsPath(r.SpecID), r)
}

// FindRecord finds a decision by trace_id. If the spec is known, specID narrows the search (empty looks everywhere).
func (s Store) FindRecord(specID, traceID string) (*Record, error) {
	var files []string
	if specID != "" {
		files = []string{s.recordsPath(specID)}
	} else {
		var err error
		files, err = filepath.Glob(filepath.Join(s.Dir, "records", "*.jsonl"))
		if err != nil {
			return nil, err
		}
	}
	for _, f := range files {
		var found *Record
		err := s.scanJSONL(f, func(line []byte) error {
			var r Record
			if err := json.Unmarshal(line, &r); err != nil {
				return nil // skip broken lines
			}
			if r.TraceID == traceID {
				found = &r
			}
			return nil
		})
		if err != nil && !os.IsNotExist(err) {
			return nil, err
		}
		if found != nil {
			return found, nil
		}
	}
	return nil, fmt.Errorf("no record with trace_id %s", traceID)
}

// LabelFor builds a label from a decision record and the user's answer. The answer must match the type's form.
func LabelFor(rec *Record, truth string, weight float64, source string) (Label, error) {
	truth = strings.TrimSpace(truth)
	switch rec.Type {
	case "noul":
		if truth != "yes" && truth != "no" {
			return Label{}, fmt.Errorf("noul truth must be yes or no, got %q", truth)
		}
	case "choice", "score":
		// "!<id>" is a partial label (only known: it was not that option). Used when picking it up from an outcome.
		if _, ok := rec.Raw[strings.TrimPrefix(truth, "!")]; !ok || truth == "!" {
			keys := make([]string, 0, len(rec.Raw))
			for k := range rec.Raw {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			return Label{}, fmt.Errorf("%s truth must be one of %v, got %q", rec.Type, keys, truth)
		}
	default:
		return Label{}, fmt.Errorf("record has unknown type %q", rec.Type)
	}
	if weight <= 0 {
		weight = 1
	}
	if weight > 1 {
		return Label{}, fmt.Errorf("weight must be in (0, 1], got %g (1 = a firm truth; less = a weak one)", weight)
	}
	if source == "" {
		source = "manual"
	}
	return Label{
		TraceID: rec.TraceID, SpecID: rec.SpecID, SpecHash: rec.SpecHash, Type: rec.Type,
		Model: rec.Model, InputsHash: rec.InputsHash, Inputs: rec.Inputs, Raw: rec.Raw,
		Truth: truth, Weight: weight, Source: source, Tainted: rec.Tainted, At: time.Now(),
	}, nil
}

// LabelsFor attaches one truth to every step the record went through.
// One label if the record was decided by the minimal question. Two if it was decided after adding context: the final answer (context hash and raw) and the minimal question (its hash and raw).
// A person's answer does not depend on the wording, so the lower step learns from the same truth (the person's side of "the upper step's answer becomes the lower step's truth").
func LabelsFor(rec *Record, truth string, weight float64, source string) ([]Label, error) {
	l, err := LabelFor(rec, truth, weight, source)
	if err != nil {
		return nil, err
	}
	l.Rung = rec.RungOf()
	out := []Label{l}
	if rec.Minimal != nil {
		m := l
		m.SpecHash, m.Raw, m.Rung = rec.Minimal.SpecHash, rec.Minimal.Raw, "minimal"
		if rec.Minimal.InputsHash != "" {
			m.InputsHash = rec.Minimal.InputsHash
		}
		if _, ok := m.Raw[strings.TrimPrefix(truth, "!")]; !ok && rec.Type != "noul" {
			return nil, fmt.Errorf("truth %q is not an option of the minimal pass", truth)
		}
		out = append(out, m)
	}
	return out, nil
}

// Funnel counts which step decided the records. Progress is read as "person decisions per 1000 decisions (those left at ask and sent to a person)".
type Funnel struct {
	Records int `json:"records"`
	Minimal int `json:"minimal"` // decided by the minimal question (act / discard)
	Context int `json:"context"` // decided after adding context (act / discard)
	Human   int `json:"human"`   // not decided at any step and sent to a person (ask)
}
