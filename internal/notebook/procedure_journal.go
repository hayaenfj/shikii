package notebook

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"sort"
)

func procedureKey(b ProcedureBasis) string {
	b.Articles = slices.Clone(b.Articles)
	sort.Slice(b.Articles, func(i, j int) bool { return b.Articles[i].ID < b.Articles[j].ID })
	data, _ := json.Marshal(b)
	return digest(data)
}

func (b Book) procedureCurrent(c ProcedureBasis) bool {
	if !ValidAssistanceText(c.Topic, true) || len(c.Topic) > 1500 || len(c.Stage) > HostStageLimit || len(c.Articles) < 1 || len(c.Articles) > 8 {
		return false
	}
	seen := map[string]bool{}
	for _, r := range c.Articles {
		if seen[r.ID] || !idPattern.MatchString(r.ID) || !idPattern.MatchString(r.Revision) {
			return false
		}
		seen[r.ID] = true
		v, e := b.Query("lesson:" + r.ID)
		if e != nil || len(v.Lessons) != 1 || v.Lessons[0].Status == "deleted" || v.Lessons[0].Revision != r.Revision || len(v.Lessons[0].Recheck) > 0 {
			return false
		}
	}
	return true
}

func readProcedureJournal(dir string) (procedureJournal, error) {
	j := procedureJournal{Version: 1, Lanes: map[string]*procedureLane{}}
	data, e := readFile(filepath.Join(dir, ".procedure-policy-v1.json"))
	if os.IsNotExist(e) {
		return j, nil
	}
	if e != nil {
		return j, e
	}
	if json.Unmarshal(data, &j) != nil || j.Version != 1 || j.Lanes == nil || len(j.Lanes) > 32 {
		return j, errors.New("invalid bounded procedure policy journal")
	}
	for _, l := range j.Lanes {
		if l == nil || l.Seen == nil || len(l.Seen) > 64 || len(l.History) > 4 || !validProcedureConfig(l.Active) {
			return j, errors.New("invalid procedure policy lane")
		}
		proposals := slices.Clone(l.History)
		if l.Candidate != nil {
			proposals = append(proposals, *l.Candidate)
		}
		ancestors := map[string]ProcedureConfig{DefaultProcedure().ID: DefaultProcedure()}
		for _, p := range proposals {
			if !validProcedureConfig(p.Config) || p.Exposures == nil || len(p.Exposures) > 6 || len(p.Support) > 4 || p.Required < 2 || p.Required > 4 || !idPattern.MatchString(p.Origin) {
				return j, errors.New("invalid bounded procedure proposal")
			}
			parent, ok := ancestors[p.Config.Parent]
			if !ok || p.Config.Compact <= parent.Compact {
				return j, errors.New("a procedure must extend an earlier, simpler parent")
			}
			if _, exists := ancestors[p.Config.ID]; exists {
				return j, errors.New("duplicate procedure policy")
			}
			ancestors[p.Config.ID] = p.Config
		}
		if _, ok := ancestors[l.Active.ID]; !ok {
			return j, errors.New("active procedure has no retained validation")
		}
	}
	return j, nil
}

func saveProcedureJournal(dir string, j *procedureJournal) error {
	data, e := json.Marshal(j)
	if e != nil {
		return e
	}
	if len(data) > 60<<10 {
		// Retain prior evidence and stop optimization before the hard file bound.
		// The new original report remains separately durable and unevaluated.
		old, err := readProcedureJournal(dir)
		if err != nil {
			return err
		}
		old.Limited = true
		*j = old
		data, e = json.Marshal(j)
		if e != nil {
			return e
		}
	}
	return writeFile(filepath.Join(dir, ".procedure-policy-v1.json"), data)
}

func procedureView(l *procedureLane) ProcedureStatus {
	v := ProcedureStatus{Status: "baseline", Policy: l.Active.ID, Required: min(4, 2+l.Failures)}
	v.Baseline = DefaultProcedure()
	if l.Active.Compact > 0 {
		v.Status = "adopted"
		for _, p := range l.History {
			if p.Config.ID == l.Active.ID {
				v.Cases, v.Required, v.SavedBytes = len(p.Support), p.Required, p.SavedBytes
				for _, old := range l.History {
					if old.Config.ID == p.Config.Parent {
						v.Baseline = old.Config
					}
				}
			}
		}
	}
	if len(l.History) > 0 {
		p := l.History[len(l.History)-1]
		if p.Status == "rolled_back" || p.Status == "rejected" {
			v.Status = p.Status
		}
	}
	if p := l.Candidate; p != nil {
		v.Status, v.Policy, v.Cases, v.Required, v.SavedBytes = p.Status, p.Config.ID, len(p.Support), p.Required, p.SavedBytes
		v.Baseline = l.Active
	}
	if l.Limited {
		v.Status = "limited"
	}
	return v
}
