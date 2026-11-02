package notebook

import (
	"os"
	"sort"
	"strconv"
	"strings"
)

// Counterexamples uses explicitly interpreted passages and recorded failures as
// an independent search lane. Relevance is not a finding of contradiction.
func (b Book) counterexamples(pages []Page, request, proposal string) ([]AssistanceCheck, error) {
	byID := map[string]Page{}
	searchable := map[string]Page{}
	origin := map[string]string{}
	failures := map[string]ProcedureOutcome{}
	for _, p := range pages {
		byID[p.ID] = p
		for i, part := range p.Passages {
			if part.Role == "counterexample" {
				q := Page{ID: "passage:" + p.ID + ":" + strconv.Itoa(i), Title: part.Quote, AppliesTo: part.Conditions, Lesson: part.Reason}
				searchable[q.ID], origin[q.ID] = q, p.ID
			}
		}
	}
	dir, err := b.directory(false, "searches")
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	if err == nil {
		j, e := readSearchJournal(dir)
		if e != nil {
			return nil, e
		}
		for _, o := range j.Outcomes {
			p, ok := byID[o.PageID]
			if !ok || p.Kind != "howto" || strings.TrimSpace(o.Failure) == "" {
				continue
			}
			// Rank each failure by its own evidence. Parent titles and learned
			// discovery text otherwise make unrelated outcomes tie for first.
			q := Page{ID: "outcome:" + o.Snapshot.ID, Title: o.Failure, AppliesTo: o.Conditions, Lesson: o.Correction, Body: o.Action}
			searchable[q.ID], origin[q.ID], failures[q.ID] = q, p.ID, o
		}
	}
	candidates := []Page{}
	for _, p := range searchable {
		candidates = append(candidates, p)
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].ID < candidates[j].ID })
	ranked, _ := rankPages(candidates, request+" "+proposal, false)
	out := []AssistanceCheck{}
	for _, candidate := range ranked {
		p := byID[origin[candidate.ID]]
		index := -1
		for i := range out {
			if out[i].Page.ID == p.ID {
				index = i
				break
			}
		}
		if index < 0 {
			if len(out) == 2 {
				continue
			}
			out = append(out, AssistanceCheck{Page: p, Relation: "counterexample_candidate"})
			index = len(out) - 1
		}
		if o, ok := failures[candidate.ID]; ok && len(out[index].Outcomes) < 2 {
			view, err := b.outcomeView(o, p, request+" "+proposal)
			if err != nil {
				return nil, err
			}
			out[index].Outcomes = append(out[index].Outcomes, view)
		}
	}
	return out, nil
}
