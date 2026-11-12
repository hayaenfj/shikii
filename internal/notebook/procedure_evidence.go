package notebook

import (
	"encoding/json"
	"errors"
	"slices"
)

func (b Book) procedureSourcesCurrent(p procedureProposal) bool {
	if !b.procedureExecutionCurrent(p.Source) {
		return false
	}
	for _, s := range p.Support {
		if !b.procedureExecutionCurrent(s.Source) {
			return false
		}
		for _, ref := range s.Evidence {
			if _, e := b.checkedOutcomeSource(ref); e != nil {
				return false
			}
		}
	}
	return true
}

func (b Book) refreshProcedureSources(l *procedureLane) bool {
	changed := false
	if p := l.Candidate; p != nil && !b.procedureSourcesCurrent(*p) {
		p.Status = "rejected"
		p.Failure = &p.Source
		l.History = append(l.History, *p)
		l.Candidate = nil
		l.Failures++
		changed = true
	}
	// A descendant also depends on the parent it improved. Do not retain it
	// after losing the evidence that justified that parent.
	id := l.Active.ID
	for id != DefaultProcedure().ID {
		var p *procedureProposal
		for i := range l.History {
			if l.History[i].Config.ID == id {
				p = &l.History[i]
				break
			}
		}
		if p == nil {
			l.Active = DefaultProcedure()
			l.Candidate = nil
			return true
		}
		if !b.procedureSourcesCurrent(*p) {
			rollbackProcedure(l, id, &p.Source)
			l.Candidate = nil
			l.Failures++
			changed = true
			id = l.Active.ID
		} else {
			id = p.Config.Parent
		}
	}
	return changed
}

func (b Book) procedureExecutionCurrent(ref Source) bool {
	if !idPattern.MatchString(ref.ID) || !validOutcomeHash(ref.Hash) {
		return false
	}
	o, e := b.checkedOutcomeSource(ref)
	if kind, ok := WorkSourceOf(o); e != nil || !ok || kind != WorkProcedure() {
		return false
	}
	var s ProcedureSample
	if json.Unmarshal([]byte(o.Content), &s) != nil || s.Ineligible {
		return false
	}
	_, e = b.procedureEvidence(s)
	return e == nil
}

func (b Book) captureProcedureExecution(s ProcedureSample) (Source, error) {
	o, e := b.checkedOutcomeSource(s.Snapshot)
	if e != nil {
		return Source{}, e
	}
	data, e := json.Marshal(s)
	if e != nil {
		return Source{}, e
	}
	execution, e := b.CaptureWork(WorkProcedure(), string(data), "procedure:"+s.Snapshot.ID, o.Version, o.ObservedAt)
	if e != nil {
		return Source{}, e
	}
	refs, e := b.OutcomeSources([]string{execution.ID})
	if e != nil {
		return Source{}, e
	}
	// This automatic diagnostic does not create another manual reflection.
	if e = b.RetainExecutionSources([]Source{refs[0], s.Snapshot}, s.Evidence); e != nil {
		return Source{}, e
	}
	return refs[0], nil
}

var errProcedureQuality = errors.New("procedure quality evidence is ineligible")

func (b Book) procedureSources(s ProcedureSample) ([]Observation, error) {
	if !idPattern.MatchString(s.Snapshot.ID) || !validOutcomeHash(s.Snapshot.Hash) {
		return nil, errors.New("invalid original procedure result source")
	}
	o, e := b.checkedOutcomeSource(s.Snapshot)
	if e != nil {
		return nil, e
	}
	if kind, ok := WorkSourceOf(o); !ok || kind != WorkResult() || o.Kind != "agent" {
		return nil, errors.New("procedure feedback needs the saved original work result")
	}
	var report struct {
		Sufficient *bool `json:"sufficient"`
	}
	if json.Unmarshal([]byte(o.Content), &report) != nil || (report.Sufficient == nil) != (s.Sufficient == nil) || report.Sufficient != nil && *report.Sufficient != *s.Sufficient {
		return nil, errors.New("procedure quality must match its original report")
	}
	observations := []Observation{}
	for _, ref := range s.Evidence {
		if !idPattern.MatchString(ref.ID) || !validOutcomeHash(ref.Hash) {
			return nil, errors.New("invalid procedure check source")
		}
		o, e := b.checkedOutcomeSource(ref)
		if e != nil {
			return nil, e
		}
		observations = append(observations, o)
	}
	return observations, nil
}

func (b Book) procedureEvidence(s ProcedureSample) ([]string, error) {
	observations, e := b.procedureSources(s)
	if e != nil {
		return nil, e
	}
	ids := []string{}
	for _, o := range observations {
		if o.Kind == "human" && s.Sufficient != nil {
			return nil, errProcedureQuality
		}
		d := ReadingDetails{}
		if s.Sufficient != nil {
			d.Sufficient = *s.Sufficient
		}
		if v, ok := readingJSON(o.Content); ok {
			if object, ok := v.(map[string]any); ok {
				d.Check, _ = object["check"].(string)
				if s.Sufficient == nil {
					d.Sufficient, _ = object["sufficient"].(bool)
				}
			}
		}
		id, e := readingQualityIdentity(o.Content, d)
		if e != nil {
			return nil, errProcedureQuality
		}
		ids = append(ids, id)
	}
	if s.Sufficient != nil && len(ids) == 0 {
		return nil, errProcedureQuality
	}
	return ids, nil
}

func containsProcedureEvidence(p procedureProposal, ids []string) bool {
	for _, id := range ids {
		if slices.Contains(p.OriginIdentities, id) {
			return true
		}
	}
	for _, v := range p.Support {
		for _, id := range ids {
			if slices.Contains(v.Identities, id) {
				return true
			}
		}
	}
	return false
}
