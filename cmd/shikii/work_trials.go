package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/hayaenfj/shikii/internal/notebook"
	"github.com/hayaenfj/shikii/internal/store"
)

// Trials are evidence-linked agent reports. They do not execute methods or
// certify that a reported observation, assessment or measurement is true.
type workTrialPlan struct {
	Case       string   `json:"case"`
	Question   string   `json:"question"`
	Expected   string   `json:"expected"`
	Method     string   `json:"method"`
	Conditions string   `json:"conditions"`
	Decision   string   `json:"decision"`
	Effort     string   `json:"effort"`
	Evidence   []string `json:"evidence"`
}

type workTrialResult struct {
	Assessment   string             `json:"assessment"`
	Observed     string             `json:"observed"`
	Evidence     []string           `json:"evidence"`
	Measurements map[string]float64 `json:"measurements,omitempty"`
}

type workTrialRequest struct {
	Ref    string           `json:"ref,omitempty"`
	Plan   *workTrialPlan   `json:"plan,omitempty"`
	Result *workTrialResult `json:"result,omitempty"`
}

type workTrialState struct {
	Ref          string           `json:"ref"`
	Plan         workTrialPlan    `json:"plan"`
	Source       string           `json:"source,omitempty"`
	Result       *workTrialResult `json:"result,omitempty"`
	ResultSource string           `json:"result_source,omitempty"`
	Reviewed     bool             `json:"reviewed,omitempty"`
}

type workTrialChoice struct {
	Ref      string `json:"ref"`
	Question string `json:"question"`
	Method   string `json:"method"`
	Decision string `json:"decision"`
	Reason   string `json:"reason"`
}

type workTrialReport struct {
	Type       string           `json:"type"`
	Store      string           `json:"store"`
	Session    string           `json:"session"`
	Turn       string           `json:"turn"`
	Ref        string           `json:"ref"`
	Plan       workTrialPlan    `json:"plan"`
	PlanSource string           `json:"plan_source,omitempty"`
	Result     *workTrialResult `json:"result,omitempty"`
}

func trialIdentity(book notebook.Book, r *reasoningProgress, plan workTrialPlan) string {
	return "trial:" + workHash([]any{store.StoreName(book.Root, book.Scope), r.SessionID, r.TurnID, plan})
}

func validateTrialPlan(p workTrialPlan) error {
	for _, text := range []string{p.Case, p.Question, p.Expected, p.Method, p.Conditions, p.Decision} {
		if !assignmentText(text, 1200, true) {
			return errors.New("trial case, question, expected, method, conditions and decision need 1-1200 UTF-8 bytes")
		}
	}
	if !slices.Contains([]string{"small", "medium", "large"}, p.Effort) || len(p.Evidence) < 1 || len(p.Evidence) > 4 {
		return errors.New("trial needs small/medium/large effort and one to four evidence references")
	}
	return nil
}

var trialMetricName = regexp.MustCompile(`^[a-z][a-z0-9_]{0,47}$`)

func validateTrialResult(r workTrialResult) error {
	if !slices.Contains([]string{"supports", "challenges", "inconclusive"}, r.Assessment) || !assignmentText(r.Observed, 1200, true) || len(r.Evidence) < 1 || len(r.Evidence) > 4 || len(r.Measurements) > 4 {
		return errors.New("trial result needs supports/challenges/inconclusive, observed text, one to four sources and at most four measurements")
	}
	for name, value := range r.Measurements {
		if !trialMetricName.MatchString(name) || math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || value > 1e15 {
			return errors.New("trial measurements need bounded nonnegative values and short metric names including their units")
		}
	}
	return nil
}

func bindWorkEvidence(book notebook.Book, w *workState, refs []string, resolve func([]string) ([]string, error)) ([]string, error) {
	bound := []string{}
	for _, ref := range refs {
		if !assignmentText(ref, 200, true) {
			return nil, errors.New("evidence references must fit 200 bytes")
		}
		if article, ok := w.Articles[ref]; ok {
			live, err := book.LiveWhere(func(p notebook.Page) bool { return p.ID == article.ID })
			if err != nil {
				return nil, err
			}
			if len(live) != 1 || live[0].Revision != article.Revision {
				return nil, errors.New("reasoning needs the same live article revision; read it again")
			}
			bound = append(bound, "article:"+article.ID+"@"+article.Revision)
			continue
		}
		if w.Reasoning != nil {
			if comparison := w.Reasoning.Comparisons[ref]; comparison != nil {
				if comparison.Source == "" || !prepareWorkSource(book, comparison.Source).Complete {
					return nil, errors.New("the comparison source is not confirmed; retry its reference")
				}
				bound = append(bound, "source:"+comparison.Source)
				continue
			}
			if trial := w.Reasoning.Trials[ref]; trial != nil {
				id := trial.ResultSource
				if id == "" {
					id = trial.Source
				}
				if id == "" || !prepareWorkSource(book, id).Complete {
					return nil, errors.New("the trial source is not confirmed; retry the same trial reference")
				}
				bound = append(bound, "source:"+id)
				continue
			}
		}
		if ref != "current" && w.Sources[ref] == "" && !strings.HasPrefix(ref, "new") {
			return nil, fmt.Errorf("unknown reasoning evidence %q; use current, a prepared article, trial or captured source: evidence takes references, not text (what you checked goes in checked or reason; to cite it, add it to observations and name it new1)", ref)
		}
		ids, err := resolve([]string{ref})
		if err != nil {
			return nil, err
		}
		for _, id := range ids {
			if !prepareWorkSource(book, id).Complete {
				return nil, errors.New("reasoning evidence is unavailable or incomplete")
			}
			bound = append(bound, "source:"+id)
		}
	}
	slices.Sort(bound)
	return slices.Compact(bound), nil
}

func trialBasisCurrent(book notebook.Book, evidence []string) bool {
	for _, ref := range evidence {
		if strings.HasPrefix(ref, "article:") {
			id, revision, ok := strings.Cut(strings.TrimPrefix(ref, "article:"), "@")
			if !ok {
				return false
			}
			live, err := book.LiveWhere(func(p notebook.Page) bool { return p.ID == id })
			if err != nil || len(live) != 1 || live[0].Revision != revision {
				return false
			}
		} else if !completeWorkSourceReference(book, ref) {
			return false
		}
	}
	return len(evidence) > 0
}

func recordWorkTrial(book notebook.Book, w *workState, request workTrialRequest, resolve func([]string) ([]string, error), save func() error, commit workCommitter) (workItem, error) {
	item := workItem{Ref: request.Ref, Kind: "trial"}
	r := w.Reasoning
	if r == nil || r.Start == nil {
		return item, errors.New("save reasoning start before planning or observing a trial")
	}
	if !assignmentText(request.Ref, 100, false) || request.Plan != nil && request.Result != nil {
		return item, errors.New("trial takes a plan or a result, not both")
	}
	if r.Trials == nil {
		r.Trials = map[string]*workTrialState{}
	}
	var state *workTrialState
	if request.Plan != nil {
		if err := validateTrialPlan(*request.Plan); err != nil {
			return item, err
		}
		bound := *request.Plan
		var err error
		bound.Evidence, err = bindWorkEvidence(book, w, bound.Evidence, resolve)
		if err != nil {
			return item, err
		}
		ref := trialIdentity(book, r, bound)
		if request.Ref != "" && request.Ref != ref {
			return item, errors.New("a trial plan cannot replace another trial reference")
		}
		state = r.Trials[ref]
		if state == nil {
			if len(r.Trials) >= 8 {
				return item, errors.New("a turn accepts at most eight trials")
			}
			if err := workSavedCapacity(w, "trial:"+ref); err != nil {
				return item, err
			}
			w.Expected["trial:"+ref] = store.KnowledgeReference{}
			state = &workTrialState{Ref: ref, Plan: bound}
			r.Trials[ref] = state
			// Preserve the original intent before its source can be published.
			if err := save(); err != nil {
				return item, err
			}
		}
	} else {
		state = r.Trials[request.Ref]
		if state == nil {
			return item, errors.New("trial result or recovery needs a trial from this turn")
		}
	}
	item.Ref = state.Ref
	if state.Source == "" {
		key := "trial:" + state.Ref
		if err := workSavedCapacity(w, key); err != nil {
			return item, err
		}
		w.Expected[key] = store.KnowledgeReference{}
		report := workTrialReport{Type: "trial-plan/v1", Store: store.StoreName(book.Root, book.Scope), Session: r.SessionID, Turn: r.TurnID, Ref: state.Ref, Plan: state.Plan}
		data, _ := json.Marshal(report)
		if err := captureWorkTrialSource(book, w, workObservation{Kind: "agent", Title: "Work trial plan", Content: string(data), Reference: state.Ref}, &state.Source, key, item, save, commit); err != nil {
			return item, err
		}
	}
	if request.Result != nil {
		if err := validateTrialResult(*request.Result); err != nil {
			return item, err
		}
		bound := *request.Result
		var err error
		bound.Evidence, err = bindWorkEvidence(book, w, bound.Evidence, resolve)
		if err != nil {
			return item, err
		}
		if state.Result != nil && workHash(state.Result) != workHash(bound) {
			return item, errors.New("a trial result is immutable; plan another trial for a different observation")
		}
		if state.Result == nil {
			key := "trial_result:" + state.Ref
			if err := workSavedCapacity(w, key); err != nil {
				return item, err
			}
			w.Expected[key] = store.KnowledgeReference{}
			state.Result = &bound
			if err := save(); err != nil {
				return item, err
			}
		}
	}
	if state.Result != nil {
		item.Kind = "trial_result"
		if state.ResultSource == "" {
			key := "trial_result:" + state.Ref
			if err := workSavedCapacity(w, key); err != nil {
				return item, err
			}
			w.Expected[key] = store.KnowledgeReference{}
			report := workTrialReport{Type: "trial-result/v1", Store: store.StoreName(book.Root, book.Scope), Session: r.SessionID, Turn: r.TurnID, Ref: state.Ref, Plan: state.Plan, PlanSource: state.Source, Result: state.Result}
			data, _ := json.Marshal(report)
			if err := captureWorkTrialSource(book, w, workObservation{Kind: "agent", Title: "Work trial execution", Content: string(data), Reference: state.Ref}, &state.ResultSource, key, item, save, commit); err != nil {
				return item, err
			}
		}
		item.ID = state.ResultSource
	} else {
		item.ID = state.Source
	}
	key := item.Kind + ":" + state.Ref
	if _, ok := w.Saved[key]; !ok {
		if err := workSavedCapacity(w, key); err != nil {
			// A confirmed canonical result remains replayable without a new cache.
			return item, nil
		}
		w.Saved[key] = item
	}
	return item, save()
}

// Keep the capture memo with the canonical turn until the source binding and
// saved reply are committed together. Preparation may discard receipt caches.
func captureWorkTrialSource(book notebook.Book, w *workState, observation workObservation, binding *string, key string, item workItem, save func() error, commit workCommitter) error {
	_, captureKey, err := prepareWorkObservation(w, observation)
	if err != nil {
		return err
	}
	r := w.Reasoning
	if r.TrialCaptures == nil {
		r.TrialCaptures = map[string]workCapture{}
	}
	if memo, ok := r.TrialCaptures[captureKey]; ok {
		w.Captures[captureKey] = memo
	}
	checkpoint := func() error {
		r.TrialCaptures[captureKey] = w.Captures[captureKey]
		return save()
	}
	_, source, err := captureWorkObservation(book, w, observation, checkpoint, commit)
	if err != nil {
		return err
	}
	*binding, item.ID = source, source
	w.Saved[key] = item
	delete(w.Expected, key)
	delete(r.TrialCaptures, captureKey)
	return save()
}

func workTrialView(r *reasoningProgress) ([]*workTrialState, *workTrialChoice, []string) {
	if r == nil {
		return nil, nil, nil
	}
	states := []*workTrialState{}
	for _, trial := range r.Trials {
		states = append(states, trial)
	}
	sort.Slice(states, func(i, j int) bool {
		a, b := trialEffort(states[i].Plan.Effort), trialEffort(states[j].Plan.Effort)
		if a != b {
			return a < b
		}
		return states[i].Ref < states[j].Ref
	})
	gaps := []string{}
	for _, trial := range states {
		if trial.ResultSource != "" && trial.Result.Assessment == "challenges" && !trial.Reviewed {
			gaps = append(gaps, trial.Ref)
		}
	}
	return states, chooseTrial(r, states, nil), gaps
}

func trialEffort(s string) int { return slices.Index([]string{"small", "medium", "large"}, s) }

func chooseTrial(r *reasoningProgress, states []*workTrialState, current func(*workTrialState) bool) *workTrialChoice {
	var choice *workTrialState
	for _, trial := range states {
		if trial.Source == "" || trial.Result != nil || current != nil && !current(trial) {
			continue
		}
		if choice == nil || trial.Ref == r.SelectedTrial {
			choice = trial
		}
		if trial.Ref == r.SelectedTrial {
			break
		}
	}
	if choice == nil {
		return nil
	}
	reason := "Smallest reported effort among unresolved checks that can change the stated decision; no experiment permission or optimality is implied."
	if choice.Ref == r.SelectedTrial {
		reason = "Selected by the current agent judgment from unresolved checks."
	}
	return &workTrialChoice{Ref: choice.Ref, Question: choice.Plan.Question, Method: choice.Plan.Method, Decision: choice.Plan.Decision, Reason: reason}
}

func currentTrialChoice(book notebook.Book, r *reasoningProgress) *workTrialChoice {
	states, _, _ := workTrialView(r)
	if r == nil {
		return nil
	}
	return chooseTrial(r, states, func(t *workTrialState) bool { return trialBasisCurrent(book, t.Plan.Evidence) })
}

func trialPendingCount(r *reasoningProgress) int {
	if r == nil {
		return 0
	}
	n := 0
	for _, reading := range r.Readings {
		if reading.Source == "" || !reading.Indexed {
			n++
		}
	}
	for _, t := range r.Trials {
		if t.Source == "" {
			n++
		}
		if t.Result != nil {
			if t.ResultSource == "" || t.Result.Assessment == "challenges" && !t.Reviewed {
				n++
			}
		}
	}
	for _, c := range r.Comparisons {
		if c.Source == "" {
			n++
		}
	}
	return n
}

func checkTrialReview(r *reasoningProgress, evidence []string) error {
	for _, reading := range r.Readings {
		if reading.Source == "" || !reading.Indexed {
			return errors.New("recover the pending reading execution with its original reference before finishing")
		}
	}
	for _, t := range r.Trials {
		if t.Source == "" || t.Result != nil && t.ResultSource == "" {
			return errors.New("recover the original pending trial capture before finishing")
		}
		if t.ResultSource != "" && t.Result.Assessment == "challenges" && !t.Reviewed && !slices.Contains(evidence, "source:"+t.ResultSource) {
			return errors.New("cite each challenged trial result in reasoning update or finish before ending")
		}
	}
	for _, c := range r.Comparisons {
		if c.Source == "" {
			return errors.New("recover the original pending comparison before finishing")
		}
	}
	return nil
}

func markTrialReviews(r *reasoningProgress, evidence []string) {
	for _, t := range r.Trials {
		if t.ResultSource != "" && slices.Contains(evidence, "source:"+t.ResultSource) {
			t.Reviewed = true
		}
	}
}
