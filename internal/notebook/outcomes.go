package notebook

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// OutcomeDetails reports an execution, not a success label. The agent provides
// the interpretation; immutable evidence and the procedure revision stay linked.
type OutcomeDetails struct {
	PageID     string   `json:"page_id"`
	Revision   string   `json:"revision"`
	Conditions string   `json:"conditions"`
	Action     string   `json:"action"`
	Failure    string   `json:"failure,omitempty"`
	Correction string   `json:"correction,omitempty"`
	Result     string   `json:"result"`
	Evidence   []Source `json:"evidence"`
}
type ProcedureOutcome struct {
	OutcomeDetails
	Snapshot Source `json:"snapshot"`
}
type ProcedureOutcomeView struct {
	SourceID        string              `json:"source_id"`
	Revision        string              `json:"revision"`
	RevisionChanged bool                `json:"revision_changed"`
	Conditions      string              `json:"conditions"`
	Action          string              `json:"action"`
	Failure         string              `json:"failure,omitempty"`
	Correction      string              `json:"correction,omitempty"`
	Result          string              `json:"result"`
	Evidence        []ProcedureEvidence `json:"evidence"`
	Unknown         []string            `json:"unknown,omitempty"`
}

func (d OutcomeDetails) ValidateContent() error {
	if !idPattern.MatchString(d.PageID) || !idPattern.MatchString(d.Revision) {
		return errors.New("an outcome needs a procedure revision")
	}
	for _, s := range []string{d.Conditions, d.Action, d.Result} {
		if strings.TrimSpace(s) == "" || len(s) > 1000 || !utf8.ValidString(s) {
			return errors.New("outcome conditions, action and result must each be 1 to 1000 UTF-8 bytes")
		}
	}
	for _, s := range []string{d.Failure, d.Correction} {
		if len(s) > 1000 || !utf8.ValidString(s) {
			return errors.New("outcome failure and correction must each fit 1000 UTF-8 bytes")
		}
	}
	return nil
}

func (d OutcomeDetails) Validate() error {
	if err := d.ValidateContent(); err != nil {
		return err
	}
	if len(d.Evidence) == 0 || len(d.Evidence) > 4 {
		return errors.New("an outcome needs one to four evidence sources")
	}
	for _, s := range d.Evidence {
		if !idPattern.MatchString(s.ID) || !validOutcomeHash(s.Hash) {
			return errors.New("invalid outcome evidence reference")
		}
	}
	return nil
}
func (o ProcedureOutcome) validate() error {
	if err := o.OutcomeDetails.Validate(); err != nil {
		return err
	}
	if !idPattern.MatchString(o.Snapshot.ID) || !validOutcomeHash(o.Snapshot.Hash) {
		return errors.New("invalid outcome snapshot")
	}
	return nil
}
func validOutcomeHash(s string) bool {
	_, err := hex.DecodeString(s)
	return len(s) == 64 && err == nil
}

func (b Book) checkedOutcomeSource(s Source) (Observation, error) {
	dir, err := b.directory(false, "sources")
	if err != nil {
		return Observation{}, err
	}
	data, err := readSourceRecord(filepath.Join(dir, s.ID+".json"))
	if err != nil {
		return Observation{}, err
	}
	if !validSource(data, s.ID) || digest(data) != s.Hash {
		return Observation{}, errors.New("outcome evidence changed; inspect its original source")
	}
	var o Observation
	if err = json.Unmarshal(data, &o); err != nil {
		return o, err
	}
	if !o.valid() {
		return o, errors.New("outcome evidence must be a captured observation; capture the checked result first")
	}
	return o, nil
}

// OutcomeSources validates local immutable snapshots before recording a link.
func (b Book) OutcomeSources(ids []string) ([]Source, error) {
	if len(ids) == 0 || len(ids) > 4 {
		return nil, errors.New("an outcome needs one to four evidence sources")
	}
	dir, err := b.directory(false, "sources")
	if err != nil {
		return nil, err
	}
	refs := []Source{}
	for _, id := range ids {
		if !idPattern.MatchString(id) {
			return nil, errors.New("invalid outcome source")
		}
		data, e := readSourceRecord(filepath.Join(dir, id+".json"))
		if e != nil {
			return nil, e
		}
		if !validSource(data, id) {
			return nil, errors.New("invalid outcome evidence")
		}
		var observation Observation
		if json.Unmarshal(data, &observation) != nil || !observation.valid() {
			return nil, errors.New("outcome evidence must be a captured observation; capture the checked result first")
		}
		refs = append(refs, Source{ID: id, Hash: digest(data)})
	}
	return refs, nil
}

// ErrOutcomeRejected identifies a definite refusal without publication.
var ErrOutcomeRejected = errors.New("outcome rejected")

// RecordOutcome uses the existing bounded search journal as an index. The
// snapshot contains the details; the index cannot invent or change that content.
func (b Book) RecordOutcome(o ProcedureOutcome) error {
	if err := o.validate(); err != nil {
		return err
	}
	snapshot, err := b.checkedOutcomeSource(o.Snapshot)
	if err != nil {
		return err
	}
	content, _ := json.Marshal(o.OutcomeDetails)
	if snapshot.Kind != "agent" || snapshot.Content != string(content) {
		return errors.New("outcome details do not match their captured snapshot")
	}
	for _, s := range o.Evidence {
		if _, err = b.checkedOutcomeSource(s); err != nil {
			return err
		}
	}
	pageDir, err := b.directory(false, "pages")
	if err != nil {
		return err
	}
	unlock, err := lock(pageDir)
	if err != nil {
		return err
	}
	defer unlock()
	dir, err := b.directory(true, "searches")
	if err != nil {
		return err
	}
	unsearch, err := lock(dir)
	if err != nil {
		return err
	}
	defer unsearch()
	j, err := readSearchJournal(dir)
	if err != nil {
		return err
	}
	for _, old := range j.Outcomes {
		if old.Snapshot.ID == o.Snapshot.ID {
			if digest(mustOutcomeJSON(old)) != digest(mustOutcomeJSON(o)) {
				return errors.New("a saved outcome cannot be rewritten")
			}
			return nil
		}
	}
	live, err := b.LiveWhere(func(p Page) bool { return p.ID == o.PageID })
	if err != nil {
		return err
	}
	if len(live) != 1 || live[0].Revision != o.Revision || live[0].Kind != "howto" {
		return fmt.Errorf("%w: the outcome needs the same live howto revision; read it again", ErrOutcomeRejected)
	}
	j.Outcomes = append(j.Outcomes, o)
	if len(j.Outcomes) > 128 {
		j.Outcomes = j.Outcomes[len(j.Outcomes)-128:]
	}
	return saveSearchJournal(dir, j)
}
func mustOutcomeJSON(v any) []byte { data, _ := json.Marshal(v); return data }

// ProcedureOutcomes keeps past conditions separate from the new request and
// supplies evidence, never an automatic claim that the procedure succeeded.
func (b Book) ProcedureOutcomes(page Page, context string) ([]ProcedureOutcomeView, error) {
	return b.procedureOutcomes(page, context, false)
}

func (b Book) procedureOutcomes(page Page, context string, failuresOnly bool) ([]ProcedureOutcomeView, error) {
	dir, err := b.directory(false, "searches")
	if os.IsNotExist(err) {
		return []ProcedureOutcomeView{}, nil
	}
	if err != nil {
		return nil, err
	}
	j, err := readSearchJournal(dir)
	if err != nil {
		return nil, err
	}
	out := []ProcedureOutcomeView{}
	for i := len(j.Outcomes) - 1; i >= 0 && len(out) < 2; i-- {
		o := j.Outcomes[i]
		if o.PageID != page.ID || failuresOnly && strings.TrimSpace(o.Failure) == "" {
			continue
		}
		v, e := b.outcomeView(o, page, context)
		if e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, nil
}

func (b Book) outcomeView(o ProcedureOutcome, page Page, context string) (ProcedureOutcomeView, error) {
	v := ProcedureOutcomeView{}
	if err := o.validate(); err != nil {
		return v, err
	}
	snapshot, err := b.checkedOutcomeSource(o.Snapshot)
	if err != nil {
		return v, err
	}
	if snapshot.Kind != "agent" || snapshot.Content != string(mustOutcomeJSON(o.OutcomeDetails)) {
		return v, errors.New("outcome index does not match its source")
	}
	v = ProcedureOutcomeView{SourceID: o.Snapshot.ID, Revision: o.Revision, RevisionChanged: o.Revision != page.Revision, Conditions: o.Conditions, Action: o.Action, Failure: o.Failure, Correction: o.Correction, Result: o.Result, Evidence: []ProcedureEvidence{}}
	validated := false
	for _, s := range o.Evidence {
		evidence, e := b.checkedOutcomeSource(s)
		if e != nil {
			return v, e
		}
		entry := ProcedureEvidence{SourceID: s.ID, Kind: evidence.Kind, Reference: evidence.Reference, Version: evidence.Version, ObservedAt: evidence.ObservedAt.Format("2006-01-02T15:04:05.999999999Z07:00")}
		if len(evidence.Content) > 1200 {
			entry.Cut = true
			entry.ContentExcerpts = FocusExcerpts(evidence.Content, context, 1200)
		} else {
			entry.Content = evidence.Content
		}
		validated = validated || entry.Kind == "test" || entry.Kind == "release"
		v.Evidence = append(v.Evidence, entry)
	}
	if !validated {
		v.Unknown = append(v.Unknown, "No test or release snapshot is linked; the reported result does not establish success.")
	}
	return v, nil
}
