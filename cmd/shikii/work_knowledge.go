package main

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/hayaenfj/shikii/internal/notebook"
	"github.com/hayaenfj/shikii/internal/store"
)

// Excerpts are exact passages of a pinned revision, not generated conclusions.
// Keep full-body recovery and explicit incompleteness beside the passages.
func workLearningBudget(query string) int {
	if strings.HasPrefix(query, "lesson:") && strings.Contains(query, "@") {
		return 12 << 10
	}
	return 2000
}

// A single bounded context carries learning on both initial and extended reads.
// Do not leave a second unbounded copy inside the supplied article.
func setWorkLearning(book notebook.Book, entry map[string]any, p notebook.Page, query string) {
	entry["learning_context"] = book.LearningContext(p, workLearningBudget(query))
	if article, ok := entry["article"].(notebook.Page); ok {
		article.Learning = nil
		entry["article"] = article
	}
}

// A pinned current revision keeps live premise warnings. Historical revisions
// must not inherit warnings belonging to a different current article.
func currentWorkPremise(book notebook.Book, p notebook.Page) (notebook.Page, error) {
	current, err := book.Query("lesson:" + p.ID)
	if err != nil {
		return p, err
	}
	if len(current.Lessons) == 1 && current.Lessons[0].Revision == p.Revision {
		p.Recheck = current.Lessons[0].Recheck
	}
	return p, nil
}

func focusWorkKnowledge(book notebook.Book, entries []any, query, context string, quoted map[string]string) error {
	budget := 16000
	for _, raw := range entries {
		e := raw.(map[string]any)
		id, _ := e["id"].(string)
		revision, _ := e["revision"].(string)
		p, supplied := e["article"].(notebook.Page)
		if !supplied {
			v, err := book.Query("lesson:" + id + "@" + revision)
			if err != nil {
				return err
			}
			if len(v.Lessons) != 1 {
				return errors.New("a prepared article revision is unavailable; read its exact query")
			}
			p = v.Lessons[0]
		}
		e["rationale"] = p.Rationale
		setWorkLearning(book, e, p, query)
		p, err := currentWorkPremise(book, p)
		if err != nil {
			return err
		}
		e["recheck"] = p.Recheck
		orientation := notebook.Orient(p, context, min(budget, 2400))
		e["orientation"] = orientation
		for _, x := range orientation.Excerpts() {
			budget -= len(x.Text)
		}
		if e["body_included"] == true || e["article"] != nil {
			continue
		}
		e["body_excerpts"] = orientation.Excerpts()
		quoted[e["ref"].(string)] = workArticleSource(p, true, e["body_excerpts"].([]notebook.TextExcerpt))
		e["reading_notice"] = "Exact passages of this revision, not a complete body. Check conditions, reasons and exceptions; read query only when a material condition remains unresolved. Excerpts do not establish support or permission."
	}
	return nil
}

func workArticleRef(w *workState, p notebook.Page) (string, error) {
	for ref, old := range w.Articles {
		if old.ID == p.ID && old.Revision == p.Revision {
			return ref, nil
		}
	}
	if len(w.Articles) >= 64 {
		return "", errors.New("this work has reached its 64 article-revision limit; prepare a new context")
	}
	ref := "knowledge" + strconv.Itoa(len(w.Articles)+1)
	w.Articles[ref] = store.KnowledgeReference{ID: p.ID, Revision: p.Revision}
	return ref, nil
}

// A receipt-bearing preparation adds knowledge without replacing its nonce or
// its earlier saves. Models and retrieval stay outside the session commit lock.
func extendWork(root, scope string, req workRequest) (any, error) {
	if req.SessionID != "" || req.TurnID != "" || req.Request != "" || len(req.Query) > requestMax || len(req.Cursor) > 16000 ||
		!notebook.ValidAssistanceText(req.Proposal, false) || !notebook.ValidAssistanceText(req.Conditions, false) ||
		req.Proposal != "" && req.Conditions != "" || (req.Query != "" || req.Cursor != "") && (req.Proposal != "" || req.Conditions != "") {
		return nil, errors.New("extend the receipt with a bounded query, proposal or current conditions; do not replace its task")
	}
	if strings.TrimSpace(req.Query+req.Proposal+req.Conditions) == "" {
		return nil, errors.New("provide search words, a proposal or current conditions")
	}
	token, err := workDecode(req.Receipt)
	if err != nil {
		return nil, err
	}
	dir, err := openWorkSessions(root)
	if err != nil {
		return nil, err
	}
	defer dir.Close()
	// loadWorkState derives the actual filename and checks the active turn.
	w, name, err := loadWorkState(dir, scope, token)
	if err != nil {
		return nil, err
	}
	unlock, err := workLock(dir, name)
	if err != nil {
		return nil, err
	}
	defer unlock()
	w, _, err = loadWorkState(dir, scope, token)
	if err != nil {
		return nil, err
	}
	if w.Articles == nil {
		return nil, errors.New("the prepared article references are missing; prepare again")
	}
	s, err := readPlanningSession(dir, name)
	if err != nil {
		return nil, err
	}
	task, taskContext, err := preparedWorkTask(s, w, workRequest{Request: w.Task, Query: w.Query})
	if err != nil {
		return nil, err
	}
	book := notebook.Book{Root: root, Scope: scope}
	query := req.Query
	if query == "" {
		query = w.Query
	}
	entries := []any{}
	quoted := map[string]string{}
	read := []store.KnowledgeReference{}
	reply := map[string]any{"reasoning": currentReasoningView(book, w.Reasoning), "receipt": req.Receipt, "knowledge": entries, "how_to_record": workHowToRecord, "task": task, "task_context": taskContext, "agent_supplied_task": w.Standalone}
	add := func(p notebook.Page) error {
		p, e := currentWorkPremise(book, p)
		if e != nil {
			return e
		}
		ref, e := workArticleRef(w, p)
		if e != nil {
			return e
		}
		quoted[ref] = planningArticleSource(p)
		read = append(read, store.KnowledgeReference{ID: p.ID, Revision: p.Revision})
		entry := map[string]any{"ref": ref, "id": p.ID, "revision": p.Revision, "article": p, "recheck": p.Recheck, "orientation": notebook.Orient(p, task+" "+query, 2400), "body_included": true, "query": map[string]any{"operation": "query", "query": "lesson:" + p.ID + "@" + p.Revision}}
		setWorkLearning(book, entry, p, req.Query)
		entries = append(entries, entry)
		return nil
	}
	if req.Proposal != "" || req.Conditions != "" {
		op := "consult"
		if req.Conditions != "" {
			op = "reuse"
		}
		view, e := consultExperience(book, notebookRequest{Operation: op, Request: task, Proposal: req.Proposal, Conditions: req.Conditions})
		if e != nil {
			return nil, e
		}
		view.Collective = nil
		reply["experience"] = view
		// Reuse the exact payload supplied by consult, including omitted-body
		// state; do not add a second copy of each passage to the response.
		for i, c := range view.Checks {
			p := notebook.Page{ID: c.Page.ID, Revision: c.Page.Revision}
			ref, e := workArticleRef(w, p)
			if e != nil {
				return nil, e
			}
			page := notebook.Page{Title: c.Page.Title, Lesson: c.Page.Lesson, AppliesTo: c.Page.AppliesTo, Rationale: c.Page.Rationale, Body: c.Page.Body}
			for _, id := range c.Page.Sources {
				page.Sources = append(page.Sources, notebook.Source{ID: id})
			}
			quoted[ref] = workArticleSource(page, c.BodyOmitted, c.BodyExcerpts)
			if !c.BodyOmitted {
				read = append(read, store.KnowledgeReference{ID: p.ID, Revision: p.Revision})
			}

			learningPath := ""
			if c.Learning != nil && !c.Learning.Omitted && c.Learning.Learning != nil {
				learningPath = "experience.checks[" + strconv.Itoa(i) + "].learning_context.learning"
			}
			entries = append(entries, map[string]any{"ref": ref, "id": p.ID, "revision": p.Revision, "experience_check": i, "recheck": c.Page.Recheck, "learning_evidence": learningPath, "collective_excluded": c.ModelExcluded, "body_included": !c.BodyOmitted, "query": map[string]any{"operation": "query", "query": "lesson:" + p.ID + "@" + p.Revision}})
		}
	} else {
		view, e := book.QueryPage(query, req.Cursor)
		if e != nil {
			return nil, e
		}
		for _, p := range view.Lessons {
			if e = add(p); e != nil {
				return nil, e
			}
		}
		context, e := preparedKnowledgeContext(view, query)
		if e != nil {
			return nil, e
		}
		reply["knowledge_context"] = context
		next := map[string]any{"operation": "prepare", "work": map[string]any{"receipt": req.Receipt, "query": query, "cursor": view.NextCursor}}
		reply["knowledge_query"] = next
	}
	collective, e := preparedCollective(book, w, nil, entries)
	if e != nil {
		return nil, e
	}
	reply["collective"] = collective
	reply["knowledge"] = entries
	if workContextSize(reply) > workSourceBudget*3 {
		return nil, errors.New("the added knowledge exceeds the response budget; use a narrower query or the individual wiki operations")
	}
	if err = saveWorkContext(dir, name, scope, token.Turn, w, false, func(s *hookSession) error {
		return retainWorkKnowledge(s, w, quoted, read)
	}); err != nil {
		return nil, err
	}
	return reply, nil
}

// Only material actually supplied becomes quoteable. Full reads and exact
// excerpts use the same registration path without promoting unseen bodies.
func workArticleSource(page notebook.Page, omitted bool, excerpts []notebook.TextExcerpt) string {
	ids := []string{}
	for _, source := range page.Sources {
		ids = append(ids, source.ID)
	}
	text := fmt.Sprintf("Wiki evidence (hypothesis, not agreement)\ntitle: %s\nsummary: %s\napplies when: %s\nrationale: %s\nsources: %s", page.Title, page.Lesson, page.AppliesTo, page.Rationale, strings.Join(ids, ", "))
	if !omitted {
		return text + "\n\n" + page.Body
	}
	for _, excerpt := range excerpts {
		text += fmt.Sprintf("\n\nExact excerpt [%d:%d]:\n%s", excerpt.Start, excerpt.End, excerpt.Text)
	}
	return text
}

func retainWorkKnowledge(s *hookSession, w *workState, quoted map[string]string, read []store.KnowledgeReference) error {
	if s.Planning.Sources == nil {
		s.Planning.Sources = map[string]string{}
	}
	k := s.Planning.Knowledge
	if k == nil {
		k = &store.KnowledgeContext{Query: w.Query}
		s.Planning.Knowledge = k
	}
	for ref, text := range quoted {
		p := w.Articles[ref]
		key := w.Keys[ref]
		if key == "" {
			key = "article:" + p.ID + ":" + p.Revision
			w.Keys[ref] = key
		}
		previous := s.Planning.Sources[key]
		if previous == "" {
			s.Planning.Sources[key] = text
		} else if !strings.Contains(previous, text) {
			s.Planning.Sources[key] = previous + "\n\nAdditional returned evidence:\n" + text
		}
		if !slices.Contains(k.Available, p) {
			k.Available = append(k.Available, p)
		}
		if slices.Contains(read, p) && !slices.Contains(k.Read, p) {
			k.Read = append(k.Read, p)
		}
	}
	// This is the cumulative context supplied through the receipt, not a
	// replacement search or a claim that omitted bodies have been read.
	k.At, k.ContextBytes = time.Now().UTC(), 0
	for key, text := range s.Planning.Sources {
		if strings.HasPrefix(key, "lesson:") || strings.HasPrefix(key, "article:") {
			k.ContextBytes += len(text)
		}
	}
	return nil
}
