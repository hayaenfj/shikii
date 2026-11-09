package main

import (
	"errors"
	"sort"

	"github.com/hayaenfj/shikii/internal/notebook"
)

func prepareAssignmentView(root, scope, receipt string, v map[string]any, records []assignmentRecord, r assignmentRecord, a assignmentContext, req workRequest, size func(any) int) (assignmentBinding, map[string]any, error) {
	b := notebook.Book{Root: root, Scope: scope}
	binding := assignmentBinding{Knowledge: map[string]notebook.CollectiveBaseline{}}
	exposures, err := assignmentSupplied(root, scope, receipt)
	if err != nil {
		return assignmentBinding{}, nil, err
	}
	for ref, exposure := range exposures {
		binding.Knowledge[ref] = exposure
	}
	selected := []notebook.ArticleReference{}
	for _, raw := range v["knowledge"].([]any) {
		p := raw.(map[string]any)
		ref := notebook.ArticleReference{ID: p["id"].(string), Revision: p["revision"].(string)}
		base, ok := exposures[p["ref"].(string)]
		if !ok || base.ArticleReference != ref {
			return assignmentBinding{}, nil, errors.New("the supplied assignment knowledge is incomplete")
		}
		binding.Knowledge[p["ref"].(string)] = base
		selected = append(selected, ref)
	}
	knownByID := map[string]notebook.CollectiveBaseline{}
	coverage := []any{}
	questions := []any{}
	reviews := []any{}
	omitted := 0
	for i := len(records) - 1; i >= 0; i-- {
		prior := records[i]
		if prior.Context.ID == a.ID || !assignmentRelated(a, prior.Context) {
			continue
		}
		if prior.Context.Group == a.Group {
			for id, ref := range prior.Used {
				if _, seen := knownByID[id]; !seen {
					knownByID[id] = ref
				}
			}
		}
		if len(coverage) >= 8 {
			omitted++
			continue
		}
		state, last := assignmentState(prior)
		codeState, commit := prior.Context.CodeState, prior.Context.Commit
		item := map[string]any{"id": prior.Context.ID, "group": prior.Context.Group, "role": prior.Context.Role, "stage": prior.Context.Stage, "question": prior.Context.Question, "conditions": prior.Context.Conditions, "state": state, "commit": commit, "code_state": codeState, "reference": "assignment:" + prior.Context.ID}
		if last != nil && (a.EvidenceScope == "" || prior.Context.Shareable || a.EvidenceScope == prior.Context.EvidenceScope) {
			item["summary"] = assignmentClip(last.Event.Summary, 1200)
			item["source"] = last.Source
			if last.Event.Commit != "" {
				commit = last.Event.Commit
			}
			if last.Event.CodeState != "" {
				codeState = last.Event.CodeState
			}
			if state == "integrated" || state == "reverted" {
				codeState = state
			}
			item["commit"], item["code_state"] = commit, codeState
			for _, q := range last.Event.Unknowns {
				if len(questions) < 8 {
					questions = append(questions, map[string]any{"question": q, "method": last.Event.Method, "contradictions": last.Event.Contradictions, "conditions": prior.Context.Conditions, "source": last.Source, "reference": item["reference"]})
				}
			}
			if state == "failed" || len(last.Event.Contradictions) > 0 {
				if len(reviews) < 4 {
					reviews = append(reviews, map[string]any{"assignment": prior.Context.ID, "source": last.Source, "summary": assignmentClip(last.Event.Summary, 1200), "conditions": prior.Context.Conditions, "contradictions": last.Event.Contradictions, "method": last.Event.Method, "action": "Compare this observed failure with the relevant howto; save a source-backed correction through ordinary notes, never an automatic success label."})
				}
			}
		}
		item["applicability"] = "compare_conditions"
		if state == "reverted" || codeState == "provisional" && commit != a.Commit || prior.Context.Conditions != a.Conditions {
			item["applicability"] = "recheck"
		}
		coverage = append(coverage, item)
	}
	for id, ref := range r.Used {
		knownByID[id] = ref
	}
	known := []notebook.CollectiveBaseline{}
	for _, ref := range knownByID {
		known = append(known, ref)
	}
	sort.Slice(known, func(i, j int) bool { return known[i].ID < known[j].ID })
	if len(known) > 64 {
		known = known[:64]
		omitted++
	}
	changes, err := b.Collective(known, selected)
	if err != nil {
		return assignmentBinding{}, nil, err
	}
	team := map[string]any{"assignment": a, "changes": changes, "coverage": coverage, "questions": questions, "procedure_reviews": reviews, "omitted": omitted, "notice": assignmentNotice}
	for workContextSize(team) > 24<<10 {
		switch {
		case reduceAssignmentPeers(team, &reviews, &questions, &coverage):
		default:
			return assignmentBinding{}, nil, errors.New("assignment context exceeds 24 KiB; narrow the context")
		}
		omitted++
		team["omitted"] = omitted
	}
	v["teamwork"] = team
	budget, err := workOutputBudget(req.MaxBytes)
	if err != nil {
		return assignmentBinding{}, nil, err
	}
	for size(v)+1 > budget {
		switch {
		case reduceAssignmentPeers(team, &reviews, &questions, &coverage):
		case team["changes"] != nil:
			delete(team, "changes")
		default:
			// Empty optional fields also occupy bytes in a small response.
			delete(team, "coverage")
			delete(team, "questions")
			delete(team, "procedure_reviews")
			if size(v)+1 > budget {
				return assignmentBinding{}, nil, errors.New("assignment identity exceeds max_bytes; prepare with a larger response budget")
			}
		}
		omitted++
		team["omitted"] = omitted
	}
	return binding, team, nil
}
