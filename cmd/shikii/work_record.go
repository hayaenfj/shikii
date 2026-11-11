package main

import (
	"errors"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/hayaenfj/shikii/internal/notebook"
)

// workAfterWrite is a deterministic failure point for persistence tests.
var workAfterWrite = func(kind string) error { return nil }

func validateWorkRecording(req workRequest) error {
	var beside []string
	for _, field := range []struct{ name, value string }{{"session_id", req.SessionID}, {"turn_id", req.TurnID}, {"query", req.Query}, {"request", req.Request}, {"proposal", req.Proposal}, {"conditions", req.Conditions}, {"cursor", req.Cursor}} {
		if field.value != "" {
			beside = append(beside, field.name)
		}
	}
	if len(beside) > 0 {
		text := "record uses its receipt; do not replace prepared task identifiers (leave out " + strings.Join(beside, ", ")
		if req.SessionID != "" || req.TurnID != "" { // the receipt's own ids were taken out by the caller: these are another request's
			text += "; session_id and turn_id are not those of the receipt: for another request, prepare with its own ids"
		}
		return errors.New(text + ")")
	}
	if len(req.Observations) > 4 || len(req.Notes) > 2 || len(req.Reviews) > 128 || len(req.Memory) > 256 || len(req.Reviewed) > 64 || len(req.Maintenance) > 8 || len(req.Discoveries) > 4 || len(req.Outcomes) > 2 {
		return errors.New("the recording batch exceeds its fixed item limits")
	}
	if req.Result == nil && req.Reading == nil && req.Trial == nil && req.Comparison == nil && req.Reasoning == nil && req.Agreed == nil && req.Open == nil && req.ContinuesPrevious == nil && len(req.Observations)+len(req.Notes)+len(req.Reviews)+len(req.Memory)+len(req.Reviewed)+len(req.Maintenance)+len(req.Discoveries)+len(req.Outcomes) == 0 {
		return errors.New("record needs content that was judged or observed")
	}
	return validateWorkCompletion(req.Result)
}

func recordWork(root, scope string, req workRequest) (reply any, retErr error) {
	if req.Result != nil && req.Result.Ref == "" && req.Reading != nil && req.Reading.Ref == "" {
		if req.Result.Sufficient != nil && req.Reading.Sufficient != nil && *req.Result.Sufficient != *req.Reading.Sufficient {
			return nil, errors.New("result and reading quality disagree; retain the actual checked result")
		}
		if req.Result.Sufficient == nil {
			req.Result.Sufficient = req.Reading.Sufficient
			if len(req.Result.Evidence) == 0 {
				req.Result.Evidence = req.Reading.Evidence
			}
		}
	}
	token, err := workDecode(req.Receipt)
	if err != nil {
		return nil, err
	}
	if (req.SessionID == "" || req.SessionID == token.Session) && (req.TurnID == "" || req.TurnID == token.Turn) {
		// the ids of the receipt's own request add nothing and change nothing: an agent that repeats them from the hook notice or
		// the Stop block ("use the same request IDs") is not refused for it
		req.SessionID, req.TurnID = "", ""
	}
	if err = validateWorkRecording(req); err != nil {
		return nil, err
	}
	dir, err := openWorkSessions(root)
	if err != nil {
		return nil, err
	}
	defer dir.Close()
	name := filepath.Base(sessionPath(root, token.Session))
	unlock, err := workLock(dir, name)
	if err != nil {
		return nil, err
	}
	defer unlock()
	w, _, err := loadWorkState(dir, scope, token)
	if err != nil {
		return nil, err
	}
	if w.Saved == nil || w.Captures == nil || w.Sources == nil || w.Articles == nil || w.Keys == nil || w.Pending == nil || w.Expected == nil || w.PendingKeys == nil || w.PendingOrigins == nil || w.Replies == nil {
		return nil, errors.New("the prepared recording metadata is incomplete; prepare again")
	}
	if len(w.Captures) > 32 {
		return nil, errors.New("this recording context is full; prepare again")
	}
	if w.Standalone && (req.Agreed != nil || req.Open != nil || req.ContinuesPrevious != nil) {
		return nil, errors.New("an agent-supplied task is not a human agreement; use the current hook context for planning")
	}
	if req.Reasoning != nil {
		if err := validateReasoning(*req.Reasoning); err != nil {
			return nil, err
		}
	}
	persistence := &workRecordPersistence{dir: dir, name: name, scope: scope, token: token, w: w, checkpoint: &workSessionCheckpoint{}}
	save := persistence.save
	commit := workCommitter(persistence.commit)
	book := notebook.Book{Root: root, Scope: scope}
	if e := validateWorkQualityAgreement(book, w, req); e != nil {
		return nil, e
	}
	reportSource := ""
	bindingScope := ""
	if req.Result != nil && req.Result.Ref != "" && w.Result != nil && !w.Result.Ready && w.Result.Source == req.Result.Ref && workResultReviewCurrent(w) {
		bindingScope = w.Result.Source
	}
	// Ref-only repairs may advance bookkeeping for an already reviewed batch.
	// New observations, reports, trials or changed decisions cannot do so.
	recoveryReview := workResultReviewCurrent(w) && onlyWorkRecovery(req)
	reviewStamped := false
	stampReview := func() error {
		if reviewStamped || w.Result == nil || reportSource != w.Result.Source && (!recoveryReview || !w.Result.Ready) {
			return nil
		}
		if e := save(); e != nil {
			return e
		}
		w.Result.ReviewGeneration = w.Reasoning.Generation
		if e := save(); e != nil {
			return e
		}
		reviewStamped = true
		return nil
	}
	result := workResult{Complete: true, Saved: []workItem{}}
	if req.Result != nil {
		result.Storage = map[string]any{"status": "unconfirmed"}
	}
	defer func() {
		finishWorkRecordReply(book, w, req, &result, stampReview)
		if retErr == nil {
			reply = result
		}
	}()
	add := func(item workItem) {
		if item.Error != "" {
			result.Complete = false
			result.Errors = append(result.Errors, item)
		} else {
			result.Saved = append(result.Saved, item)
		}
	}
	halt := func() bool { return persistence.halt(add) }

	local := map[string]string{}
	for i, o := range req.Observations {
		ref, id, err := captureWorkObservation(book, w, o, save, commit)
		item := workItem{Ref: ref, Kind: "observation", ID: id}
		if err != nil {
			item.Error = err.Error()
		} else {
			local["new"+strconv.Itoa(i+1)] = id
		}
		add(item)
		if halt() {
			return result, nil
		}
	}
	resolveSources := func(refs []string) ([]string, error) {
		sources := []string{}
		for _, ref := range refs {
			id := local[ref]
			if id == "" {
				id = w.Sources[ref]
			}
			if ref == "current" && !w.Standalone {
				var err error
				id, err = resolveCurrentWorkSource(book, w, save, commit)
				if err != nil {
					return nil, err
				}
			}
			if id == "" && heldSource(w, ref) {
				id = ref // the id of a source this receipt holds, as the replies print it
			}
			if id == "" {
				usable := sortedRefs(w.Sources)
				if !w.Standalone {
					usable = append(usable, "current")
				}
				return nil, errors.New(unknownSourceRef(ref, len(req.Observations), usable))
			}
			sources = append(sources, id)
		}
		return sources, nil
	}
	batch := &workRecordBatch{root: root, scope: scope, req: req, token: token, book: book, w: w, save: save, commit: commit, add: add, halt: halt, resolve: resolveSources, result: &result}
	qualityEvidence := workReadingQualityEvidence(book, w, req.Reading, resolveSources)
	var resultStopped bool
	reportSource, resultStopped = batch.recordResult(qualityEvidence, bindingScope, local)
	if resultStopped {
		return result, nil
	}
	// Queue feedback from the immutable result before unrelated indexing can fail.
	// Recovery of an earlier report must never prevent capture of this input.
	if w.Result != nil && w.Result.Ready {
		view, e := recordProcedureFeedback(book, w, req.Result, qualityEvidence, req.Reasoning == nil, save, commit)
		result.Procedure = view
		if e != nil {
			add(workItem{Kind: "procedure", Ref: "procedure", Error: e.Error()})
		}
	} else if e := recoverProcedureFeedback(book, w, save, commit); e != nil {
		add(workItem{Kind: "procedure", Ref: "procedure", Error: e.Error()})
	}
	noteRefs := make([]string, len(req.Notes))
	for i, note := range req.Notes {
		ref, normalized, e := reserveRequiredNote(book, w, note, save, commit)
		if e != nil {
			add(workItem{Ref: ref, Kind: "note", Error: e.Error()})
			return result, nil
		}
		noteRefs[i], req.Notes[i] = ref, normalized
	}
	batch.req = req
	if batch.recordActions() {
		return result, nil
	}
	noteSources, stopped := batch.recordNotes(noteRefs)
	if stopped {
		return result, nil
	}
	batch.recordReflection(noteSources)
	if w.Reasoning != nil && len(req.Notes) > 0 {
		w.Reasoning.WikiPending = len(w.Reasoning.RequiredNotes) > 0 || reasoningPublicationsPending(w)
		if e := save(); e != nil {
			add(workItem{Ref: "reasoning", Kind: "reasoning", Error: e.Error()})
		}
	}
	if req.Reasoning != nil {
		if result.Complete {
			item, e := recordWorkReasoning(book, w, *req.Reasoning, resolveSources, save, commit)
			if e != nil {
				item.Error = e.Error()
			}
			add(item)
		} else {
			add(workItem{Ref: "reasoning", Kind: "reasoning", Error: "save the failed or pending work items before recording this judgment"})
		}
	}
	if e := stampReview(); e != nil {
		add(workItem{Kind: "context", Ref: "result-review", Error: e.Error()})
	}
	if req.Reasoning == nil && w.Result != nil && result.Complete && (req.Result == nil || req.Result.Ref == "" || req.Result.Ref == w.Result.Source) {
		item, e := finishWorkCompletion(book, w, resolveSources, save, commit)
		if e != nil {
			item.Error = e.Error()
		} else if w.Reasoning.Procedure != nil && w.Reasoning.Procedure.CloseReport {
			w.Reasoning.Procedure.CloseReport = false
			if e = save(); e != nil {
				item.Error = e.Error()
			}
		}
		add(item)
	}
	result.Reasoning = currentReasoningView(book, w.Reasoning)
	batch.prepareProcedureReviews()
	return result, nil
}
