package main

import (
	"errors"
	"sort"
	"strconv"
	"strings"

	"github.com/hayaenfj/shikii/internal/notebook"
)

type workCollective struct {
	Baseline       map[string]notebook.CollectiveBaseline `json:"baseline"`
	Delivered      map[string]notebook.KnowledgeChange    `json:"delivered"`
	Confirmed      map[string]notebook.CollectiveBaseline `json:"confirmed"`
	Next           int                                    `json:"next"`
	Investigations map[string]bool                        `json:"investigations,omitempty"`
	Exposures      map[string]notebook.CollectiveBaseline `json:"exposures,omitempty"`
}

func preparedCollective(book notebook.Book, w *workState, seen []notebook.CollectiveBaseline, entries []any) (notebook.CollectiveContext, error) {
	if w.Collective == nil {
		w.Collective = &workCollective{Baseline: map[string]notebook.CollectiveBaseline{}, Delivered: map[string]notebook.KnowledgeChange{}, Confirmed: map[string]notebook.CollectiveBaseline{}}
		relevant := map[string]bool{}
		if strings.HasPrefix(w.Query, "lesson:") {
			id, _, _ := strings.Cut(strings.TrimPrefix(w.Query, "lesson:"), "@")
			relevant[id] = true
		}
		for _, raw := range entries {
			e := raw.(map[string]any)
			id, _ := e["id"].(string)
			relevant[id] = true
		}
		for _, r := range seen {
			if relevant[r.ID] && len(w.Collective.Baseline) < 64 {
				w.Collective.Baseline[r.ID] = r
			}
		}
	}
	c := w.Collective
	if c.Baseline == nil || c.Delivered == nil || c.Confirmed == nil {
		return notebook.CollectiveContext{}, errors.New("the collective references are incomplete; prepare again")
	}
	if c.Exposures == nil {
		c.Exposures = map[string]notebook.CollectiveBaseline{}
	}
	for _, raw := range entries {
		e := raw.(map[string]any)
		ref, _ := e["ref"].(string)
		if r, ok := w.Articles[ref]; ok {
			if changes, ok := e["recheck"].([]notebook.PremiseChange); ok || e["recheck"] == nil {
				c.Exposures[ref] = notebook.SuppliedCollectiveBaseline(notebook.ArticleReference{ID: r.ID, Revision: r.Revision}, changes)
			}
		}
	}
	refs := make([]string, 0, len(w.Articles))
	for ref := range w.Articles {
		refs = append(refs, ref)
	}
	sort.Slice(refs, func(i, j int) bool {
		a, _ := strconv.Atoi(strings.TrimPrefix(refs[i], "knowledge"))
		z, _ := strconv.Atoi(strings.TrimPrefix(refs[j], "knowledge"))
		return a < z
	})
	for _, ref := range refs {
		r := w.Articles[ref]
		if _, ok := c.Baseline[r.ID]; !ok {
			if len(c.Baseline) >= 64 {
				return notebook.CollectiveContext{}, errors.New("this work has reached its 64 collective article limit; prepare a new context")
			}
			base, ok := c.Exposures[ref]
			if !ok {
				base = notebook.CollectiveBaseline{ArticleReference: notebook.ArticleReference{ID: r.ID, Revision: r.Revision}, Availability: "live"}
			}
			c.Baseline[r.ID] = base
		}
	}
	known := []notebook.CollectiveBaseline{}
	ids := []string{}
	for id := range c.Baseline {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		known = append(known, c.Baseline[id])
	}
	selected := []notebook.ArticleReference{}
	paths := map[string]string{}
	for _, raw := range entries {
		e := raw.(map[string]any)
		id, _ := e["id"].(string)
		revision, _ := e["revision"].(string)
		if id != "" && revision != "" && e["collective_excluded"] != true {
			selected = append(selected, notebook.ArticleReference{ID: id, Revision: revision})
			if lc, ok := e["learning_context"].(*notebook.LearningContext); ok && lc != nil && lc.Learning != nil && !lc.Omitted {
				paths[id+"@"+revision] = e["ref"].(string) + ".learning_context.learning"
			}
			if path, ok := e["learning_evidence"].(string); ok && path != "" {
				paths[id+"@"+revision] = path
			}
		}
	}
	v, err := book.Collective(known, selected)
	if err != nil {
		return v, err
	}
	if c.Investigations == nil {
		c.Investigations = map[string]bool{}
	}
	questions := v.Investigations[:0]
	for _, q := range v.Investigations {
		key := q.Key + "@" + q.Article.Revision
		if !c.Investigations[key] {
			if len(c.Investigations) >= 64 {
				return v, errors.New("this work has reached its 64 investigation delivery limit; prepare a new context")
			}
			c.Investigations[key] = true
			questions = append(questions, q)
		}
	}
	v.Investigations = questions
	v.ReuseLearning(paths)
	for i := range v.Updates {
		u := &v.Updates[i]
		for ref, prior := range c.Delivered {
			if prior.Key == u.Key {
				u.Ref = ref
				break
			}
		}
		if u.Ref == "" {
			if len(c.Delivered) >= 64 {
				return v, errors.New("this work has reached its 64 change reference limit; review pending changes or prepare a new context")
			}
			c.Next++
			u.Ref = "change" + strconv.Itoa(c.Next)
			c.Delivered[u.Ref] = *u
		}
	}
	if workContextSize(v) > 4096 {
		return notebook.CollectiveContext{}, errors.New("collective context exceeds 4 KiB after binding references; use a narrower query")
	}
	return v, nil
}

// Acknowledgement advances only the inspected revision/premise. It never records
// usefulness, semantic agreement, an outcome or a human truth.
func reviewCollective(book notebook.Book, w *workState, ref string) error {
	if w.Collective == nil {
		return errors.New("unknown collective reference")
	}
	c := w.Collective
	if strings.HasPrefix(ref, "knowledge") {
		r, ok := w.Articles[ref]
		if !ok {
			return errors.New("unknown prepared article reference")
		}
		base, ok := c.Exposures[ref]
		if !ok {
			return errors.New("the supplied premise state is unavailable; read this article again before confirming it")
		}
		if _, ok := c.Confirmed[r.ID]; !ok {
			c.Confirmed[r.ID] = base
		}
		return nil
	}
	u, ok := c.Delivered[ref]
	if !ok {
		return errors.New("unknown prepared change reference")
	}
	current := c.Baseline[u.Before.ID]
	if current.Revision != u.Before.Revision || current.Premise != u.FromPremise || current.Availability != u.FromAvailability {
		return nil
	}
	// Keep the delivered binding for retry and advance to exactly its after
	// revision. A newer peer edit remains a new change at the next preparation.
	base := notebook.CollectiveBaseline{ArticleReference: u.After, Premise: u.Premise, Availability: "live"}
	if u.After.Revision == "" {
		base.ArticleReference = u.Before
		base.Availability = "unavailable"
	}
	c.Baseline[u.Before.ID] = base
	c.Confirmed[u.Before.ID] = base
	return nil
}

func retainCollectiveSeen(s *hookSession, w *workState) {
	if w.Collective == nil || len(w.Collective.Confirmed) == 0 {
		return
	}
	byID := map[string]notebook.CollectiveBaseline{}
	for _, r := range s.KnowledgeSeen {
		byID[r.ID] = r
	}
	for id, r := range w.Collective.Confirmed {
		byID[id] = r
	}
	ids := []string{}
	for id := range byID {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	s.KnowledgeSeen = nil
	// New confirmed references take precedence over older session entries.
	for _, id := range ids {
		if _, ok := w.Collective.Confirmed[id]; ok {
			s.KnowledgeSeen = append(s.KnowledgeSeen, byID[id])
		}
	}
	for _, id := range ids {
		if len(s.KnowledgeSeen) >= 64 {
			break
		}
		if _, ok := w.Collective.Confirmed[id]; !ok {
			s.KnowledgeSeen = append(s.KnowledgeSeen, byID[id])
		}
	}
}
