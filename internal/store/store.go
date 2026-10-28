// Package store keeps decision records and labels. One JSONL file per spec.
//
//	<dir>/records/<spec_id>.jsonl  … every decision (with raw probabilities)
//	<dir>/labels/<spec_id>.jsonl   … truth labels (picked up from the user's actions)
//
// The text (inputs) is always kept in the local record (so a person can add the truth later, and it can be re-asked when the wording or model changes). Whether it is sent out is decided by the spec's send_to and pii.
package store

import (
	"bufio"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
)

// Record is one decision: decide.Decision plus provenance.
type Record struct {
	TraceID          string             `json:"trace_id"`
	SpecID           string             `json:"spec_id"`
	SpecHash         string             `json:"spec_hash"`
	Type             string             `json:"type"`
	Model            string             `json:"model,omitempty"`
	Endpoint         string             `json:"endpoint,omitempty"`
	Inputs           map[string]string  `json:"inputs,omitempty"` // the text (old records may not have it)
	InputsHash       string             `json:"inputs_hash"`
	Raw              map[string]float64 `json:"raw,omitempty"`
	Calibrated       map[string]float64 `json:"calibrated,omitempty"`
	IsCalibrated     bool               `json:"is_calibrated"`
	Top              string             `json:"top,omitempty"`
	P                float64            `json:"p"`
	Band             string             `json:"band"`
	Tainted          bool               `json:"tainted,omitempty"`        // a decision from a session that read production data. This text is never sent to hosted, even when re-asked
	Rung             string             `json:"rung,omitempty"`           // which step decided it: minimal (the minimal question; empty in old records) | context (context was added and asked again)
	Minimal          *Pass              `json:"minimal,omitempty"`        // when context was added, the minimal question's answer (that step's hash and raw; a person's truth is attached here too)
	Display          *RecordDisplay     `json:"display,omitempty"`        // side-by-side text in the configured language (written by the hook; absent for English): to trace later what was asked and what was done
	Project          string             `json:"project,omitempty"`        // which work the decision belongs to (written by the hook: the name of the repository root of cwd, or the cwd name outside git). Neither an input nor a hash key; a mark for splitting views on the page and in status
	RedoOf           string             `json:"redo_of,omitempty"`        // a past decision's (trace_id) text decided again with the current wording and line. The original record leaves the queue
	ReviewPending    bool               `json:"review_pending,omitempty"` // the AI provisionally scores it from the rest of the conversation. Not put in the queue for a person to score every item
	At               time.Time          `json:"at"`
	KnowledgeContext *KnowledgeContext  `json:"knowledge_context,omitempty"`
	// AgentPaired is set on a needs-plan decision organized under 0.11's rule (open filled only when something blocks): only such a
	// decision is compared with the agent's answer. One organized before filled open with reasons too, and comparing it asked the person
	AgentPaired bool `json:"agent_paired,omitempty"`
}

// Pass is the answer of one step. A record that added context and asked again uses it to keep the minimal question's answer.
// RecordDisplay is the translation in the configured language attached to a decision record (from the spec's display.<lang>). Never sent to the model and not part of the calibration key.
type RecordDisplay struct {
	Lang     string `json:"lang"`
	Question string `json:"question,omitempty"` // the question in people's words
	Top      string `json:"top,omitempty"`      // the model's answer (option) in people's words
	Message  string `json:"message,omitempty"`  // the text the hook showed to the person or agent (alongside English). Absent if nothing was shown
}

type Pass struct {
	SpecHash     string             `json:"spec_hash"`
	InputsHash   string             `json:"inputs_hash,omitempty"` // hash of only the inputs this step's template uses (the minimal question sees the same input even if the context inputs differ)
	Raw          map[string]float64 `json:"raw,omitempty"`
	Calibrated   map[string]float64 `json:"calibrated,omitempty"`
	IsCalibrated bool               `json:"is_calibrated"`
	Top          string             `json:"top,omitempty"`
	P            float64            `json:"p"`
	Band         string             `json:"band"`
}

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

// HumanPerThousand is person decisions per 1000 decisions. 0 when there are no records.
func (f Funnel) HumanPerThousand() float64 {
	if f.Records == 0 {
		return 0
	}
	return math.Round(1000 * float64(f.Human) / float64(f.Records))
}

// Funnel counts the spec's records by step and band. Unreadable lines are not counted.
// discardUnsure is the same rule as decide's "unsure": for types whose band comes from the highest option (choice, score without level), discard also goes to a person.
func (s Store) Funnel(specID string, discardUnsure bool) (Funnel, error) {
	var f Funnel
	err := s.scanJSONL(s.recordsPath(specID), func(line []byte) error {
		var r Record
		if json.Unmarshal(line, &r) != nil {
			return nil
		}
		f.Records++
		switch {
		case r.Band == "ask" || (discardUnsure && r.Band == "discard"):
			f.Human++
		case r.RungOf() == "context":
			f.Context++
		default:
			f.Minimal++
		}
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		return f, err
	}
	return f, nil
}

// AuditSample decides "show this decision to a person as a spot check". Decisions that acted automatically are not seen by people, so act precision cannot be measured with people's truths.
// So 1 in 20 is picked by trace_id (the same every time) and put in the answer-check queue. Empty traces are never picked.
func AuditSample(traceID string) bool {
	if traceID == "" {
		return false
	}
	h := fnv.New32a()
	h.Write([]byte(traceID))
	return h.Sum32()%20 == 0
}

// HumanSource reports whether truths from that source were decided by a person (or the outcome). Machine-decided truths are the upper step's answer (context), the judge (judge: another model's answer)
// and an agreement (agreed, 0.11: the decision model and the agent gave the same answer, so nobody was asked; calibrating on agreements alone would overstate precision).
// Re-asking (replay) and files (file) only had the machine fetch the probabilities again; the truth itself was given by a person.
func HumanSource(source string) bool {
	return !machineSources[source]
}

// machineSources are the sources of truths no person or outcome decided.
var machineSources = map[string]bool{"context": true, "judge": true, SourceAgreed: true}

// SourceAgreed is the source of a truth written when the decision model and the agent agreed (0.11).
const SourceAgreed = "agreed"

// Partial reports whether it is a partial label in "!<id>" form.
func Partial(truth string) bool { return strings.HasPrefix(truth, "!") && len(truth) > 1 }

// Correct reports whether the model's top answer top matches the truth. A partial label "!x" means "right unless it is x".
func Correct(top, truth string) bool {
	if Partial(truth) {
		return top != truth[1:]
	}
	return top == truth
}

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

func (s Store) appendJSONL(path string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if len(b) > MaxLineBytes {
		return fmt.Errorf("record too large: %d bytes > %d (inputs must stay under the line limit)", len(b), MaxLineBytes)
	}
	f, err := s.openFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(append(b, '\n'))
	return err
}

func (s Store) scanJSONL(path string, fn func(line []byte) error) error {
	f, err := s.openFile(path, os.O_RDONLY)
	if err != nil {
		return err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64<<10), 2*MaxLineBytes) // larger than the writer's limit (so old records and hand-edited lines can still be read)
	for sc.Scan() {
		if len(strings.TrimSpace(sc.Text())) == 0 {
			continue
		}
		if err := fn([]byte(sc.Text())); err != nil {
			return err
		}
	}
	return sc.Err()
}

// AppendLabelsOnce appends labels that are not already there with the same trace, spec hash, model, truth and source, under a lock on
// the spec's labels (0.9.3). Outcome truths are decided by hooks that can race (the original session's hook gives up its lock after
// 2 seconds) or retry after a failed save; this makes each one land once. A different truth for the same trace is still written.
func (s Store) AppendLabelsOnce(ls []Label) error {
	if len(ls) == 0 {
		return nil
	}
	lock, err := s.openFile(s.labelsPath(ls[0].SpecID)+".lock", os.O_CREATE|os.O_RDWR)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	key := func(l Label) string {
		return l.TraceID + "\x00" + l.SpecHash + "\x00" + l.Model + "\x00" + l.Truth + "\x00" + l.Source
	}
	have := map[string]bool{}
	if err := s.scanJSONL(s.labelsPath(ls[0].SpecID), func(line []byte) error {
		var l Label
		if json.Unmarshal(line, &l) == nil {
			have[key(l)] = true
		}
		return nil
	}); err != nil && !os.IsNotExist(err) {
		return err
	}
	var fresh []Label
	for _, l := range ls {
		if !have[key(l)] {
			have[key(l)] = true
			fresh = append(fresh, l)
		}
	}
	return s.AppendLabels(fresh)
}
