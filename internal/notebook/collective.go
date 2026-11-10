package notebook

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"
)

// Collective views point to retained wiki evidence. They do not decide which
// hypothesis is true, whether a probe succeeded, or whether an action is allowed.
type CollectiveBaseline struct {
	ArticleReference
	Premise      string `json:"premise,omitempty"`
	Availability string `json:"availability,omitempty"`
}

type KnowledgeChange struct {
	Ref              string                `json:"ref,omitempty"`
	Key              string                `json:"key"`
	Before           ArticleReference      `json:"before"`
	FromPremise      string                `json:"from_premise,omitempty"`
	FromAvailability string                `json:"from_availability,omitempty"`
	After            ArticleReference      `json:"after"`
	Premise          string                `json:"premise,omitempty"`
	Status           string                `json:"status"`
	Fields           []string              `json:"fields"`
	Reason           string                `json:"reason,omitempty"`
	Sources          []string              `json:"sources,omitempty"`
	Queries          []string              `json:"queries"`
	Differences      []KnowledgeDifference `json:"differences,omitempty"`
	Omitted          bool                  `json:"omitted,omitempty"`
}

type KnowledgeDifference struct {
	Field  string `json:"field"`
	Before string `json:"before"`
	After  string `json:"after"`
}

type KnowledgeContrast struct {
	Article       ArticleReference       `json:"article"`
	Sides         []ExperienceConnection `json:"sides,omitempty"`
	Sources       [][]string             `json:"sources,omitempty"`
	Independence  string                 `json:"independence"`
	Judgment      string                 `json:"judgment"`
	Conditions    string                 `json:"conditions"`
	Unknown       []string               `json:"unknown"`
	Recheck       []string               `json:"recheck,omitempty"`
	Query         string                 `json:"query"`
	Omitted       bool                   `json:"omitted,omitempty"`
	Evidence      string                 `json:"evidence,omitempty"`
	Reports       []ProcedureOutcomeView `json:"reports,omitempty"`
	ReportQueries []string               `json:"report_queries,omitempty"`
}

type Investigation struct {
	Article        ArticleReference `json:"article"`
	Key            string           `json:"key"`
	Question       string           `json:"question,omitempty"`
	Method         string           `json:"method,omitempty"`
	ReconsiderWhen string           `json:"reconsider_when,omitempty"`
	Results        []ProbeResult    `json:"results,omitempty"`
	ResultCount    int              `json:"result_count"`
	Query          string           `json:"query"`
	Omitted        bool             `json:"omitted,omitempty"`
	Evidence       string           `json:"evidence,omitempty"`
}

// ReuseLearning avoids repeating exact evidence already supplied by the caller.
func (v *CollectiveContext) ReuseLearning(paths map[string]string) {
	for i := range v.Contrasts {
		c := &v.Contrasts[i]
		if path := paths[c.Article.ID+"@"+c.Article.Revision]; path != "" && len(c.ReportQueries) == 0 {
			c.Sides = nil
			c.Evidence = path + ".connections"
			c.Omitted = false
		}
	}
	for i := range v.Investigations {
		q := &v.Investigations[i]
		if path := paths[q.Article.ID+"@"+q.Article.Revision]; path != "" {
			q.Question = ""
			q.Method = ""
			q.ReconsiderWhen = ""
			q.Results = nil
			q.Evidence = path + ".probe"
			q.Omitted = false
		}
	}
}

type CollectiveContext struct {
	Updates        []KnowledgeChange   `json:"updates"`
	Contrasts      []KnowledgeContrast `json:"contrasts"`
	Investigations []Investigation     `json:"investigations"`
	Omitted        map[string]int      `json:"omitted,omitempty"`
	Recovery       []string            `json:"recovery,omitempty"`
	Notice         string              `json:"notice"`
}

func collectiveKey(v any) string {
	raw, _ := json.Marshal(v)
	return fmt.Sprintf("%x", sha256.Sum256(raw))[:16]
}

func (b Book) CollectiveBaseline(ref ArticleReference) CollectiveBaseline {
	base := CollectiveBaseline{ArticleReference: ref, Availability: "live"}
	live, err := b.LiveWhere(func(p Page) bool { return p.ID == ref.ID })
	if err == nil && len(live) == 1 && live[0].Revision == ref.Revision {
		base.Premise = collectiveKey(live[0].Recheck)
	}
	return base
}

// The confirmation baseline must describe what was actually supplied, rather
// than files reread later when an agent records its inspection.
func SuppliedCollectiveBaseline(ref ArticleReference, changes []PremiseChange) CollectiveBaseline {
	return CollectiveBaseline{ArticleReference: ref, Premise: collectiveKey(changes), Availability: "live"}
}

func articleQuery(r ArticleReference) string { return "lesson:" + r.ID + "@" + r.Revision }

// At most 32 known/selected article identities are inspected. No unrelated
// article becomes evidence merely because another agent changed it.
func (b Book) Collective(known []CollectiveBaseline, selected []ArticleReference) (CollectiveContext, error) {
	b = b.scopedRead()
	v := CollectiveContext{Updates: []KnowledgeChange{}, Contrasts: []KnowledgeContrast{}, Investigations: []Investigation{}, Omitted: map[string]int{},
		Notice: "Relevant wiki hypotheses, never permission, usefulness or a human truth. Compare conditions, quotes and sources. Different sources do not establish independence. Inspect queries for omitted evidence. Investigations are opportunities, not commands; return checked results to the same wiki probe. Review change refs only after inspection."}
	ids := map[string]bool{}
	for _, r := range known {
		if len(ids) < 32 {
			ids[r.ID] = true
		}
	}
	for _, r := range selected {
		if len(ids) < 32 {
			ids[r.ID] = true
		}
	}
	live, err := b.LiveWhere(func(p Page) bool { return ids[p.ID] })
	if err != nil {
		return v, err
	}
	current := map[string]Page{}
	for _, p := range live {
		current[p.ID] = p
	}
	omit := func(kind string, r ArticleReference) {
		v.Omitted[kind]++
		q := articleQuery(r)
		for _, old := range v.Recovery {
			if old == q {
				return
			}
		}
		v.Recovery = append(v.Recovery, q)
	}
	for _, base := range known {
		if !ids[base.ID] {
			omit("candidates", base.ArticleReference)
			continue
		}
		p, ok := current[base.ID]
		now := ArticleReference{ID: base.ID, Revision: p.Revision}
		premise := collectiveKey(p.Recheck)
		if !ok && base.Availability == "unavailable" {
			continue
		}
		if ok && base.Availability != "unavailable" && base.Revision == p.Revision && (base.Premise == "" || base.Premise == premise) {
			continue
		}
		u := KnowledgeChange{Before: base.ArticleReference, FromPremise: base.Premise, FromAvailability: base.Availability, After: now, Premise: premise, Status: "revised", Fields: []string{}, Queries: []string{articleQuery(base.ArticleReference)}}
		if !ok {
			u.Status = "unavailable"
			u.After.Revision = ""
			u.Queries = append(u.Queries, "lesson:"+base.ID)
		} else {
			u.Queries = append(u.Queries, articleQuery(now))
			if base.Revision == p.Revision {
				u.Status = "premise_changed"
				u.Fields = append(u.Fields, "recheck")
			}
			if base.Availability == "unavailable" {
				u.Status = "restored"
			}
			old, e := b.readRevision(articleQuery(base.ArticleReference))
			if e != nil {
				u.Status = "base_unavailable"
			} else {
				a, z := reconcileFields(old), reconcileFields(p)
				for _, field := range []string{"title", "lesson", "applies_to", "body", "rationale", "kind", "scene"} {
					if a[field] != z[field] {
						u.Fields = append(u.Fields, field)
						if len(a[field])+len(z[field]) <= 400 && len(u.Differences) < 2 {
							u.Differences = append(u.Differences, KnowledgeDifference{field, a[field], z[field]})
						} else {
							u.Omitted = true
						}
					}
				}
				for name, pair := range map[string][2]any{"learning": {old.Learning, p.Learning}, "passages": {old.Passages, p.Passages}, "related": {old.Related, p.Related}, "sources": {old.Sources, p.Sources}} {
					if collectiveKey(pair[0]) != collectiveKey(pair[1]) {
						u.Fields = append(u.Fields, name)
						u.Omitted = true
					}
				}
				sort.Strings(u.Fields)
			}
			u.Reason = p.ChangeReason
			for _, s := range p.Sources {
				u.Sources = append(u.Sources, s.ID)
			}
		}
		u.Key = collectiveKey(u)
		if len(v.Updates) >= 2 {
			omit("updates", base.ArticleReference)
			continue
		}
		if len(u.Reason) > 500 {
			u.Reason = ""
			u.Omitted = true
		}
		if len(u.Sources) > 4 {
			u.Sources = nil
			u.Omitted = true
		}
		v.Updates = append(v.Updates, u)
	}
	seen := map[string]bool{}
	var unreadableContrast *KnowledgeContrast // a procedure whose report cannot be read: shown only when no other contrast is
	for _, r := range selected {
		if seen[r.ID] {
			continue
		}
		seen[r.ID] = true
		if !ids[r.ID] {
			omit("candidates", r)
			continue
		}
		p, ok := current[r.ID]
		if !ok || p.Revision != r.Revision {
			continue
		}
		if p.Kind == "howto" && len(v.Contrasts) == 0 {
			// A report that can no longer be checked is listed as unreadable: one lost source must not fail the
			// preparation of every work that finds this procedure.
			reports, e := b.selectOutcomes(p, "", false, true)
			if e != nil {
				return v, e
			}
			unreadable := len(reports) == 2 && (reports[0].unreadable || reports[1].unreadable)
			if len(reports) == 2 && (unreadable || reports[0].Result != reports[1].Result || reports[0].Failure != reports[1].Failure) {
				c := KnowledgeContrast{Article: r, Reports: reports, ReportQueries: []string{reports[0].SourceID, reports[1].SourceID}, Independence: "not_established", Judgment: "uninterpreted", Conditions: "different_text", Query: articleQuery(r), Unknown: []string{"Different reported results are not proof of contradiction or success. Interpret shared conditions, remaining differences and the cited evidence."}}
				if reports[0].Conditions == reports[1].Conditions {
					c.Conditions = "same_text"
				}
				if unreadable {
					// Nothing of an unchecked report is compared: no difference and no agreement is claimed.
					c.Conditions = "not_compared"
					c.Unknown = []string{"A report of this procedure cannot be read, so the two reports cannot be compared and no difference is established. Recover the saved sources that report names before relying on it."}
				}
				for _, report := range reports {
					ss := []string{}
					for _, e := range report.Evidence {
						ss = append(ss, e.SourceID)
					}
					c.Sources = append(c.Sources, ss)
				}
				for _, a := range c.Sources[0] {
					for _, z := range c.Sources[1] {
						if a == z {
							c.Independence = "shared_sources"
						}
					}
				}
				if unreadable {
					// it says only that a report must be recovered: a contrast that can be read, of this or another article, takes
					// the one place first, and this one is shown when there is none
					if unreadableContrast == nil {
						unreadableContrast = &c
					}
				} else {
					v.Contrasts = append(v.Contrasts, c)
				}
			}
		}
		if p.Learning == nil {
			continue
		}
		l := p.Learning
		var support, counter *ExperienceConnection
		for i := range l.Connections {
			c := &l.Connections[i]
			if c.Relation == "supports" && support == nil {
				support = c
			}
			if c.Relation == "counterexample" && counter == nil {
				counter = c
			}
		}
		if support != nil && counter != nil {
			if len(v.Contrasts) >= 1 {
				omit("contrasts", r)
			} else {
				c := KnowledgeContrast{Article: r, Sides: []ExperienceConnection{*support, *counter}, Independence: "not_established", Judgment: "uninterpreted", Query: articleQuery(r), Conditions: "different_text", Unknown: []string{"The host must interpret common and differing conditions, whether these address the same question, and which observation distinguishes them."}}
				if support.Conditions == counter.Conditions {
					c.Conditions = "same_text"
				}
				for _, side := range c.Sides {
					past, e := b.readRevision(articleQuery(ArticleReference{side.ID, side.Revision}))
					ss := []string{}
					if e != nil || !uniqueLearningQuote(past.Body, side.Quote) {
						c.Recheck = append(c.Recheck, "The connected revision or quote cannot be verified.")
					} else {
						for _, s := range past.Sources {
							ss = append(ss, s.ID)
						}
					}
					c.Sources = append(c.Sources, ss)
				}
				for _, a := range c.Sources[0] {
					for _, z := range c.Sources[1] {
						if a == z {
							c.Independence = "shared_sources"
						}
					}
				}
				if ctx := b.LearningContext(p, 0); ctx != nil {
					c.Recheck = append(c.Recheck, ctx.Recheck...)
				}
				v.Contrasts = append(v.Contrasts, c)
			}
		}
		if q := l.Probe; q != nil {
			if len(v.Investigations) >= 1 {
				omit("investigations", r)
			} else {
				key := collectiveKey([]string{p.ID, q.Question, q.Method, q.ReconsiderWhen})
				v.Investigations = append(v.Investigations, Investigation{Article: r, Key: key, Question: q.Question, Method: q.Method, ReconsiderWhen: q.ReconsiderWhen, Results: q.Results, ResultCount: len(q.Results), Query: articleQuery(r)})
			}
		}
	}
	if unreadableContrast != nil && len(v.Contrasts) == 0 {
		v.Contrasts = append(v.Contrasts, *unreadableContrast)
	}
	// Keep exact quotes whole. When they cannot fit, return the exact revision
	// query rather than shortening a condition into a different assertion.
	size := func() int { raw, _ := json.Marshal(v); return len(raw) + 80*len(v.Updates) }
	if size() > 4096 {
		for i := range v.Investigations {
			q := &v.Investigations[i]
			q.Results = nil
			q.Question = ""
			q.Method = ""
			q.ReconsiderWhen = ""
			q.Omitted = true
		}
	}
	if size() > 4096 {
		for i := range v.Contrasts {
			c := &v.Contrasts[i]
			c.Sides = nil
			c.Sources = nil
			c.Recheck = nil
			c.Reports = nil
			c.Omitted = true
		}
	}
	if size() > 4096 {
		for i := range v.Updates {
			v.Updates[i].Reason = ""
			v.Updates[i].Sources = nil
			v.Updates[i].Differences = nil
			v.Updates[i].Omitted = true
		}
	}
	if size() > 4096 {
		return CollectiveContext{}, fmt.Errorf("collective context exceeds 4 KiB; use a narrower query")
	}
	return v, nil
}
