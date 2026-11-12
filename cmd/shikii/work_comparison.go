package main

import (
	"encoding/json"
	"errors"
	"regexp"
	"slices"
	"strings"

	"github.com/hayaenfj/shikii/internal/notebook"
	"github.com/hayaenfj/shikii/internal/store"
)

type workComparisonRequest struct {
	Ref    string `json:"ref,omitempty"`
	Before string `json:"before,omitempty"`
	After  string `json:"after,omitempty"`
	Reason string `json:"reason,omitempty"`
}

type workPairedMeasurement struct {
	Before float64 `json:"before"`
	After  float64 `json:"after"`
	Delta  float64 `json:"delta"`
}

type workComparisonView struct {
	Ref        string                           `json:"ref"`
	Source     string                           `json:"source,omitempty"`
	Before     string                           `json:"before"`
	After      string                           `json:"after"`
	Case       string                           `json:"case"`
	Conditions string                           `json:"conditions"`
	Method     string                           `json:"method"`
	Expected   string                           `json:"expected"`
	Reason     string                           `json:"reason"`
	Values     map[string]workPairedMeasurement `json:"values"`
	Notice     string                           `json:"notice"`
}

var trialSourceID = regexp.MustCompile(`^[0-9a-f]{16}$`)

func comparisonSource(book notebook.Book, w *workState, ref string, resolve func([]string) ([]string, error)) (string, error) {
	if t := w.Reasoning.Trials[ref]; t != nil {
		if t.ResultSource == "" {
			return "", errors.New("comparison needs saved execution results, not plans")
		}
		return t.ResultSource, nil
	}
	// Explicit source IDs allow the next work to compare a previous execution.
	// The source is still read through this project's store and checked below.
	if trialSourceID.MatchString(ref) {
		return ref, nil
	}
	ids, err := resolve([]string{ref})
	if err != nil {
		return "", err
	}
	if len(ids) != 1 {
		return "", errors.New("comparison needs exactly one source for each execution")
	}
	return ids[0], nil
}

func readWorkTrialSource(book notebook.Book, id string, source notebook.WorkSource, kind string) (workTrialReport, error) {
	var report workTrialReport
	s := prepareWorkSource(book, id)
	if !s.Complete {
		return report, errors.New("the trial source is unavailable or incomplete")
	}
	observations, _ := s.Material["observations"].([]notebook.Observation)
	if len(observations) != 1 || observations[0].ID != id || !isWorkSource(observations[0], source) {
		return report, errors.New("the source is not a saved trial report")
	}
	if err := json.Unmarshal([]byte(observations[0].Content), &report); err != nil {
		return report, err
	}
	if report.Type != kind || report.Store != store.StoreName(book.Root, book.Scope) || report.Session == "" || len(report.Turn) != 16 || observations[0].Reference != report.Ref || validateTrialPlan(report.Plan) != nil {
		return report, errors.New("the trial report does not match this project and its execution context")
	}
	owner := &reasoningProgress{SessionID: report.Session, TurnID: report.Turn}
	if report.Ref != trialIdentity(book, owner, report.Plan) {
		return report, errors.New("the trial report changed its original plan")
	}
	return report, nil
}

func historicalTrialEvidence(book notebook.Book, evidence []string) bool {
	for _, ref := range evidence {
		if strings.HasPrefix(ref, "article:") {
			id, revision, ok := strings.Cut(strings.TrimPrefix(ref, "article:"), "@")
			if !ok {
				return false
			}
			v, err := book.Query("lesson:" + id + "@" + revision)
			if err != nil || len(v.Lessons) != 1 || v.Lessons[0].Revision != revision {
				return false
			}
		} else if !completeWorkSourceReference(book, ref) {
			return false
		}
	}
	return len(evidence) > 0
}

func readWorkTrialExecution(book notebook.Book, id string) (workTrialReport, error) {
	r, err := readWorkTrialSource(book, id, notebook.WorkTrialExecution(), "trial-result/v1")
	if err != nil {
		return r, err
	}
	if r.Result == nil || validateTrialResult(*r.Result) != nil || !historicalTrialEvidence(book, r.Plan.Evidence) || !historicalTrialEvidence(book, r.Result.Evidence) {
		return r, errors.New("the execution result lost its observed evidence")
	}
	p, err := readWorkTrialSource(book, r.PlanSource, notebook.WorkTrialPlan(), "trial-plan/v1")
	if err != nil {
		return r, err
	}
	if p.Ref != r.Ref || workHash(p.Plan) != workHash(r.Plan) || p.Result != nil {
		return r, errors.New("the execution does not match its saved plan")
	}
	return r, nil
}

func recordWorkComparison(book notebook.Book, w *workState, request workComparisonRequest, resolve func([]string) ([]string, error), save func() error, commit workCommitter) (workItem, *workComparisonView, error) {
	item := workItem{Kind: "comparison", Ref: request.Ref}
	r := w.Reasoning
	if r == nil || r.Start == nil {
		return item, nil, errors.New("save reasoning start before comparing executions")
	}
	if r.Comparisons == nil {
		r.Comparisons = map[string]*workComparisonView{}
	}
	var view *workComparisonView
	if request.Ref != "" {
		if request.Before != "" || request.After != "" || request.Reason != "" {
			return item, nil, errors.New("comparison recovery takes only its reference")
		}
		view = r.Comparisons[request.Ref]
		if view == nil {
			return item, nil, errors.New("unknown comparison reference for this turn")
		}
	} else {
		if !assignmentText(request.Reason, 1200, true) || !assignmentText(request.Before, 200, true) || !assignmentText(request.After, 200, true) {
			return item, nil, errors.New("comparison needs before, after and a bounded reason")
		}
		before, err := comparisonSource(book, w, request.Before, resolve)
		if err != nil {
			return item, nil, err
		}
		after, err := comparisonSource(book, w, request.After, resolve)
		if err != nil {
			return item, nil, err
		}
		if before == after {
			return item, nil, errors.New("comparison needs two distinct executions")
		}
		a, err := readWorkTrialExecution(book, before)
		if err != nil {
			return item, nil, err
		}
		b, err := readWorkTrialExecution(book, after)
		if err != nil {
			return item, nil, err
		}
		if a.Plan.Case != b.Plan.Case || a.Plan.Conditions != b.Plan.Conditions || a.Plan.Method != b.Plan.Method || a.Plan.Expected != b.Plan.Expected || a.Plan.Question != b.Plan.Question {
			return item, nil, errors.New("comparison needs the same case, question, conditions, method and expected outcome")
		}
		if len(a.Result.Measurements) == 0 || workHash(measurementKeys(a.Result.Measurements)) != workHash(measurementKeys(b.Result.Measurements)) {
			return item, nil, errors.New("comparison needs the same nonempty measurement names and units")
		}
		ref := "comparison:" + workHash([]string{before, after, request.Reason})
		view = r.Comparisons[ref]
		if view == nil {
			if len(r.Comparisons) >= 8 {
				return item, nil, errors.New("a turn accepts at most eight comparisons")
			}
			key := "comparison:" + ref
			if err := workSavedCapacity(w, key); err != nil {
				return item, nil, err
			}
			w.Expected[key] = store.KnowledgeReference{}
			view = &workComparisonView{Ref: ref, Before: before, After: after, Case: a.Plan.Case, Conditions: a.Plan.Conditions, Method: a.Plan.Method, Expected: a.Plan.Expected, Reason: request.Reason, Values: map[string]workPairedMeasurement{}, Notice: "Agent-reported paired values, not proof of improvement, causality, success or a human truth. Check execution conditions, units and order effects."}
			for name, v := range a.Result.Measurements {
				view.Values[name] = workPairedMeasurement{Before: v, After: b.Result.Measurements[name], Delta: b.Result.Measurements[name] - v}
			}
			r.Comparisons[ref] = view
			if err := save(); err != nil {
				return item, view, err
			}
		}
	}
	item.Ref = view.Ref
	if view.Source == "" {
		key := "comparison:" + view.Ref
		if err := workSavedCapacity(w, key); err != nil {
			return item, view, err
		}
		w.Expected[key] = store.KnowledgeReference{}
		data, _ := json.Marshal(view)
		if err := captureWorkTrialSource(book, w, workArchive(notebook.WorkTrialComparison(), string(data), view.Ref), &view.Source, key, item, save, commit); err != nil {
			return item, view, err
		}
	}
	item.ID = view.Source
	key := "comparison:" + view.Ref
	if _, ok := w.Saved[key]; !ok {
		if err := workSavedCapacity(w, key); err != nil {
			return item, view, nil
		}
		w.Saved[key] = item
	}
	return item, view, save()
}

func measurementKeys(values map[string]float64) []string {
	keys := []string{}
	for key := range values {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}
