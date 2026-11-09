package notebook

import (
	"strings"

	"github.com/hayaenfj/shikii/internal/store"
)

func (b Book) Query(query string) (View, error) {
	b = b.scopedRead()
	v, err := b.query(query, false)
	if strings.HasPrefix(query, "lesson:") && strings.Contains(query, "@") {
		return v, err
	}
	for i := range v.Lessons {
		v.Lessons[i].Recheck = b.premiseChanges(v.Lessons[i])
	}
	return v, err
}

// Recall is for lessons delivered automatically. It weighs the title and applies-to condition over chance matches in the body.
// Query, for manual search, still searches the body as before.
// Live returns the pages usable in search (sources intact and not replaced), without ranking by search terms. Candidates for choosing where to insert.
func (b Book) Live() ([]Page, error) {
	return b.LiveWhere(nil)
}

// LiveWhere is Live limited to the pages keep accepts (nil keeps all). keep runs before the sources are checked, so a narrow keep
// reads and hashes only the sources of the pages it accepts (the delivery right before a tool call runs on every edit, 0.11).
func (b Book) LiveWhere(keep func(Page) bool) ([]Page, error) {
	b = b.scopedRead()
	pages, _, err := b.pages()
	if err != nil {
		return nil, err
	}
	superseded := map[string]bool{}
	for _, p := range pages {
		for _, id := range p.Replaces {
			superseded[id] = true
		}
	}
	live := []Page{}
	for _, p := range pages {
		if p.Status != "deleted" && !superseded[p.ID] && (keep == nil || keep(p)) && len(b.sourceIssues(p)) == 0 {
			p.Recheck = b.premiseChanges(p)
			live = append(live, p)
		}
	}
	return live, nil
}

// RecallCandidates is the cap on automatic-delivery candidates. 2 are delivered after the caller removes revisions it already delivered (audit 2026-10-23: cutting before removing lets them take the seats).
const RecallCandidates = 6

// AlwaysScenes are the ways to write the scene of a "rule that applies to every request" (the Japanese and English the guide fixed).
var AlwaysScenes = []string{"いつも", "always"}

// IsAlways reports whether scene is one of AlwaysScenes (case-insensitive for English).
func IsAlways(scene string) bool {
	for _, a := range AlwaysScenes {
		if strings.EqualFold(strings.TrimSpace(scene), a) {
			return true
		}
	}
	return false
}

// AlwaysRules returns the rules decided in this project (rule) whose scene is "always" (in the order the pages were read). Unreadable pages, pages missing sources and replaced pages are not returned.
// This is what goes to subagents whose task text is unknown (0.9.1): they cannot be chosen by closeness to the request, so only rules that apply to every request
func (b Book) AlwaysRules() ([]Page, error) {
	pages, _, err := b.pages()
	if err != nil {
		return nil, err
	}
	superseded := map[string]bool{}
	valid := []Page{}
	for _, p := range pages {
		for _, id := range p.Replaces {
			superseded[id] = true
		}
		if len(b.sourceIssues(p)) > 0 {
			continue
		}
		valid = append(valid, p)
	}
	out := []Page{}
	for _, p := range valid {
		if p.Status != "deleted" && !superseded[p.ID] && p.Kind == "rule" && IsAlways(p.Scene) {
			p.Recheck = b.premiseChanges(p)
			out = append(out, p)
		}
	}
	return out, nil
}

func (b Book) Recall(query string) ([]Page, error) {
	pages, _, err := b.RecallDetailed(query)
	return pages, err
}

func (b Book) query(query string, recall bool) (View, error) {
	v := View{Lessons: []Page{}, Pending: []store.Feedback{}, Issues: []Issue{}}
	if !recall && strings.HasPrefix(query, "lesson:") && strings.Contains(query, "@") {
		p, err := b.readRevision(query)
		if err != nil {
			return v, err
		}
		v.Lessons = append(v.Lessons, p)
		return v, nil
	}
	pages, issues, err := b.pages()
	if err != nil {
		return v, err
	}
	v.Issues = append(v.Issues, issues...)
	superseded := map[string]bool{}
	valid := []Page{}
	exact := !recall && strings.HasPrefix(query, "lesson:")
	for _, p := range pages {
		for _, id := range p.Replaces {
			superseded[id] = true
		}
		bad := b.sourceIssues(p)
		v.Issues = append(v.Issues, bad...)
		if len(bad) > 0 {
			continue
		}
		valid = append(valid, p)
		// A direct reference can also read pages from before replacement. Source checks are not skipped.
		if !recall && query == "lesson:"+p.ID {
			return View{Lessons: []Page{p}, Pending: v.Pending, Issues: v.Issues}, nil
		}
	}
	for _, p := range valid {
		// For automatic delivery (recall), drop history pages before ranking (dropping them after cutting the candidates lets history take the seats and rules do not arrive; audit 0.9.1 medium-3)
		if p.Status != "deleted" && !superseded[p.ID] && !exact {
			v.Lessons = append(v.Lessons, p)
		}
	}
	if query != "" || recall {
		contexts, err := b.discoveryContexts()
		if err != nil {
			return v, err
		}
		for i := range v.Lessons {
			p := &v.Lessons[i]
			p.discovered = contexts[p.ID+":"+p.Revision]
		}
		v.Lessons, v.Matches = rankPages(v.Lessons, query, recall)
	}
	if recall {
		// History stays in the count for rare words, and is ranked but dropped before cutting
		lessons, matches := v.Lessons[:0:0], v.Matches[:0:0]
		for i, p := range v.Lessons {
			if p.Kind != "history" {
				lessons = append(lessons, p)
				if i < len(v.Matches) {
					matches = append(matches, v.Matches[i])
				}
			}
		}
		v.Lessons, v.Matches = lessons, matches
	}
	if query != "" && !recall {
		v.Search = &SearchResult{Total: len(v.Lessons), Candidates: []SearchCandidate{}}
		for i, page := range v.Lessons {
			v.Search.Candidates = append(v.Search.Candidates, SearchCandidate{Match: v.Matches[i], Title: page.Title, AppliesTo: page.AppliesTo})
		}
	}
	limit := len(v.Lessons)
	if query != "" && !recall {
		limit = 2
	}
	if recall {
		limit = RecallCandidates // the caller (memoryContext) cuts to 2 after removing revisions it already delivered
	}
	if query == "" && !recall {
		pending, err := b.PendingObservations()
		if err != nil {
			return v, err
		}
		v.Observations = Heads(pending)
		v.Maintenance, err = b.Maintenance()
		if err != nil {
			return v, err
		}
	}
	if len(v.Lessons) > limit {
		v.Lessons = v.Lessons[:limit]
		if len(v.Matches) > limit {
			v.Matches = v.Matches[:limit]
		}
	}
	if recall {
		return v, nil
	}
	v.Pending, err = b.PendingSources()
	if len(v.Pending) > 2 {
		v.Pending = v.Pending[:2]
	}
	return v, err
}

// PendingSources returns source records from oldest. Store limits how much history is read.
func (b Book) PendingSources() ([]store.Feedback, error) {
	recent, err := b.RecentFeedback()
	if err != nil {
		return nil, err
	}
	return b.PendingSourcesFromFeedback(recent)
}
