package notebook

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
)

// These candidates can change only loop advice, never evidence delivery, guards,
// commands, evaluation criteria, or user permissions. All checks are host-side.
type ProcedureBasis struct {
	Topic    string             `json:"topic"`
	Stage    string             `json:"stage,omitempty"`
	Articles []ArticleReference `json:"articles"`
}
type ProcedureConfig struct {
	ID          string `json:"id"`
	Parent      string `json:"parent,omitempty"`
	RepeatAfter int    `json:"repeat_after"`
	Compact     int    `json:"compact"`
}
type ProcedureStatus struct {
	Ineligible    bool            `json:"ineligible,omitempty"`
	QualityNotice string          `json:"quality_notice,omitempty"`
	Baseline      ProcedureConfig `json:"-"`
	Source        string          `json:"source,omitempty"`
	Status        string          `json:"status"`
	Policy        string          `json:"policy"`
	Cases         int             `json:"cases"`
	Required      int             `json:"required"`
	SavedBytes    int             `json:"saved_bytes,omitempty"`
}
type ProcedureTrace struct {
	Baseline      ProcedureConfig `json:"baseline"`
	Basis         ProcedureBasis  `json:"basis"`
	Work          string          `json:"work"`
	Case          string          `json:"case"`
	Config        ProcedureConfig `json:"config"`
	Status        string          `json:"status"`
	BaselineBytes int             `json:"baseline_bytes"`
	ActualBytes   int             `json:"actual_bytes"`
	Stalls        int             `json:"stalls"`
	Mixed         bool            `json:"mixed,omitempty"`
	Safe          bool            `json:"safe"`
}
type ProcedureSample struct {
	Ineligible bool           `json:"ineligible,omitempty"`
	Trace      ProcedureTrace `json:"trace"`
	Sufficient *bool          `json:"sufficient,omitempty"`
	Evidence   []Source       `json:"evidence,omitempty"`
	Snapshot   Source         `json:"snapshot"`
}
type procedureSupport struct {
	Case       string   `json:"case"`
	Identities []string `json:"identities"`
	Source     Source   `json:"source"`
	Evidence   []Source `json:"evidence"`
}
type procedureProposal struct {
	Failure          *Source            `json:"failure,omitempty"`
	Config           ProcedureConfig    `json:"config"`
	Origin           string             `json:"origin"`
	Source           Source             `json:"source"`
	Required         int                `json:"required"`
	Exposures        map[string]string  `json:"exposures"`
	Support          []procedureSupport `json:"support,omitempty"`
	SavedBytes       int                `json:"saved_bytes"`
	Status           string             `json:"status"`
	OriginIdentities []string           `json:"origin_identities,omitempty"`
}
type procedureLane struct {
	Active    ProcedureConfig     `json:"active"`
	Candidate *procedureProposal  `json:"candidate,omitempty"`
	History   []procedureProposal `json:"history,omitempty"`
	Seen      map[string]string   `json:"seen"`
	Failures  int                 `json:"failures"`
	Limited   bool                `json:"limited,omitempty"`
}
type procedureJournal struct {
	Version int                       `json:"version"`
	Lanes   map[string]*procedureLane `json:"lanes"`
	Limited bool                      `json:"limited,omitempty"`
}

func validProcedureConfig(c ProcedureConfig) bool {
	if c.ID == "loop-v1" {
		return c == DefaultProcedure()
	}
	return len(c.ID) == 21 && strings.HasPrefix(c.ID, "loop-") && idPattern.MatchString(strings.TrimPrefix(c.ID, "loop-")) && c.Parent != "" && c.RepeatAfter >= 1 && c.RepeatAfter <= 2 && c.Compact >= 1 && c.Compact <= 2
}

func DefaultProcedure() ProcedureConfig { return ProcedureConfig{ID: "loop-v1", RepeatAfter: 2} }
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
	if e != nil || o.Title != "Procedure execution" {
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
	execution, e := b.Capture(Observation{Kind: "agent", Title: "Procedure execution", Content: string(data), Reference: "procedure:" + s.Snapshot.ID, Version: o.Version, ObservedAt: o.ObservedAt})
	if e != nil {
		return Source{}, e
	}
	refs, e := b.OutcomeSources([]string{execution.ID})
	if e != nil {
		return Source{}, e
	}
	// This automatic diagnostic does not create another manual reflection.
	if _, e = b.Ingest(Draft{Sources: []string{execution.ID, s.Snapshot.ID}}); e != nil {
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
	if o.Kind != "agent" || o.Title != "Work result and remaining conditions" {
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

// The generator, held-out validation, promotion and rollback all run from one
// ordinary ending report. Checked quality remains an agent/host report, not truth.
func (b Book) RecordProcedure(s ProcedureSample) (view *ProcedureStatus, retErr error) {
	if !idPattern.MatchString(s.Trace.Work) || !idPattern.MatchString(s.Trace.Case) || s.Trace.BaselineBytes < 0 || s.Trace.BaselineBytes > 16<<20 || s.Trace.ActualBytes < 0 || s.Trace.ActualBytes > 16<<20 || len(s.Evidence) > 4 {
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
