package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/hayaenfj/shikii/internal/notebook"
)

func validateWorkCompletion(c *workCompletion) error {
	if c == nil {
		return nil
	}
	if c.Ref != "" {
		if !sourceIDText(c.Ref) || len(c.Evidence) > 4 || c.Changed != "" || c.Checked != "" || c.Remaining != "" || c.Sufficient != nil {
			return errors.New("result recovery needs only ref and actual evidence refs")
		}
		return nil
	}
	if !assignmentText(c.Changed, 12000, true) || !assignmentText(c.Checked, 12000, false) || !assignmentText(c.Remaining, 4000, false) || len(c.Evidence) > 4 {
		return errors.New("result needs changed (1-12000 bytes), optional checked (12000), remaining (4000), and up to four evidence refs")
	}
	if c.Sufficient != nil && len(c.Evidence) == 0 {
		return errors.New("result.sufficient needs captured check evidence; omit it when quality is unknown")
	}
	return nil
}

// evidenceUse is what cites the evidence: what may be cited is the same for all, and the refusal names what each takes.
type evidenceUse uint8

const (
	forResult  evidenceUse = iota // a result, and a recovered result
	forTrial                      // the plan and the result of a trial
	forOutcome                    // the sources of a procedure outcome: saved observations only
)

// selfEvidence refuses evidence that is no observation: the result itself, a report or an archive a work wrote about itself (known
// by what the saved source is, notebook.WorkSource, and by the note this receipt took of it, Reports), and a page every source of
// which is such a report. It is the one check of what a result, a trial and an outcome cite. The audits of 0.31.0 found, round
// after round, a way through which a report passed as evidence while each entrance had a check of its own: a saved source, an
// archive, a trial, a procedure outcome, a page written from the work's own result.
//
// An outcome may cite this work's result when the result cites evidence of its own (recording.md: result supplies sources for
// notes/outcomes); a result with nothing cited is what it would launder.
func selfEvidence(book notebook.Book, w *workState, use evidenceUse, result string, refs []string) error {
	for _, ref := range refs {
		what := ""
		if article, ok := w.Articles[ref]; ok {
			if pageOfReports(book, w, article.ID, result) {
				what = "lesson:" + article.ID + ", a page whose every source is a report or an archive shikii wrote about a work, not something that was observed"
			}
		} else {
			id := w.Sources[ref]
			if id == "" {
				id = ref
			}
			switch {
			case result != "" && id == result, use == forResult && ref == "result":
				what = "this result itself" // "result" in the record of a result names the result being recorded
			case use == forOutcome && w.Result != nil && id == w.Result.Source && len(w.Result.Evidence) > 0:
				// the result with the evidence it cites
			case use == forOutcome && w.Result != nil && id == w.Result.Source:
				what = "this work's result, which cites no evidence of its own (record the result with the evidence it rests on first)"
			case w.Reports[id]:
				what = "a report this work wrote about itself (" + workReportKind(book, id) + "), not something it observed"
			case workReportSource(book, id):
				what = "a report or a record shikii wrote about a work itself (" + workReportKind(book, id) + "), not something that was observed"
			}
		}
		if what == "" {
			continue
		}
		if use == forOutcome {
			return fmt.Errorf("outcome source %q is %s: an outcome cites the observations it rests on (an observation of this call as new1, or a saved observation)", ref, what)
		}
		cite := "an observation of this call as new1, a saved observation, a page you read"
		if !w.Standalone {
			cite += " or current"
		}
		return fmt.Errorf("evidence %q is %s: cite what you checked (%s), or leave evidence out", ref, what, cite)
	}
	return nil
}

// pageOfReports reports whether every source of the live page is a report or an archive a work wrote about itself (or the result
// being recorded): a page written from them is no observation (audit K of 0.31.0: a result with nothing observed cited the page its
// own note wrote from its earlier result, and was complete).
func pageOfReports(book notebook.Book, w *workState, id, result string) bool {
	live, err := book.LiveWhere(func(p notebook.Page) bool { return p.ID == id })
	if err != nil || len(live) != 1 || len(live[0].Sources) == 0 {
		return false
	}
	for _, s := range live[0].Sources {
		if !(s.ID == result || w.Reports[s.ID] || (w.Result != nil && s.ID == w.Result.Source) || workReportSource(book, s.ID)) {
			return false
		}
	}
	return true
}

// workReportSource reads a saved source and reports whether it is one a work wrote about itself (a report or an archive of its
// state, notebook.WorkSource), which evidence never cites. A reference that is no saved observation (current, a page, a
// conversation) is none.
func workReportSource(book notebook.Book, id string) bool {
	k, ok := workSourceOf(book, id)
	return ok && !k.Citable()
}

// workSourceOf is the work source a saved source is.
func workSourceOf(book notebook.Book, id string) (notebook.WorkSource, bool) {
	material, err := book.ReadSources([]string{id})
	if err != nil {
		return notebook.WorkSource{}, false
	}
	originals, _ := material["observations"].([]notebook.Observation)
	if len(originals) != 1 {
		return notebook.WorkSource{}, false
	}
	return notebook.WorkSourceOf(originals[0])
}

// isWorkSource reports whether a saved observation is the work source k.
func isWorkSource(o notebook.Observation, k notebook.WorkSource) bool {
	got, ok := notebook.WorkSourceOf(o)
	return ok && got == k && o.Kind == "agent"
}

// workReportKind says which of a work's own sources a refusal names (an archive of the work's state was called "an earlier result
// or an ending review": audit J of 0.31.0).
func workReportKind(book notebook.Book, id string) string {
	if k, ok := workSourceOf(book, id); ok {
		return k.Describe()
	}
	return "a result, an ending review or the record of a procedure"
}

// currentRef is "current, " in a sentence that lists what evidence can cite, and nothing for a receipt without hook ids: it has no
// current request. The sentences that list current in their text take it from here; the refusals that add it to a list (selfEvidence,
// the list of usable references) ask w.Standalone the same way. A refusal or a recording contract that offered current to such a
// receipt sent the agent into a second refusal (audits of 0.31.0, two rounds: each round found the sentences the last one missed).
func currentRef(w *workState) string {
	if w == nil || w.Standalone {
		return ""
	}
	return "current, "
}

// compactHowToRecord is the short recording contract of a reply of prepare, for this receipt.
func compactHowToRecord(w *workState) string {
	return strings.Replace(compactWorkHowTo, "(current, newN", "("+currentRef(w)+"newN", 1)
}

// markReport notes a source this work wrote about itself.
func markReport(w *workState, id string) {
	if id == "" {
		return
	}
	if w.Reports == nil {
		w.Reports = map[string]bool{}
	}
	w.Reports[id] = true
}

// Save the original agent report before interpretation or placement. Local
// evidence refs are pinned separately; neither the report nor a saved start is
// an observed success, a human answer, or an evaluation of delivered memory.
func captureWorkCompletion(book notebook.Book, w *workState, c workCompletion, resolve func([]string) ([]string, error), save func() error, commit workCommitter) (workItem, error) {
	data, err := json.Marshal(c)
	if err != nil {
		return workItem{Kind: "result", Ref: "result"}, err
	}
	signature := workHash(c)
	if w.Result != nil && w.Result.Signature != signature {
		w.Result = nil
		w.Reasoning.Result = nil
		if err := save(); err != nil {
			return workItem{Kind: "result", Ref: "result"}, err
		}
	}
	id, evidenceVersion := "", ""
	if w.Result != nil && w.Result.Signature == signature {
		material, e := book.ReadSources([]string{w.Result.Source})
		if e != nil {
			return workItem{Kind: "result", Ref: "result"}, e
		}
		originals := material["observations"].([]notebook.Observation)
		if len(originals) != 1 || !isWorkSource(originals[0], notebook.WorkResult) || originals[0].Content != string(data) {
			return workItem{Kind: "result", Ref: "result"}, errors.New("the saved result changed or is unavailable")
		}
		if w.Result.Ready {
			if e := selfEvidence(book, w, forResult, w.Result.Source, c.Evidence); e != nil {
				return workItem{Kind: "result", Ref: "result", ID: w.Result.Source}, e
			}
			bound, e := bindWorkEvidence(book, w, c.Evidence, resolve)
			if e != nil {
				return workItem{Kind: "result", Ref: "result", ID: w.Result.Source}, e
			}
			if slices.Equal(bound, w.Result.Evidence) {
				id = w.Result.Source
			} else {
				evidenceVersion = "result evidence " + workHash(bound)
			}
		} else {
			id = w.Result.Source
		}
	}
	if id == "" {
		// evidence that is no observation is refused before the result is saved: a refused result left waiting, and the record
		// sent again as the refusal said did not close the work (audit M of 0.31.0). The result itself is checked again below,
		// once it has an id.
		if err = selfEvidence(book, w, forResult, "", c.Evidence); err != nil {
			return workItem{Kind: "result", Ref: "result"}, err
		}
		_, id, err = captureWorkObservation(book, w, workArchive(notebook.WorkResult, string(data), evidenceVersion), save, commit)
	}
	item := workItem{Kind: "result", Ref: "result", ID: id}
	if err != nil {
		return item, err
	}
	w.Sources["result"] = id
	w.Result = &workCompletionRef{ReviewGeneration: w.Reasoning.Generation, Source: id, Signature: signature, Unresolved: c.Remaining != "" || c.Sufficient != nil && !*c.Sufficient}
	w.Reasoning.Result = w.Result
	if w.Reasoning.PendingResults == nil {
		w.Reasoning.PendingResults = map[string]*workResultRecovery{}
	}
	if w.Reasoning.PendingResults[id] == nil {
		pending := &workResultRecovery{Result: w.Result, Turn: w.Reasoning.TurnID}
		if p := w.Reasoning.Procedure; p != nil {
			copy := *p
			copy.Pending = nil
			copy.Indexed = ""
			copy.CloseReport = false
			pending.Procedure = &copy
		}
		w.Reasoning.PendingResults[id] = pending
	}
	if err = save(); err != nil {
		return item, err
	}
	if err = selfEvidence(book, w, forResult, id, c.Evidence); err != nil {
		return item, err
	}
	evidence, err := bindWorkEvidence(book, w, c.Evidence, resolve)
	if err != nil {
		return item, err
	}
	w.Result.Evidence = evidence
	w.Result.Ready = true
	w.Result.ReviewGeneration = w.Reasoning.Generation
	delete(w.Reasoning.PendingResults, id)
	return item, save()
}

// The report covers its submitted batch. Recovery may finish that same review,
// but later work evidence requires another reported result or explicit review.
func workResultReviewCurrent(w *workState) bool {
	if w == nil || w.Result == nil || w.Reasoning == nil {
		return false
	}
	r := w.Reasoning
	return w.Result.ReviewGeneration > 0 && w.Result.ReviewGeneration == r.Generation || r.Finish != nil && r.Finish.Generation == r.Generation
}

func finishWorkCompletion(book notebook.Book, w *workState, resolve func([]string) ([]string, error), save func() error, commit workCommitter) (workItem, error) {
	if !workResultReviewCurrent(w) {
		return workItem{Kind: "reasoning", Ref: "reasoning"}, errors.New("work changed after this report; record its changed result or an explicit ending review")
	}

	if !w.Result.Ready {
		return workItem{Kind: "reasoning", Ref: "reasoning"}, errors.New("retry the saved result with valid evidence refs before finishing")
	}
	if len(w.Result.Evidence) > 0 && !trialBasisCurrent(book, w.Result.Evidence) {
		return workItem{Kind: "reasoning", Ref: "reasoning"}, errors.New("the result evidence changed; read the current sources and report the changed result")
	}
	// Internal ending captures do not introduce new work evidence. Persist
	// their generation with the reviewed scope in the same atomic session save.
	previousReview := w.reviewingResult
	w.reviewingResult = true
	defer func() { w.reviewingResult = previousReview }()
	keys := []string{}
	for key, capture := range w.Captures {
		if capture.ID == "" && capture.Original != nil {
			keys = append(keys, key)
		}
	}
	slices.Sort(keys)
	for _, key := range keys {
		original := *w.Captures[key].Original
		original.source = w.Captures[key].Source // a source shikii writes is finished as its kind
		if _, _, e := captureWorkObservation(book, w, original, save, commit); e != nil {
			return workItem{Kind: "reasoning", Ref: "reasoning"}, e
		}
	}
	refs := []string{"result"}
	if w.Sources["procedure"] != "" {
		refs = append(refs, "procedure")
	}
	// The source retains the narrative once. The ending review links it without
	// restating that narrative in focus, reason, next_action and result.
	r := workReasoning{Phase: "finish", Focus: "Review the saved work result", Assessment: "retain", Reason: "The agent report and its remaining conditions are retained in the result source; saving does not establish success.", Evidence: refs, NextAction: "Use the saved result and unresolved conditions for the next task.", Result: "The work report is saved; see its source for changes, checks and remaining conditions."}
	if w.Result.Unresolved {
		r.Assessment = "unresolved"
	}
	item, err := recordWorkReasoning(book, w, r, resolve, save, commit)
	if err != nil {
		return item, err
	}
	ids := []string{w.Result.Source}
	if item.ID != "" {
		ids = append(ids, item.ID)
	}
	// The ending report already interpreted these execution checks. Retain their
	// originals and refs without requesting another manual reflection. Human
	// statements and general agent observations still use the normal review path.
	checks := []string{}
	for _, ref := range w.Result.Evidence {
		if !strings.HasPrefix(ref, "source:") {
			continue
		}
		checks = append(checks, strings.TrimPrefix(ref, "source:"))
	}
	if len(checks) > 0 {
		v, e := book.ReadSources(checks)
		if e != nil {
			return item, e
		}
		for _, o := range v["observations"].([]notebook.Observation) {
			if o.Kind == "test" || o.Kind == "code" {
				ids = append(ids, o.ID)
			}
		}
	}
	err = commit(func() error { _, e := book.Ingest(notebook.Draft{Sources: ids}); return e })
	return item, err
}

func workStorageState(w *workState, r *workResult) {
	if w.Result == nil {
		return
	}
	status := "saved"
	if !r.Complete || r.Reasoning.WikiPending {
		status = "saved-pending"
		r.NextAction = "Retry only the failed or pending refs with this receipt; keep saved evidence refs. Read reasoning.pending_notes for the retained original draft."
		if w.Result.Ready && !workResultReviewCurrent(w) {
			r.NextAction = "The original report is saved. Work changed afterward; record the changed result or an explicit ending review. A ref-only retry cannot cover new work."
		}
		for _, item := range r.Errors {
			if item.Kind == "procedure" {
				r.NextAction = "The report is saved. Prepare with this receipt to retry procedure feedback and finish automatically; do not resend the result."
				break
			}
		}
	}
	r.Storage = map[string]any{"status": status, "result_source": w.Result.Source}
}
