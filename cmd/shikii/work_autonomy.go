package main

import (
	"encoding/json"
	"errors"
	"os"
	"slices"
	"strings"

	"github.com/hayaenfj/shikii/internal/notebook"
)

type workProcedureProgress struct {
	CloseReport bool                      `json:"close_report,omitempty"`
	Notice      string                    `json:"notice,omitempty"`
	Mixed       bool                      `json:"mixed,omitempty"`
	Advertise   bool                      `json:"-"`
	Trace       notebook.ProcedureTrace   `json:"trace"`
	View        *notebook.ProcedureStatus `json:"view,omitempty"`
	Pending     *notebook.ProcedureSample `json:"pending,omitempty"`
	Indexed     string                    `json:"indexed,omitempty"`
}

func prepareProcedureAdvice(book notebook.Book, w *workState, req workRequest, entries []any, a *workLoopAdvice, context ...string) {
	if w.Reasoning == nil {
		return
	}
	r := w.Reasoning
	if len(r.PendingProcedures) > 0 {
		return
	}
	if r.Procedure == nil {
		articles := readingArticles(entries)
		if len(articles) == 0 || len(articles) > 8 || strings.HasPrefix(req.Query, "lesson:") || sourceIDText(req.Query) || req.Proposal != "" || req.Conditions != "" || req.Cursor != "" {
			return
		}
		task, stage := w.Task, ""
		if r.Reading != nil {
			stage = r.Reading.Stage
		}
		if len(context) > 0 {
			task = context[0]
		}
		if len(context) > 1 {
			stage = context[1]
		}
		basis := notebook.ProcedureBasis{Topic: readingTopic(w.Query), Stage: stage, Articles: articles}
		work, taskID := workHash([]string{r.SessionID, r.TurnID})[:16], workHash(task)[:16]
		config, view, e := book.ChooseProcedure(basis, work, taskID)
		if e != nil {
			view = &notebook.ProcedureStatus{Status: "unavailable", Policy: config.ID}
		}
		baseline := notebook.DefaultProcedure()
		if view != nil && view.Baseline.ID != "" {
			baseline = view.Baseline
		}
		r.Procedure = &workProcedureProgress{Trace: notebook.ProcedureTrace{Baseline: baseline, Basis: basis, Work: work, Case: taskID, Config: config, Status: "baseline", Safe: true}, View: view}
		if view != nil {
			r.Procedure.Trace.Status = view.Status
		}
	}
	p := r.Procedure
	// Recovery within the pinned basis is available, but does not contribute
	// stalls or display savings to a broad-search procedure comparison.
	explicit := strings.HasPrefix(req.Query, "lesson:") || sourceIDText(req.Query)
	if explicit {
		for _, article := range readingArticles(entries) {
			if !slices.Contains(p.Trace.Basis.Articles, article) {
				p.Mixed = true
			}
		}
	}
	if req.Proposal != "" || req.Conditions != "" || !explicit && (readingTopic(req.Query) != p.Trace.Basis.Topic || workHash(readingArticles(entries)) != workHash(p.Trace.Basis.Articles)) {
		p.Mixed = true
	}
	if len(context) > 0 && workHash(context[0])[:16] != p.Trace.Case || len(context) > 1 && context[1] != p.Trace.Basis.Stage {
		p.Mixed = true
	}
	p.Advertise = false
	if p.Mixed || explicit || req.Cursor != "" {
		return
	}
	base := *a
	applyProcedureAdvice(&base, p.Trace.Baseline)
	applyProcedureAdvice(a, p.Trace.Config)
	p.Advertise = false
	if p.View != nil && a.Action != "continue" {
		notice := p.View.Status + ":" + p.View.Policy
		if notice != p.Notice {
			p.Notice = notice
			p.Advertise = true
		}
	}
	p.Trace.Stalls = max(p.Trace.Stalls, a.SameResultReads)
	if len(a.RepeatedTrials) > 0 {
		p.Trace.Stalls = max(p.Trace.Stalls, 2)
	}
	// Shadow cost includes the policy status envelope. It is a paired display
	// measurement, not a claim about LLM tokens, task time or semantic quality.
	if base.Action != "continue" || base.SameResultReads != 0 {
		p.Trace.BaselineBytes += workContextSize(base)
	}
	if a.Action != "continue" || a.SameResultReads != 0 {
		p.Trace.ActualBytes += workContextSize(a)
	}
	if p.View != nil {
		p.Trace.ActualBytes += workContextSize(procedureAttention(w))
	}
	if p.Trace.BaselineBytes > 16<<20 || p.Trace.ActualBytes > 16<<20 {
		p.Trace.Safe = false
		p.Trace.BaselineBytes = min(p.Trace.BaselineBytes, 16<<20)
		p.Trace.ActualBytes = min(p.Trace.ActualBytes, 16<<20)
	}
}

func applyProcedureAdvice(a *workLoopAdvice, c notebook.ProcedureConfig) {
	if c.Compact == 0 || a.Action == "continue" && a.SameResultReads < c.RepeatAfter && len(a.RepeatedTrials) == 0 {
		return
	}
	if a.SameResultReads >= c.RepeatAfter || len(a.RepeatedTrials) > 0 {
		a.Action = "change_approach"
	}
	if a.Action == "change_approach" {
		a.Next = "Change method; keep checks and exact source recovery."
	} else {
		a.Next = "Reuse refs; preserve checks and source recovery."
	}
	if c.Compact == 2 {
		if a.Action == "change_approach" {
			a.Next = "Change method; keep checks and recovery."
		} else {
			a.Next = "Reuse refs; keep checks and recovery."
		}
	}
}

func procedureAttention(w *workState) map[string]any {
	if w.Reasoning != nil && w.Reasoning.Procedure != nil {
		if v := w.Reasoning.Procedure.View; v != nil && w.Reasoning.Procedure.Advertise {
			return map[string]any{"status": v.Status, "policy": v.Policy}
		}
	}
	return nil
}

func recordProcedureFeedback(book notebook.Book, w *workState, c *workCompletion, additional workQualityEvidence, closeReport bool, save func() error, commit workCommitter) (*notebook.ProcedureStatus, error) {
	if w.Reasoning == nil {
		return nil, nil
	}
	if w.Reasoning.Procedure == nil || w.Result == nil || !w.Result.Ready {
		return nil, recoverProcedureFeedback(book, w, save, commit)
	}
	p := w.Reasoning.Procedure
	if p.Pending == nil && p.Indexed == w.Result.Source {
		return p.View, recoverProcedureFeedback(book, w, save, commit)
	}
	if p.View != nil && p.View.Status == "limited" {
		return p.View, recoverProcedureFeedback(book, w, save, commit)
	}
	if p.Trace.Stalls == 0 && p.Trace.Config.ID == notebook.DefaultProcedure().ID && p.View == nil && !p.Mixed {
		if e := recoverProcedureFeedback(book, w, save, commit); e != nil {
			return nil, e
		}
		if p.Pending == nil {
			p.Indexed = w.Result.Source
			return nil, save()
		}
		return p.View, nil
	}
	if p.Pending == nil || p.Pending.Snapshot.ID != w.Result.Source {
		snapshot, e := book.OutcomeSources([]string{w.Result.Source})
		if e != nil {
			return nil, e
		}
		// A ref-only retry derives quality from its saved report, never from the
		// retry body. Unknown quality still retains origin evidence identities.
		material, e := book.ReadSources([]string{w.Result.Source})
		if e != nil {
			return nil, e
		}
		var original workCompletion
		if e = json.Unmarshal([]byte(material["observations"].([]notebook.Observation)[0].Content), &original); e != nil {
			return nil, e
		}
		ids := []string{}
		for _, ref := range w.Result.Evidence {
			if !strings.HasPrefix(ref, "source:") {
				continue
			}
			ids = append(ids, strings.TrimPrefix(ref, "source:"))
		}
		evidence := []notebook.Source{}
		if len(ids) > 0 {
			evidence, e = book.OutcomeSources(ids)
			if e != nil {
				return nil, e
			}
		}
		trace := p.Trace
		trace.Mixed = p.Mixed

		if p.Pending != nil && p.Indexed != p.Pending.Snapshot.ID {
			if len(w.Reasoning.PendingProcedures) >= 8 {
				return nil, errors.New("recover pending procedure feedback before adding another report")
			}
			prior := *p
			prior.CloseReport = false
			w.Reasoning.PendingProcedures = append(w.Reasoning.PendingProcedures, &prior)
		}

		evidence, e = mergeWorkQualityEvidence(evidence, additional, original.Sufficient)
		if e != nil {
			excludeWorkQuality(w, p, w.Result.Source, original.Sufficient)
			return p.View, save()
		}
		p.Pending = &notebook.ProcedureSample{Trace: trace, Sufficient: original.Sufficient, Evidence: evidence, Snapshot: snapshot[0]}
		p.CloseReport = closeReport
		if e = save(); e != nil {
			return nil, e
		}
	}
	e := recoverProcedureFeedback(book, w, save, commit)
	view := p.View
	if e == nil && view != nil && view.Source != "" {
		w.Sources["procedure"] = view.Source
		e = save()
	}
	return view, e
}

func resumeProcedureFeedback(book notebook.Book, p *workProcedureProgress, save func() error, commit workCommitter) (*notebook.ProcedureStatus, error) {
	if p.Pending == nil || p.Indexed == p.Pending.Snapshot.ID {
		return p.View, nil
	}
	var e error
	if err := commit(func() error { p.View, e = book.RecordProcedure(*p.Pending); return e }); err != nil {
		return p.View, err
	}
	if e = workAfterWrite("procedure"); e != nil {
		return p.View, e
	}
	p.Indexed = p.Pending.Snapshot.ID
	return p.View, save()
}

func recoverProcedureFeedback(book notebook.Book, w *workState, save func() error, commit workCommitter) error {
	if w.Reasoning == nil {
		return nil
	}
	pending := false
	for _, p := range append(w.Reasoning.PendingProcedures, w.Reasoning.Procedure) {
		if p != nil {
			pending = pending || p.Pending != nil && p.Indexed != p.Pending.Snapshot.ID
			if _, e := resumeProcedureFeedback(book, p, save, commit); e != nil {
				return e
			}
			if p.View != nil && p.View.Ineligible && p.Pending != nil && p.Pending.Sufficient != nil && w.Result != nil && w.Result.Source == p.Pending.Snapshot.ID && !w.Result.Unresolved {
				w.Result.Unresolved = true
				if e := save(); e != nil {
					return e
				}
			}
		}
	}
	if !pending {
		return nil
	}
	kept := []*workProcedureProgress{}
	for _, p := range w.Reasoning.PendingProcedures {
		if p.Pending != nil && p.Indexed != p.Pending.Snapshot.ID {
			kept = append(kept, p)
		}
	}
	w.Reasoning.PendingProcedures = kept
	if len(kept) > 8 {
		return errors.New("recover pending procedure feedback before starting more tasks")
	}
	return save()
}

func pendingProcedureCount(r *reasoningProgress) int {
	if r == nil {
		return 0
	}
	n := 0
	for _, p := range append(r.PendingProcedures, r.Procedure) {
		if p != nil && p.Pending != nil && p.Indexed != p.Pending.Snapshot.ID {
			n++
		}
	}
	return n
}
func pendingWorkProcedure(w *workState) bool { return pendingProcedureCount(w.Reasoning) > 0 }

func recoverPreparedProcedure(root, scope string, dir *os.Root, name string, w *workState, token workToken, reply map[string]any) {
	save := func() error { return saveWorkState(dir, name, scope, token.Turn, w, false) }
	commit := workCommitter(func(action func() error) error { return commitWorkTurn(dir, name, scope, token, action) })
	book := notebook.Book{Root: root, Scope: scope}
	e := recoverProcedureFeedback(book, w, save, commit)
	if e == nil && w.Reasoning != nil && w.Reasoning.Procedure != nil {
		p := w.Reasoning.Procedure
		if p.CloseReport && p.Pending != nil && p.Indexed == p.Pending.Snapshot.ID && w.Result != nil && w.Result.Source == p.Indexed && workResultReviewCurrent(w) && !w.Reasoning.WikiPending && !reasoningPublicationsPending(w) && len(w.Reasoning.PendingCaptures) == 0 && trialPendingCount(w.Reasoning) == 0 && pendingResultCount(w.Reasoning) == 0 {
			if p.View != nil && p.View.Source != "" {
				w.Sources["procedure"] = p.View.Source
			}
			resolve := func(refs []string) ([]string, error) {
				ids := []string{}
				for _, ref := range refs {
					if ref != "result" && ref != "procedure" || w.Sources[ref] == "" {
						return nil, errors.New("unknown recovered result evidence")
					}
					ids = append(ids, w.Sources[ref])
				}
				return ids, nil
			}
			_, e = finishWorkCompletion(book, w, resolve, save, commit)
			if e == nil {
				p.CloseReport = false
				e = save()
			}
			if e == nil {
				if _, included := reply["reasoning"]; included {
					reply["reasoning"] = currentReasoningView(book, w.Reasoning)
				}
				reply["procedure_recovery"] = map[string]any{"status": "saved", "result_source": w.Result.Source}
			}
		}
	}
	if e != nil {
		message := e.Error()
		for workContextSize(message) > 240 {
			message = cut(message, len(message)/2)
		}
		reply["procedure_recovery"] = map[string]any{"status": "pending", "next_action": "The original report is saved. A later prepare retries procedure indexing automatically.", "error": message}
	}
}
