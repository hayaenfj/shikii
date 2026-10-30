package main

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/hayaenfj/shikii/internal/notebook"
	"github.com/hayaenfj/shikii/internal/store"
)

const planningBodyBudget = 8000

// Only the response omits the full inventory. The decision record retains every available revision.
func planningKnowledgeReply(k *store.KnowledgeContext) any {
	if k == nil {
		return nil
	}
	return map[string]any{"query": k.Query, "candidates": k.Candidates, "related": k.Related,
		"read": k.Read, "used": k.Used, "available_count": len(k.Available), "at": k.At,
		"lookup_milliseconds": k.LookupMilliseconds, "context_bytes": k.ContextBytes}
}

func preparePlanningKnowledge(root, scope string, req planningRequest, sources map[string]string) (string, *store.KnowledgeContext, error) {
	if len(req.Pages) > 2 {
		return "", nil, errors.New("at most 2 wiki articles can be read for a decision")
	}
	for key := range sources {
		if strings.HasPrefix(key, "lesson:") || strings.HasPrefix(key, "article:") {
			delete(sources, key)
		}
	}
	if strings.TrimSpace(req.Query) == "" && len(req.Pages) == 0 {
		// The agent chooses the topic (the request text is not used as the search words as is; a822a63). Say that nothing was searched (0.8.1)
		return planningNoQueryNotice, nil, nil
	}
	started := time.Now()
	book := notebook.Book{Root: root, Scope: scope}
	available, err := book.References()
	if err != nil {
		return "", nil, err
	}
	context := &store.KnowledgeContext{Query: req.Query, Available: available, Candidates: []store.KnowledgeReference{}, Read: []store.KnowledgeReference{}, Used: []store.KnowledgeCitation{}, At: started.UTC()}
	pages, notice := planningWikiSources(root, scope, req.Query, sources)
	for _, page := range pages {
		ref := store.KnowledgeReference{ID: page.ID, Revision: page.Revision}
		found := false
		for _, known := range available {
			if known == ref {
				found = true
			}
		}
		if !found {
			return "", nil, errors.New("the wiki revision changed during the search. Read it again")
		}
		context.Candidates = append(context.Candidates, ref)
	}
	// A project's own links lead from a procedure to its purpose. Do not search globally for goals
	// or recurse through the graph: two live, directly linked criteria are enough for this context.
	linked, links, err := planningLinkedCriteria(book, pages)
	if err != nil {
		return "", nil, err
	}
	context.Related = links
	for _, page := range linked {
		ref := store.KnowledgeReference{ID: page.ID, Revision: page.Revision}
		found := false
		for _, known := range available {
			if known == ref {
				found = true
			}
		}
		if !found {
			return "", nil, errors.New("the wiki revision changed during the search. Read it again")
		}
		addPlanningLesson(sources, page)
	}
	readPages := req.Pages
	if len(readPages) == 0 {
		choices := append(append([]notebook.Page{}, pages...), linked...)
		sort.SliceStable(choices, func(i, j int) bool { return choices[i].Kind == "criteria" && choices[j].Kind != "criteria" })
		remaining := planningBodyBudget
		for _, page := range choices {
			if page.Body == "" || len(page.Body) > remaining {
				continue
			}
			readPages = append(readPages, page.ID)
			remaining -= len(page.Body)
			if len(readPages) == 2 {
				break
			}
		}
	}
	seen := map[string]bool{}
	for _, id := range readPages {
		if len(id) != 16 || seen[id] {
			return "", nil, errors.New("give page IDs without duplicates")
		}
		seen[id] = true
		view, err := book.Query("lesson:" + id)
		if err != nil || len(view.Lessons) != 1 {
			return "", nil, errors.New("cannot read the body of a page in the same project")
		}
		page := view.Lessons[0]
		ref := store.KnowledgeReference{ID: page.ID, Revision: page.Revision}
		found := false
		for _, known := range available {
			if known == ref {
				found = true
			}
		}
		if !found {
			return "", nil, errors.New("the wiki revision changed during the search. Read it again")
		}
		// Automatic selection used an earlier snapshot. Reject a body that changed since then,
		// including growth past the budget, rather than returning a partial or different body.
		if len(req.Pages) == 0 {
			for _, selected := range append(append([]notebook.Page{}, pages...), linked...) {
				if selected.ID == id && selected.Revision != page.Revision {
					return "", nil, errors.New("the wiki revision changed during the search. Read it again")
				}
			}
		}
		context.Read = append(context.Read, ref)
		ids := []string{}
		for _, source := range page.Sources {
			ids = append(ids, source.ID)
		}
		sources["article:"+page.ID+":"+page.Revision] = fmt.Sprintf("wiki article (a hypothesis with sources, not the current agreement)\ntitle: %s\nsummary: %s\napplies when: %s\n\n%s\n\nsources: %s", page.Title, page.Lesson, page.AppliesTo, page.Body, strings.Join(ids, ", "))
		sources["article:"+page.ID+":"+page.Revision] += planningPageContext(page)
	}
	for key, text := range sources {
		if strings.HasPrefix(key, "lesson:") || strings.HasPrefix(key, "article:") {
			context.ContextBytes += len(text)
		}
	}
	context.LookupMilliseconds = float64(time.Since(started).Nanoseconds()) / 1e6
	return notice, context, nil
}

func planningLinkedCriteria(book notebook.Book, pages []notebook.Page) ([]notebook.Page, []store.KnowledgeLink, error) {
	seen, wanted := map[string]bool{}, map[string]bool{}
	for _, page := range pages {
		seen[page.ID] = true
		for _, id := range page.Related {
			wanted[id] = true
		}
	}
	if len(wanted) == 0 {
		return nil, nil, nil
	}
	live, err := book.LiveWhere(func(p notebook.Page) bool { return wanted[p.ID] && p.Kind == "criteria" })
	if err != nil {
		return nil, nil, err
	}
	byID := map[string]notebook.Page{}
	for _, page := range live {
		byID[page.ID] = page
	}
	var linked []notebook.Page
	var links []store.KnowledgeLink
	for _, from := range pages {
		for _, id := range from.Related {
			page, ok := byID[id]
			if !ok || seen[id] {
				continue
			}
			seen[id] = true
			linked = append(linked, page)
			links = append(links, store.KnowledgeLink{From: from.ID, ID: id, Revision: page.Revision})
			if len(linked) == 2 {
				return linked, links, nil
			}
		}
	}
	return linked, links, nil
}

func planningCitations(req planningRequest, context *store.KnowledgeContext) {
	if context == nil {
		return
	}
	seen := map[string]bool{}
	for _, fact := range []*planningFact{req.Agreed, req.Open} {
		if fact == nil {
			continue
		}
		for _, e := range fact.Evidence {
			if !strings.HasPrefix(e.Source, "lesson:") && !strings.HasPrefix(e.Source, "article:") {
				continue
			}
			key := e.Source + "\n" + e.Quote
			if seen[key] {
				continue
			}
			seen[key] = true
			context.Used = append(context.Used, store.KnowledgeCitation{Source: e.Source, Quote: e.Quote})
		}
	}
}

func validatePlanningKnowledge(root, scope string, context *store.KnowledgeContext) error {
	if context == nil {
		return nil
	}
	book := notebook.Book{Root: root, Scope: scope}
	linked := map[string]bool{}
	for _, ref := range context.Related {
		linked[ref.ID] = true
	}
	live, err := book.LiveWhere(func(p notebook.Page) bool { return linked[p.ID] })
	if err != nil {
		return err
	}
	liveRefs := map[store.KnowledgeReference]bool{}
	for _, p := range live {
		liveRefs[store.KnowledgeReference{ID: p.ID, Revision: p.Revision}] = true
	}
	for _, e := range context.Used {
		parts := strings.Split(e.Source, ":")
		if len(parts) != 3 {
			return errors.New("the identifier of a wiki quote is invalid")
		}
		if linked[parts[1]] && !liveRefs[store.KnowledgeReference{ID: parts[1], Revision: parts[2]}] {
			return errors.New("a quoted related criterion is no longer current. Read the context again")
		}
		view, err := book.Query("lesson:" + parts[1])
		if err != nil || len(view.Lessons) != 1 || view.Lessons[0].Revision != parts[2] {
			return errors.New("the revision of a quoted wiki page changed during the decision. Read the evidence again")
		}
	}
	return nil
}

// planningNoQueryNotice is the one-liner returned when query is called without search words. The page's "records of knowledge used in decisions" stayed at 0
// because agents were not giving search words (checked in 0.8.1: 2 of the 25 pre-work decisions in this repo).
const planningNoQueryNotice = "The wiki has not been searched yet. Add topic words likely related to this request to query (not a copy of the request text, but how the rules, existing decisions and earlier corrections are worded) and query again; lessons come back as lesson:<id>:<revision>. If you use one as evidence, write that key in the evidence's source, and give submit the same query."
