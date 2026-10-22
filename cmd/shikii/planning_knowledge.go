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
		return "", nil, errors.New("判断に読むwiki本文は最大2記事です")
	}
	for key := range sources {
		if strings.HasPrefix(key, "lesson:") || strings.HasPrefix(key, "article:") {
			delete(sources, key)
		}
	}
	if strings.TrimSpace(req.Query) == "" && len(req.Pages) == 0 {
		return "", nil, nil
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
			return "", nil, errors.New("検索中にwikiの版が変わりました。読み直してください")
		}
		context.Candidates = append(context.Candidates, ref)
	}
	seen := map[string]bool{}
	for _, id := range req.Pages {
		if len(id) != 16 || seen[id] {
			return "", nil, errors.New("重複のない記事IDを指定してください")
		}
		seen[id] = true
		view, err := book.Query("lesson:" + id)
		if err != nil || len(view.Lessons) != 1 {
			return "", nil, errors.New("同じプロジェクトの記事の本文を確認できません")
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
			return "", nil, errors.New("検索中にwikiの版が変わりました。読み直してください")
		}
		context.Read = append(context.Read, ref)
		ids := []string{}
		for _, source := range page.Sources {
			ids = append(ids, source.ID)
		}
		sources["article:"+page.ID+":"+page.Revision] = fmt.Sprintf("wiki本文（出典付きの仮説・現在の合意ではない）\n題: %s\n要約: %s\n適用条件: %s\n\n%s\n\n出典: %s", page.Title, page.Lesson, page.AppliesTo, page.Body, strings.Join(ids, ", "))
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
			return errors.New("wikiの引用の識別子が不正です")
		}
		view, err := book.Query("lesson:" + parts[1])
		if err != nil || len(view.Lessons) != 1 || view.Lessons[0].Revision != parts[2] {
			return errors.New("判定中に引用したwikiの版が変わりました。根拠を読み直してください")
		}
	}
	return nil
}
