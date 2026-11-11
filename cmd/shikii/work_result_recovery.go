package main

import (
	"encoding/json"
	"errors"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/hayaenfj/shikii/internal/notebook"
)

// Pending originals belong to their first turn. A later receipt may repair only
// the missing evidence, without replacing the narrative or finishing a new task.
type workResultRecovery struct {
	Result      *workCompletionRef     `json:"result"`
	Turn        string                 `json:"turn"`
	Procedure   *workProcedureProgress `json:"procedure,omitempty"`
	Evidence    []string               `json:"evidence,omitempty"`
	Source      string                 `json:"source,omitempty"`
	Queued      bool                   `json:"queued,omitempty"`
	CompletedAt time.Time              `json:"completed_at,omitempty"`
	Complete    bool                   `json:"complete,omitempty"`
}

func repairWorkResult(book notebook.Book, w *workState, c workCompletion, additional workQualityEvidence, resolve func([]string) ([]string, error), save func() error, commit workCommitter) (workItem, error) {
	item := workItem{Kind: "result-recovery", Ref: c.Ref}
	pending := w.Reasoning.PendingResults[c.Ref]
	if pending == nil && w.Result != nil && w.Result.Source == c.Ref && w.Result.Ready {
		if !workResultReviewCurrent(w) {
			return item, errors.New("work changed after the saved finish; record the changed result")
		}
		material, e := book.ReadSources([]string{c.Ref})
		if e != nil {
			return item, e
		}
		originals := material["observations"].([]notebook.Observation)
		var original workCompletion
		if len(originals) != 1 || originals[0].Kind != "agent" || originals[0].Title != "Work result and remaining conditions" || json.Unmarshal([]byte(originals[0].Content), &original) != nil || workHash(original) != w.Result.Signature {
			return item, errors.New("the original saved result changed or is unavailable")
		}
		if len(w.Result.Evidence) > 0 && !trialBasisCurrent(book, w.Result.Evidence) {
			return item, errors.New("the saved result evidence changed; record its corrected result")
		}
		if len(c.Evidence) > 0 {
			if e := selfEvidence(w, c.Ref, c.Evidence); e != nil {
				return item, e
			}
			bound, e := bindWorkEvidence(book, w, c.Evidence, resolve)
			if e != nil {
				return item, e
			}
			if !slices.Equal(bound, w.Result.Evidence) {
				return item, errors.New("changed evidence needs a new result snapshot")
			}
		}
		return workItem{Kind: "result", Ref: "result", ID: c.Ref}, nil
	}
	if pending == nil || pending.Result == nil || pending.Result.Source != c.Ref {
		return item, errors.New("unknown pending result reference")
	}
	evidence := pending.Evidence
	var e error
	if len(c.Evidence) > 0 {
		if e = selfEvidence(w, c.Ref, c.Evidence); e != nil {
			return item, e
		}
		evidence, e = bindWorkEvidence(book, w, c.Evidence, resolve)
		if e != nil {
			return item, e
		}
	} else if pending.Source == "" {
		return item, errors.New("the original result needs actual evidence refs before ref-only recovery")
	}
	if len(evidence) == 0 {
		return item, errors.New("the result evidence is still unresolved")
	}

	if pending.Source != "" && !slices.Equal(evidence, pending.Evidence) {
		return item, errors.New("retry the same pinned recovery evidence; a changed result needs a new report")
	}
	if !trialBasisCurrent(book, evidence) {
		return item, errors.New("recovery evidence changed; read the current original")
	}
	material, e := book.ReadSources([]string{c.Ref})
	if e != nil {
		return item, e
	}
	originals := material["observations"].([]notebook.Observation)
	if len(originals) != 1 || originals[0].Kind != "agent" || originals[0].Title != "Work result and remaining conditions" {
		return item, errors.New("the original pending result is unavailable")
	}
	var original workCompletion
	if e = json.Unmarshal([]byte(originals[0].Content), &original); e != nil {
		return item, e
	}
	if workHash(original) != pending.Result.Signature {
		return item, errors.New("the original result no longer matches its saved identity")
	}
	if pending.Source == "" {
		pending.Evidence = evidence
		data, _ := json.Marshal(struct {
			Result   string   `json:"result"`
			Turn     string   `json:"turn"`
			Evidence []string `json:"evidence"`
		}{c.Ref, pending.Turn, evidence})
		e = captureWorkTrialSource(book, w, workObservation{Kind: "agent", Title: "Recovered work result evidence", Content: string(data)}, &pending.Source, "result-recovery:"+c.Ref, item, save, commit)
		if e != nil {
			return item, e
		}
	}
	item.ID = pending.Source
	diagnostic, e := book.ReadSources([]string{pending.Source})
	if e != nil {
		return item, e
	}
	observed := diagnostic["observations"].([]notebook.Observation)
	want, _ := json.Marshal(struct {
		Result   string   `json:"result"`
		Turn     string   `json:"turn"`
		Evidence []string `json:"evidence"`
	}{c.Ref, pending.Turn, pending.Evidence})
	if len(observed) != 1 || observed[0].Title != "Recovered work result evidence" || observed[0].Content != string(want) {
		return item, errors.New("the recovered result binding is unavailable or changed")
	}
	if pending.Complete {
		return item, nil
	}
	if p := pending.Procedure; p != nil && (p.Trace.Stalls > 0 || p.View != nil || p.Trace.Config.ID != notebook.DefaultProcedure().ID || p.Mixed) && !pending.Queued {
		ids := []string{}
		for _, ref := range evidence {
			if strings.HasPrefix(ref, "source:") {
				ids = append(ids, strings.TrimPrefix(ref, "source:"))

			}
		}
		proof := []notebook.Source{}
		if len(ids) > 0 {
			proof, e = book.OutcomeSources(ids)
			if e != nil {
				return item, e
			}
		}
		snapshot, e := book.OutcomeSources([]string{c.Ref})
		if e != nil {
			return item, e
		}
		if len(w.Reasoning.PendingProcedures) >= 8 {
			return item, errors.New("prepare this receipt to recover earlier procedure feedback first")
		}
		auxiliary := workQualityEvidence{}
		if pending.Turn == w.Reasoning.TurnID {
			auxiliary = additional
		}
		proof, e = mergeWorkQualityEvidence(proof, auxiliary, original.Sufficient)
		if e != nil {
			excludeWorkQuality(w, p, c.Ref, original.Sufficient)
		} else {
			trace := p.Trace
			trace.Mixed = p.Mixed
			p.Pending = &notebook.ProcedureSample{Trace: trace, Sufficient: original.Sufficient, Evidence: proof, Snapshot: snapshot[0]}
			w.Reasoning.PendingProcedures = append(w.Reasoning.PendingProcedures, p)
		}
		pending.Queued = true
		if e = save(); e != nil {
			return item, e
		}
	}
	if e = recoverProcedureFeedback(book, w, save, commit); e != nil {
		return item, e
	}
	// Execution checks already interpreted by this repair need no second manual
	// reflection. Human intent and general agent evidence retain their own path.
	consumed := []string{c.Ref, pending.Source}
	for _, ref := range evidence {
		if !strings.HasPrefix(ref, "source:") {
			continue
		}
		v, e := book.ReadSources([]string{strings.TrimPrefix(ref, "source:")})
		if e != nil {
			return item, e
		}
		for _, o := range v["observations"].([]notebook.Observation) {
			if o.Kind == "test" || o.Kind == "code" {
				consumed = append(consumed, o.ID)
			}
		}
	}
	if e = commit(func() error { _, e := book.Ingest(notebook.Draft{Sources: consumed}); return e }); e != nil {
		return item, e
	}
	pending.Result.Ready, pending.Result.Evidence = true, evidence
	pending.Complete = true
	pending.CompletedAt = time.Now().UTC()
	trimResultRecoveryHistory(w.Reasoning)
	if w.Result != nil && w.Result.Source == c.Ref && pending.Turn == w.Reasoning.TurnID {
		if p := pending.Procedure; p != nil && p.View != nil && p.View.Ineligible && original.Sufficient != nil {
			pending.Result.Unresolved = true
		}
		w.Result = pending.Result
		w.Reasoning.Result = w.Result
		if current := w.Reasoning.Procedure; current != nil {
			current.Indexed = c.Ref
			current.CloseReport = false
			if p := pending.Procedure; p != nil {
				current.Pending, current.View = p.Pending, p.View
			}
		}
	}
	return item, save()
}

// Keep recent successful repair receipts bounded. Unresolved originals are
// never evicted; the ordinary session capacity still bounds accepted work.
func trimResultRecoveryHistory(r *reasoningProgress) {
	done := []string{}
	for id, p := range r.PendingResults {
		if p.Complete {
			done = append(done, id)
		}
	}
	sort.Slice(done, func(i, j int) bool {
		a, b := r.PendingResults[done[i]], r.PendingResults[done[j]]
		if a.CompletedAt.Equal(b.CompletedAt) {
			return done[i] < done[j]
		}
		return a.CompletedAt.Before(b.CompletedAt)
	})
	for len(done) > 64 {
		delete(r.PendingResults, done[0])
		done = done[1:]
	}
}

func pendingResultCount(r *reasoningProgress) int {
	if r == nil {
		return 0
	}
	n := 0
	for _, p := range r.PendingResults {
		if !p.Complete {
			n++
		}
	}
	return n
}
