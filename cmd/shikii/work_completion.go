package main

import (
	"encoding/json"
	"errors"
	"strings"

	"github.com/hayaenfj/shikii/internal/notebook"
)

func validateWorkCompletion(c *workCompletion) error {
	if c == nil {
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
	_, id, err := captureWorkObservation(book, w, workObservation{Kind: "agent", Title: "Work result and remaining conditions", Content: string(data)}, save, commit)
	item := workItem{Kind: "result", Ref: "result", ID: id}
	if err != nil {
		return item, err
	}
	w.Sources["result"] = id
	w.Result = &workCompletionRef{Source: id, Signature: signature, Unresolved: c.Remaining != "" || c.Sufficient != nil && !*c.Sufficient}
	w.Reasoning.Result = w.Result
	if err = save(); err != nil {
		return item, err
	}
	evidence, err := bindWorkEvidence(book, w, c.Evidence, resolve)
	if err != nil {
		return item, err
	}
	w.Result.Evidence = evidence
	w.Result.Ready = true
	return item, save()
}

func finishWorkCompletion(book notebook.Book, w *workState, resolve func([]string) ([]string, error), save func() error, commit workCommitter) (workItem, error) {
	if !w.Result.Ready {
		return workItem{Kind: "reasoning", Ref: "reasoning"}, errors.New("retry the saved result with valid evidence refs before finishing")
	}
	if len(w.Result.Evidence) > 0 && !trialBasisCurrent(book, w.Result.Evidence) {
		return workItem{Kind: "reasoning", Ref: "reasoning"}, errors.New("the result evidence changed; read the current sources and report the changed result")
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
		for _, item := range r.Errors {
			if item.Kind == "procedure" {
				r.NextAction = "The report is saved. Prepare with this receipt to retry procedure feedback and finish automatically; do not resend the result."
				break
			}
		}
	}
	r.Storage = map[string]any{"status": status, "result_source": w.Result.Source}
}
