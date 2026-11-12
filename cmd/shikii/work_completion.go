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

// selfEvidence refuses evidence that names the result itself, or another report this work wrote about itself (an earlier result,
// an ending review). Such a report is saved as a source of its own (under the next observedN name, and as result), so a result with
// nothing observed could cite it and be complete; two reports could cite each other (audit of 0.31.0).
//
// A report is known by what it is, not only by the note this receipt took of it: the source's own title (workReportSource). Three
// rounds of the audit each found a name under which a report reached a receipt without the note (after a preparation again,
// after a save that failed once, as a pending observation evidenceN of this or of an earlier request, in a record written before
// the notes). The note (Reports) is still what keeps a report out of the references handed over and listed.
func selfEvidence(book notebook.Book, w *workState, result string, refs []string) error {
	for _, ref := range refs {
		id := w.Sources[ref]
		if id == "" {
			id = ref
		}
		what := ""
		switch {
		case result != "" && id == result:
			what = "this result itself"
		case w.Reports[id]:
			what = "a report this work wrote about itself (" + workReportKind(book, id) + "), not something it observed"
		case workReportSource(book, id):
			what = "a report or a record shikii wrote about a work itself (" + workReportKind(book, id) + "), not something that was observed"
		default:
			continue
		}
		cite := "an observation of this call as new1, a saved observation, a page you read"
		if !w.Standalone {
			cite += " or current"
		}
		return fmt.Errorf("evidence %q is %s: cite what you checked (%s), or leave evidence out", ref, what, cite)
	}
	return nil
}

// The sources a work writes about itself, by the titles it gives them (kind agent): a result, its ending review, the source that
// binds a result to its evidence, and the record of the procedure a result closed (written by notebook.RecordProcedure).
const (
	workResultTitle   = "Work result and remaining conditions"
	workReviewTitle   = "Evidence-linked work judgment and ending review"
	workRecoveryTitle = "Recovered work result evidence"
	workProcedureName = "Procedure execution"
)

// workSourceTitles is every title shikii gives a source it writes itself with kind agent, and whether evidence may cite it. The
// reports and the archives shikii keeps of the work's own state are no evidence; the records of what the agent did (a trial, a
// measured reading, a procedure it executed) are cited like the observations they record. A list of the reports alone missed the
// archives, and each new archive would have been missed again (audits of 0.31.0, four rounds): a test now reads every source
// shikii writes in the code and fails on a title that is not here (TestEveryTitleShikiiWritesIsClassified).
var workSourceTitles = map[string]bool{
	workResultTitle:   false,
	workReviewTitle:   false,
	workRecoveryTitle: false,
	workProcedureName: false,
	"Required wiki note and original prepared bindings":           false,
	"Required work and bindings at the session capacity boundary": false,
	"Pending wiki work before replacing preparation":              false,
	"Work trial plan":                         true,
	"Work trial execution":                    true,
	"Work trial comparison":                   true,
	"Measured reading execution":              true,
	"Procedure execution with cited evidence": true,
}

// workReport reports whether a source with this kind and title is one shikii wrote about the work itself (a report or an archive
// of the work's state), which evidence never cites.
func workReport(kind, title string) bool {
	citable, known := workSourceTitles[workTitle(title)]
	return kind == "agent" && known && !citable
}

// workTitle is the form a title takes in workSourceTitles: the spaces around it are not part of it. Every comparison with the
// table goes through here (the refusal trimmed the title and the reading of a saved source did not: audit of 0.31.0).
func workTitle(title string) string {
	return strings.TrimSpace(title)
}

// reportTitleRefusal refuses an observation the agent saves under a title shikii gives its own reports and archives: it would be
// taken for one and never cited. Every entrance that saves the agent's observation asks it first (work record, wiki capture through
// the command line, MCP and HTTP: wiki capture took the title without a word, audit of 0.31.0). A record of what the agent did
// (a trial, a reading, an outcome) keeps its title.
func reportTitleRefusal(title string) error {
	if citable, shikiis := workSourceTitles[workTitle(title)]; shikiis && !citable {
		return fmt.Errorf("the title %q is the one shikii gives its own reports and records; give the observation a title of its own (what you checked)", title)
	}
	return nil
}

// workReportSource reads a saved source and reports whether it is one a work wrote about itself. A reference that is no saved
// observation (current, a page, a conversation) is none.
func workReportSource(book notebook.Book, id string) bool {
	kind, title := workSourceTitle(book, id)
	return workReport(kind, title)
}

// workSourceTitle is the kind and the title of a saved source, and nothing for a reference that is no saved observation.
func workSourceTitle(book notebook.Book, id string) (kind, title string) {
	material, err := book.ReadSources([]string{id})
	if err != nil {
		return "", ""
	}
	originals, _ := material["observations"].([]notebook.Observation)
	if len(originals) != 1 {
		return "", ""
	}
	return originals[0].Kind, originals[0].Title
}

// workReportKind says which of a work's own sources a refusal names, by its title: an archive of the work's state was called
// "an earlier result or an ending review" (audit J of 0.31.0).
func workReportKind(book notebook.Book, id string) string {
	_, title := workSourceTitle(book, id)
	switch workTitle(title) {
	case workResultTitle:
		return "an earlier result"
	case workReviewTitle:
		return "an ending review"
	case workRecoveryTitle:
		return "the source that binds a result to its evidence"
	case workProcedureName:
		return "the record of a procedure a result closed"
	case "":
		return "a result, an ending review or the record of a procedure"
	}
	return fmt.Sprintf("%q, an archive of the work's state", workTitle(title))
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
		if len(originals) != 1 || originals[0].Title != workResultTitle || originals[0].Content != string(data) {
			return workItem{Kind: "result", Ref: "result"}, errors.New("the saved result changed or is unavailable")
		}
		if w.Result.Ready {
			if e := selfEvidence(book, w, w.Result.Source, c.Evidence); e != nil {
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
		_, id, err = captureWorkObservation(book, w, workObservation{Kind: "agent", Title: workResultTitle, Content: string(data), Reference: evidenceVersion, report: true}, save, commit)
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
	if err = selfEvidence(book, w, id, c.Evidence); err != nil {
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
