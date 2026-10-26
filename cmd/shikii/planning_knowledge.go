package main

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/hayaenfj/shikii/internal/notebook"
	"github.com/hayaenfj/shikii/internal/store"
)

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
	seen := map[string]bool{}
	for _, id := range req.Pages {
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
		context.Read = append(context.Read, ref)
		ids := []string{}
		for _, source := range page.Sources {
			ids = append(ids, source.ID)
		}
		sources["article:"+page.ID+":"+page.Revision] = fmt.Sprintf("wiki article (a hypothesis with sources, not the current agreement)\ntitle: %s\nsummary: %s\napplies when: %s\n\n%s\n\nsources: %s", page.Title, page.Lesson, page.AppliesTo, page.Body, strings.Join(ids, ", "))
	}
	for key, text := range sources {
		if strings.HasPrefix(key, "lesson:") || strings.HasPrefix(key, "article:") {
			context.ContextBytes += len(text)
		}
	}
	context.LookupMilliseconds = float64(time.Since(started).Nanoseconds()) / 1e6
	return notice, context, nil
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
	for _, e := range context.Used {
		parts := strings.Split(e.Source, ":")
		if len(parts) != 3 {
			return errors.New("the identifier of a wiki quote is invalid")
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
