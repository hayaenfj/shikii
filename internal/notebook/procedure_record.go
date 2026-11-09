package notebook

import (
	"encoding/json"
	"errors"
)

// The generator, held-out validation, promotion and rollback all run from one
// ordinary ending report. Checked quality remains an agent/host report, not truth.
func (b Book) RecordProcedure(s ProcedureSample) (view *ProcedureStatus, retErr error) {
	if !idPattern.MatchString(s.Trace.Work) || !idPattern.MatchString(s.Trace.Case) || s.Trace.BaselineBytes < 0 || s.Trace.BaselineBytes > 16<<20 || s.Trace.ActualBytes < 0 || s.Trace.ActualBytes > 16<<20 || len(s.Evidence) > 8 {
		return nil, errors.New("invalid bounded procedure execution")
	}
	// Eligibility is computed from checked originals, never accepted from callers.
	s.Ineligible = false
	ids, e := b.procedureEvidence(s)
	if e != nil && !errors.Is(e, errProcedureQuality) {
		return nil, e
	}
	s.Ineligible = e != nil
	if !b.procedureCurrent(s.Trace.Basis) {
		v := ProcedureStatus{Status: "stale", Policy: DefaultProcedure().ID}
		return &v, nil
	}
	execution, e := b.captureProcedureExecution(s)
	if e != nil {
		return nil, e
	}
	defer func() {
		if retErr == nil && view != nil {
			view.Source = execution.ID
			if s.Ineligible {
				view.Ineligible = true
				view.QualityNotice = "The original report is saved; its evidence cannot establish procedure quality. Report an actual check in a later result when available."
			}
		}
	}()
	costRegression := s.Trace.Config.Compact > 0 && s.Trace.Stalls > 0 && s.Trace.BaselineBytes > 0 && s.Trace.ActualBytes > s.Trace.BaselineBytes
	failed := !s.Trace.Safe || costRegression || !s.Ineligible && s.Sufficient != nil && !*s.Sufficient
	if s.Ineligible && !failed {
		status := "ungraded"
		if s.Sufficient != nil {
			status = "ineligible"
		}
		return &ProcedureStatus{Status: status, Policy: s.Trace.Config.ID}, nil
	}
	if s.Trace.Mixed {
		return &ProcedureStatus{Status: "mixed", Policy: DefaultProcedure().ID}, nil
	}
	dir, e := b.directory(true, "searches")
	if e != nil {
		return nil, e
	}
	un, e := lock(dir)
	if e != nil {
		return nil, e
	}
	defer un()
	j, e := readProcedureJournal(dir)
	if e != nil {
		return nil, e
	}
	if j.Limited {
		return &ProcedureStatus{Status: "limited", Policy: DefaultProcedure().ID}, nil
	}
	key := procedureKey(s.Trace.Basis)
	l := j.Lanes[key]
	if l == nil {
		if len(j.Lanes) >= 32 {
			return &ProcedureStatus{Status: "limited", Policy: DefaultProcedure().ID}, nil
		}
		l = &procedureLane{Active: DefaultProcedure(), Seen: map[string]string{}}
		j.Lanes[key] = l
	}
	refreshed := b.refreshProcedureSources(l)
	data, _ := json.Marshal(s)
	signature := digest(data)
	if prior := l.Seen[s.Snapshot.ID]; prior != "" {
		if prior != signature {
			return nil, errors.New("a saved procedure execution cannot be rewritten")
		}
		if refreshed {
			if e := saveProcedureJournal(dir, &j); e != nil {
				return nil, e
			}
		}
		v := procedureView(l)
		return &v, nil
	}
	if len(l.Seen) >= 64 {
		l.Limited = true
		l.Active = DefaultProcedure()
		l.Candidate = nil
	} else {
		l.Seen[s.Snapshot.ID] = signature
	}
	transitioned := false
	if !l.Limited && failed {
		if p := l.Candidate; p != nil && s.Trace.Config.ID == p.Config.ID {
			p.Status = "rejected"
			p.Failure = &execution
			l.History = append(l.History, *p)
			l.Candidate = nil
			l.Failures++
		} else if rollbackProcedure(l, s.Trace.Config.ID, &execution) {
			l.Candidate = nil
			l.Failures++
		}
	} else if !l.Limited && !s.Ineligible && s.Sufficient != nil && *s.Sufficient {
		if p := l.Candidate; p != nil && p.Config.ID == s.Trace.Config.ID && p.Exposures[s.Trace.Work] == s.Trace.Case && p.Origin != s.Trace.Case && s.Trace.BaselineBytes > s.Trace.ActualBytes && !containsProcedureEvidence(*p, ids) {
			seen := false
			for _, x := range p.Support {
				seen = seen || x.Case == s.Trace.Case
			}
			if !seen {
				p.Support = append(p.Support, procedureSupport{Case: s.Trace.Case, Identities: ids, Source: execution, Evidence: s.Evidence})
				p.SavedBytes += s.Trace.BaselineBytes - s.Trace.ActualBytes
			}
			if len(p.Support) >= p.Required {
				transitioned = true
				p.Status = "adopted"
				l.Active = p.Config
				l.History = append(l.History, *p)
				l.Candidate = nil
			}
		}
	}
	// Generate a new bounded recipe from actual stalls. After failure a different
	// recipe needs more held-out checks; the failed recipe is never resurrected.
	if !transitioned && !l.Limited && !failed && !s.Ineligible && l.Candidate == nil && s.Trace.Stalls >= 2 && s.Trace.Config.ID == l.Active.ID && l.Active.Compact < 2 && len(l.History) < 4 {
		level := l.Active.Compact + 1
		for _, old := range l.History {
			level = max(level, old.Config.Compact+1)
		}
		if level <= 2 {
			repeatAfter := 1
			if l.Failures > 0 {
				repeatAfter = 2
			}
			p := &procedureProposal{Config: ProcedureConfig{Parent: l.Active.ID, RepeatAfter: repeatAfter, Compact: level}, Origin: s.Trace.Case, Source: execution, Required: min(4, 2+l.Failures), Exposures: map[string]string{}, Status: "proposed", OriginIdentities: ids}
			v, _ := json.Marshal([]any{key, p.Config.Parent, level, p.Required})
			p.Config.ID = "loop-" + digest(v)[:16]
			l.Candidate = p
		}
	}
	if e := saveProcedureJournal(dir, &j); e != nil {
		return nil, e
	}
	if j.Limited {
		return &ProcedureStatus{Status: "limited", Policy: DefaultProcedure().ID}, nil
	}
	v := procedureView(l)
	v.Source = execution.ID
	return &v, nil
}

func rollbackProcedure(l *procedureLane, bad string, failure *Source) bool {
	chain := []int{}
	id := l.Active.ID
	for id != DefaultProcedure().ID {
		found := -1
		for i, p := range l.History {
			if p.Config.ID == id {
				found = i
				break
			}
		}
		if found < 0 {
			return false
		}
		chain = append(chain, found)
		p := &l.History[found]
		if id == bad {
			parent := DefaultProcedure()
			for _, old := range l.History {
				if old.Config.ID == p.Config.Parent {
					parent = old.Config
				}
			}
			for _, i := range chain {
				l.History[i].Status = "rolled_back"
				l.History[i].Failure = failure
			}
			l.Active = parent
			return true
		}
		id = p.Config.Parent
	}
	return false
}
