package store

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"time"
)

// RungOf is the name of the record's step. Old records (no rung) are the minimal question.
func (r Record) RungOf() string {
	if r.Rung == "" {
		return "minimal"
	}
	return r.Rung
}

// Label is one truth. Truth's form depends on the type: "yes"/"no" for noul, an option id for choice, a level number for score.
type Label struct {
	TraceID    string             `json:"trace_id"`
	SpecID     string             `json:"spec_id"`
	SpecHash   string             `json:"spec_hash"`
	Type       string             `json:"type"`
	Model      string             `json:"model"`
	InputsHash string             `json:"inputs_hash"`
	Inputs     map[string]string  `json:"inputs,omitempty"` // keep the truth together with the text (material for re-asking)
	Raw        map[string]float64 `json:"raw"`
	Truth      string             `json:"truth"`
	Weight     float64            `json:"weight"`
	Source     string             `json:"source"` // ask | outcome | manual | ui | mcp | file | replay | context | …
	Tainted    bool               `json:"tainted,omitempty"`
	Rung       string             `json:"rung,omitempty"` // the step this truth was attached to (minimal | context). raw and spec_hash belong to that step
	At         time.Time          `json:"at"`
}

// MaxLineBytes is the limit for one record or label line. The writer rejects longer lines (smaller than the reader's limit).
const MaxLineBytes = 4 << 20

// StoredInput is the text and whether its decision was tainted.
type StoredInput struct {
	Inputs  map[string]string
	Tainted bool
}

// Store is one directory.
type Store struct{ Dir string }

// NewTraceID is a random 16-digit value.
func NewTraceID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// HashInputs is a hash for comparing identity only, without keeping the input text.
func HashInputs(inputs map[string]string) string {
	keys := make([]string, 0, len(inputs))
	for k := range inputs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	h := sha256.New()
	for _, k := range keys {
		h.Write([]byte(k))
		h.Write([]byte{0})
		h.Write([]byte(inputs[k]))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}
