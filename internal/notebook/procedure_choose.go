package notebook

import (
	"os"
)

// Reserve a finite validation slot. A failed delivery can consume a slot, never
// produce support. Reservations prevent ungraded experiments from running forever.
func (b Book) ChooseProcedure(c ProcedureBasis, work, task string) (ProcedureConfig, *ProcedureStatus, error) {
	base := DefaultProcedure()
	dir, e := b.directory(false, "searches")
	if os.IsNotExist(e) {
		return base, nil, nil
	}
	if e != nil {
		return base, nil, e
	}
	un, e := lock(dir)
	if e != nil {
		return base, nil, e
	}
	defer un()
	j, e := readProcedureJournal(dir)
	if e != nil {
		return base, nil, e
	}
	if j.Limited {
		return base, &ProcedureStatus{Status: "limited", Policy: base.ID}, nil
	}
	l := j.Lanes[procedureKey(c)]
	if l == nil {
		return base, nil, nil
	}
	if !b.procedureCurrent(c) {
		return base, nil, nil
	}
	changed := b.refreshProcedureSources(l)
	if changed {
		if e := saveProcedureJournal(dir, &j); e != nil {
			return base, nil, e
		}
	}
	v := procedureView(l)
	if l.Limited {
		return base, &v, nil
	}
	if p := l.Candidate; p != nil && p.Origin != task && p.Config.Parent == l.Active.ID {
		if _, ok := p.Exposures[work]; ok || len(p.Exposures) < 6 {
			p.Status = "validating"
			p.Exposures[work] = task
			if e := saveProcedureJournal(dir, &j); e != nil {
				return base, nil, e
			}
			if j.Limited {
				return base, &ProcedureStatus{Status: "limited", Policy: base.ID}, nil
			}
			v.Status = "validating"
			return p.Config, &v, nil
		}
	}
	// Keep validation status out of the generating task and exhausted slots.
	v.Policy = l.Active.ID
	if l.Active.Compact > 0 {
		v.Status = "adopted"
	} else if l.Candidate != nil {
		v.Status = "baseline"
	}
	return l.Active, &v, nil
}
