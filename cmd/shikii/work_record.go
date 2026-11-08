package main

import (
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/hayaenfj/shikii"
	"github.com/hayaenfj/shikii/internal/notebook"
	"github.com/hayaenfj/shikii/internal/store"
)

// workAfterWrite is a deterministic failure point for persistence tests.
var workAfterWrite = func(kind string) error { return nil }

func validateWorkRecording(req workRequest) error {
	if req.SessionID != "" || req.TurnID != "" || req.Query != "" || req.Request != "" || req.Proposal != "" || req.Conditions != "" || req.Cursor != "" {
		return errors.New("record uses its receipt; do not replace prepared task identifiers")
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
	if req.Result != nil && req.Reading != nil && req.Reading.Ref == "" {
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
	var interrupted error
	var commitMu sync.Mutex
	save := func() error {
		commitMu.Lock()
		defer commitMu.Unlock()
		if interrupted != nil {
			return interrupted
		}
		e := saveWorkState(dir, name, scope, token.Turn, w, false)
		if e != nil {
			interrupted = e
		}
		return e
	}
	commit := workCommitter(func(action func() error) error {
		commitMu.Lock()
		defer commitMu.Unlock()
		if interrupted != nil {
			return interrupted
		}
		ran := false
		e := commitWorkTurn(dir, name, scope, token, func() error {
			ran = true
			if action == nil {
				return nil
			}
			return action()
		})
		if !ran && e != nil {
			interrupted = e
		}
		return e
	})
	result := workResult{Complete: true, Saved: []workItem{}}
	if req.Result != nil {
		result.Storage = map[string]any{"status": "unconfirmed"}
	}
	defer func() {
		workStorageState(w, &result)
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
	halt := func() bool {
		_ = commit(nil)
		commitMu.Lock()
		e := interrupted
		commitMu.Unlock()
		if e == nil {
			return false
		}
		add(workItem{Ref: "remaining", Kind: "context", Error: "remaining operations stopped: " + e.Error()})
		return true
	}

	book := notebook.Book{Root: root, Scope: scope}
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
			if id == "" {
				return nil, fmt.Errorf("unknown source reference %q; use a prepared source or new observation", ref)
			}
			sources = append(sources, id)
		}
		return sources, nil
	}
	if req.Result != nil {
		item, e := captureWorkCompletion(book, w, *req.Result, resolveSources, save, commit)
		if e != nil {
			item.Error = e.Error()
		}
		add(item)
		if halt() {
			return result, nil
		}
	}
	// Queue feedback from the immutable result before unrelated indexing can fail.
	// Recovery of an earlier report must never prevent capture of this input.
	if w.Result != nil && w.Result.Ready {
		view, e := recordProcedureFeedback(book, w, req.Result, req.Reasoning == nil, save, commit)
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
	if req.Reading != nil {
		item, e := recordWorkReading(book, w, *req.Reading, resolveSources, save, commit)
		if e != nil {
			item.Error = e.Error()
		}
		add(item)
		if halt() {
			return result, nil
		}
	}
	if req.Trial != nil {
		item, e := recordWorkTrial(book, w, *req.Trial, resolveSources, save, commit)
		if e != nil {
			item.Error = e.Error()
		}
		add(item)
		if halt() {
			return result, nil
		}
	}
	if req.Comparison != nil {
		item, view, e := recordWorkComparison(book, w, *req.Comparison, resolveSources, save, commit)
		if e != nil {
			item.Error = e.Error()
		}
		result.Comparison = view
		add(item)
		if halt() {
			return result, nil
		}
	}
	for i, d := range req.Discoveries {
		item, e := recordWorkDiscovery(book, w, token, d, save, commit)
		item.Ref = "discovery" + strconv.Itoa(i+1)
		if e != nil {
			item.Error = e.Error()
		}
		add(item)
		if halt() {
			return result, nil
		}
	}
	for i, o := range req.Outcomes {
		item, e := recordWorkOutcome(book, w, o, resolveSources, save, commit)
		item.Ref = "outcome" + strconv.Itoa(i+1)
		if e != nil {
			item.Error = e.Error()
		}
		add(item)
		if halt() {
			return result, nil
		}
	}
	if req.Agreed != nil || req.Open != nil || req.ContinuesPrevious != nil {
		item, value, e := recordWorkPlan(root, scope, token, w, req, save)
		if e != nil {
			item.Error = e.Error()
		}
		result.Plan = value
		add(item)
		if halt() {
			return result, nil
		}
	}
	noteSources := []string{}
	for i, n := range req.Notes {
		w.ActiveNote = noteRefs[i]
		sources := append([]string{}, w.Pending[n.Ref].Sources...)
		resolveNote := func(refs []string) ([]string, error) {
			ids, e := resolveSources(refs)
			if e == nil {
				sources = append(sources, ids...)
				if w.Reasoning != nil {
					if entry := w.Reasoning.RequiredNotes[noteRefs[i]]; entry != nil {
						if entry.Sources == nil {
							entry.Sources = map[string]string{}
						}
						for j, ref := range refs {
							entry.Sources[ref] = ids[j]
						}
					}
				}
			}
			return ids, e
		}
		item, pending, e := recordWorkNote(book, w, n, resolveNote, save, commit)
		w.ActiveNote = ""
		if e == nil && pending == nil && item.ID != "" && w.Reasoning != nil {
			entry := w.Reasoning.RequiredNotes[noteRefs[i]]
			if entry != nil {
				if err := commit(func() error { _, err := book.Ingest(notebook.Draft{Sources: []string{entry.Source}}); return err }); err != nil {
					e = err
				} else {
					finishRequiredPublication(w, entry)
					delete(w.Reasoning.RequiredNotes, noteRefs[i])
				}
			}
		}
		if e == nil && pending == nil && item.ID != "" {
			noteSources = append(noteSources, sources...)
		}
		if item.Ref == "" {
			item.Ref = "note" + strconv.Itoa(i+1)
		}
		if e != nil {
			item.Error = e.Error()
			if noteRefs[i] != "" {
				item.Ref = noteRefs[i]
			}
		}
		if pending != nil {
			result.Pending = append(result.Pending, pending)
			result.Complete = false
		} else {
			add(item)
		}
		if halt() {
			return result, nil
		}
	}
	reflection := reflectRequest{Operation: "submit"}
	if len(noteSources) > 0 {
		reflection.Wiki = append(reflection.Wiki, notebook.Draft{Sources: uniqueWorkStrings(noteSources)})
	}
	for _, r := range req.Reviews {
		binding, ok := w.Reviews[r.Ref]
		if !ok {
			add(workItem{Ref: r.Ref, Kind: "review", Error: "unknown prepared review reference"})
			continue
		}
		reflection.Reviews = append(reflection.Reviews, reviewRequest{SourceID: binding.Source, TraceID: binding.Trace, Truth: r.Answer, Reason: r.Reason})
	}
	for _, m := range req.Memory {
		binding, ok := w.Memory[m.Ref]
		if !ok {
			add(workItem{Ref: m.Ref, Kind: "memory", Error: "unknown prepared memory reference"})
			continue
		}
		reflection.Memory = append(reflection.Memory, memoryAssessmentRequest{SourceID: binding.Source, Key: binding.Key, Result: m.Result, Decision: m.Decision, Evidence: m.Evidence, CorrectionResult: m.CorrectionResult})
	}
	sourceReviews := []string{}
	for _, ref := range req.Reviewed {
		if strings.HasPrefix(ref, "change") || strings.HasPrefix(ref, "knowledge") {
			item := workItem{Ref: ref, Kind: "collective_review"}
			key := "collective-review:" + ref
			if _, ok := w.Saved[key]; !ok {
				if e := workSavedCapacity(w, key); e != nil {
					item.Error = e.Error()
				} else if e := reviewCollective(book, w, ref); e != nil {
					item.Error = e.Error()
				} else {
					w.Saved[key] = item
					if e := save(); e != nil {
						item.Error = e.Error()
					}
				}
			}
			add(item)
		} else {
			sourceReviews = append(sourceReviews, ref)
		}
	}
	if len(sourceReviews) > 0 {
		sources, e := resolveSources(sourceReviews)
		if e != nil {
			add(workItem{Ref: "reviewed", Kind: "sources", Error: e.Error()})
		} else {
			reflection.Wiki = append(reflection.Wiki, notebook.Draft{Sources: sources})
		}
	}
	for _, m := range req.Maintenance {
		review, ok := w.Maintenance[m.Ref]
		if !ok {
			add(workItem{Ref: m.Ref, Kind: "maintenance", Error: "unknown prepared maintenance reference"})
			continue
		}
		review.Reason = m.Reason
		reflection.Maintenance = append(reflection.Maintenance, review)
	}
	if len(reflection.Reviews)+len(reflection.Memory)+len(reflection.Wiki)+len(reflection.Maintenance) > 0 {
		// Automatically checking a note's remainder must stay available at capacity.
		// It is a fresh check, not a separately requested durable assessment batch.
		persist := len(req.Reviews)+len(req.Memory)+len(req.Reviewed)+len(req.Maintenance) > 0
		key := "reflection:" + workHash(reflection)
		if prior, ok := w.Saved[key]; ok && persist {
			result.Reflection = w.Replies[key].Reflection
			add(prior)
		} else {
			var value any
			var e error
			if persist {
				e = workSavedCapacity(w, key)
			}
			if e == nil {
				value, e = runReflectWithCommit(root, scope, reflection, commit)
			}
			item := workItem{Ref: "reflection", Kind: "reflection"}
			if e != nil {
				item.Error = e.Error()
			} else {
				result.Reflection = value
				receipt := value.(reflectResult)
				if !receipt.Complete || receipt.LeftError != "" || len(receipt.Left) > 0 {
					item.Error = "some prepared reflection items remain; inspect reflection and resend corrected content"
				} else if persist {
					w.Saved[key] = item
					w.Replies[key] = workReply{Reflection: &receipt}
					if e = save(); e != nil {
						item.Error = e.Error()
					}
				}
			}
			add(item)
		}
	}
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
	if req.Reasoning == nil && w.Result != nil && result.Complete {
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
	if len(req.Outcomes)+len(req.Notes) > 0 {
		ids := []string{}
		for _, o := range req.Outcomes {
			if p, ok := w.Articles[o.Ref]; ok {
				ids = append(ids, p.ID)
			}
		}
		for _, item := range result.Saved {
			if item.Kind == "note" && item.ID != "" {
				ids = append(ids, item.ID)
			}
		}
		// Empty IDs would ask for every procedure, including unrelated ones.
		if len(ids) == 0 {
			return result, nil
		}
		tasks, e := book.OutcomeMaintenance(ids)
		if e != nil {
			add(workItem{Ref: "procedure_reviews", Kind: "maintenance", Error: e.Error()})
		} else {
			for _, task := range tasks {
				ref := ""
				for key, old := range w.Maintenance {
					if old.PageID == task.PageID && old.Revision == task.Revision && old.Key == task.Key {
						ref = key
						break
					}
				}
				if ref == "" {
					ref = "maintenance" + strconv.Itoa(len(w.Maintenance)+1)
					w.Maintenance[ref] = notebook.MaintenanceReview{PageID: task.PageID, Revision: task.Revision, Key: task.Key}
				}
				result.ProcedureReviews = append(result.ProcedureReviews, workProcedureReview{MaintenanceTask: task, Ref: ref})
			}
			if len(tasks) > 0 {
				if e = save(); e != nil {
					add(workItem{Ref: "procedure_reviews", Kind: "maintenance", Error: e.Error()})
				}
			}
		}
	}
	return result, nil
}

func recordWorkPlan(root, scope string, token workToken, w *workState, req workRequest, save func() error) (workItem, any, error) {
	item := workItem{Ref: "plan", Kind: "plan"}
	p := planningRequest{SessionID: token.Session, TurnID: token.Turn, Query: w.Query, Pages: w.Pages, ContinuesPrevious: req.ContinuesPrevious, workNonce: token.Nonce}
	var err error
	p.Agreed, err = resolveWorkFact(req.Agreed, w)
	if err != nil {
		return item, nil, err
	}
	p.Open, err = resolveWorkFact(req.Open, w)
	if err != nil {
		return item, nil, err
	}
	key := "plan:" + workHash(p)
	if prior, ok := w.Saved[key]; ok {
		return prior, w.Replies[key].Plan, nil
	}
	if e := workSavedCapacity(w, key); e != nil {
		return item, nil, e
	}
	value, err := runPlanning(root, scope, "submit", p)
	if err != nil {
		return item, nil, err
	}
	receipt := value.(planningReceipt)
	w.Replies[key] = workReply{Plan: &receipt}
	w.Saved[key] = item
	return item, value, save()
}

func resolveWorkFact(f *workFact, w *workState) (*planningFact, error) {
	if f != nil && f.Unknown && (f.Summary != "" || len(f.Quotes) > 0) {
		return nil, errors.New("unknown facts must not include a summary or quotations")
	}
	if f == nil || f.Unknown {
		return &planningFact{Unknown: true}, nil
	}
	p := &planningFact{Summary: f.Summary}
	for _, q := range f.Quotes {
		key := w.Keys[q.Ref]
		if key == "" {
			return nil, fmt.Errorf("unknown prepared quotation reference %q", q.Ref)
		}
		p.Evidence = append(p.Evidence, planningQuote{Source: key, Quote: q.Text})
	}
	return p, nil
}
func prepareWorkObservation(w *workState, o workObservation) (notebook.Observation, string, error) {
	var value notebook.Observation
	if o.Kind == "" {
		o.Kind = "agent"
	}
	if o.Reference == "" {
		o.Reference = "work record"
	}
	if o.Version == "" {
		o.Version = "recorded by shikii " + shikii.Version
	}
	if len(o.Content) > 64<<10 {
		return value, "", errors.New("an observation must fit 64 KiB; retain a long report in handoff and cite its relevant evidence")
	}
	value = notebook.Observation{Kind: o.Kind, Title: o.Title, Content: o.Content, Reference: o.Reference, Version: o.Version, ObservedAt: time.Now().UTC()}
	if err := value.Validate(); err != nil {
		return value, "", err
	}
	key := workHash(o)
	if _, exists := w.Captures[key]; !exists && len(w.Captures) >= 32 {
		return value, key, errors.New("this work has reached its observation limit; prepare again")
	}
	return value, key, nil
}

func captureWorkObservation(book notebook.Book, w *workState, o workObservation, save func() error, commit workCommitter) (string, string, error) {
	value, key, err := prepareWorkObservation(w, o)
	if err != nil {
		return "", "", err
	}

	memo, ok := w.Captures[key]
	if !ok {
		normalized := workObservation{Kind: value.Kind, Title: value.Title, Content: value.Content, Reference: value.Reference, Version: value.Version}
		index := 1
		for _, previous := range w.Captures {
			if n, err := strconv.Atoi(strings.TrimPrefix(previous.Ref, "observed")); err == nil && n >= index {
				index = n + 1
			}
		}
		memo = workCapture{Original: &normalized, Ref: "observed" + strconv.Itoa(index), At: value.ObservedAt, Version: value.Version}
		w.Captures[key] = memo
		if err := save(); err != nil {
			return memo.Ref, "", err
		}
	}
	if memo.ID != "" {
		return memo.Ref, memo.ID, nil
	}
	value.ObservedAt, value.Version = memo.At, memo.Version
	var captured notebook.Observation
	err = commit(func() error { var e error; captured, e = book.Capture(value); return e })
	if err != nil {
		return memo.Ref, "", err
	}
	if err = workAfterWrite("observation"); err != nil {
		return memo.Ref, captured.ID, err
	}
	memo.ID = captured.ID
	memo.Original, memo.Archive = nil, ""
	w.Captures[key] = memo
	w.Sources[memo.Ref] = memo.ID
	if err = save(); err != nil {
		return memo.Ref, memo.ID, err
	}
	return memo.Ref, memo.ID, nil
}

func recordWorkNote(book notebook.Book, w *workState, n workNote, resolve func([]string) ([]string, error), save func() error, commit workCommitter) (workItem, any, error) {
	item := workItem{Ref: n.Ref, Kind: "note"}
	d := n.Draft
	if n.Ref == "" && n.Target != "" && n.Target != "new" {
		allowed := d
		allowed.Sources = nil
		allowed.Passages = nil
		allowed.Learning = nil
		allowed.ChangeReason = ""
		if !reflect.DeepEqual(allowed, notebook.Draft{}) {
			return item, nil, errors.New("an existing article accepts sources, change_reason, exact edits, interpreted passages and learning; use wiki operations for structural changes")
		}
	}
	if (n.Target == "" || n.Target == "new") && len(n.Edits) > 0 {
		return item, nil, errors.New("exact edits require a prepared existing article target; a new note uses its draft content")
	}

	if n.Ref != "" {
		prior, ok := w.Pending[n.Ref]
		if !ok {
			return item, nil, errors.New("unknown pending note reference")
		}
		if n.Target == "" {
			return item, nil, errors.New("resolving a pending note needs target new or a prepared article reference")
		}
		if !reflect.DeepEqual(n.Draft, notebook.Draft{}) {
			return item, nil, errors.New("resolve a pending note with ref, target and edits only")
		}
		if n.Target != "new" {
			structural := prior
			structural.Title, structural.Lesson, structural.AppliesTo = "", "", ""
			structural.ChangeReason, structural.Kind, structural.Scene = "", "", ""
			structural.Sources, structural.Body, structural.Rationale = nil, nil, nil
			if len(structural.Related) == 0 {
				structural.Related = nil
			}
			if !reflect.DeepEqual(structural, notebook.Draft{}) {
				return item, nil, errors.New("the pending note contains structural metadata that exact edits cannot apply; use wiki ingest or target new to retain it")
			}
		}
		d = prior
	} else {
		if (n.Target == "" || n.Target == "new") && strings.TrimSpace(d.Lesson) == "" {
			return item, nil, errors.New("a new note needs a lesson; use reviewed for sources without lasting knowledge")
		}
		if d.ID != "" || len(d.Replaces) > 0 {
			return item, nil, errors.New("use a prepared target and exact edits; supersession remains available through wiki ingest")
		}
		sources, err := resolve(d.Sources)
		if err != nil {
			return item, nil, err
		}
		d.Sources = sources
		if d.Learning != nil {
			for i := range d.Learning.Connections {
				c := &d.Learning.Connections[i]
				if ref, ok := w.Articles[c.ID]; ok {
					c.ID, c.Revision = ref.ID, ref.Revision
				}
			}
			if q := d.Learning.Probe; q != nil {
				for i := range q.Results {
					ids, err := resolve(q.Results[i].Sources)
					if err != nil {
						return item, nil, err
					}
					q.Results[i].Sources = ids
				}
			}
			if q := d.Learning.Practice; q != nil {
				for i := range q.Steps {
					ids, err := resolve(q.Steps[i].Sources)
					if err != nil {
						return item, nil, err
					}
					q.Steps[i].Sources = ids
				}
			}
		}
		related := []string{}
		for _, ref := range d.Related {
			p, ok := w.Articles[ref]
			if !ok {
				return item, nil, fmt.Errorf("unknown related article %q", ref)
			}
			related = append(related, p.ID)
		}
		d.Related = related
		if d.Correction != nil {
			sources, err := resolve([]string{d.Correction.SourceID})
			if err != nil {
				return item, nil, err
			}
			c := *d.Correction
			c.SourceID = sources[0]
			d.Correction = &c
		}
	}
	signature := workHash(struct {
		Draft  notebook.Draft
		Target string
		Edits  []notebook.TextEdit
	}{d, n.Target, n.Edits})
	key := "note:" + signature
	if prior, ok := w.Saved[key]; ok {
		return prior, nil, nil
	}
	keys := []string{key}
	if origin := w.PendingOrigins[n.Ref]; origin != "" {
		keys = append(keys, origin)
	}
	if e := workSavedCapacity(w, keys...); e != nil {
		return item, nil, e
	}
	publicationIntent := requiredPublicationIntent(w, n, d)
	d.ChangeReason = strings.TrimSpace(d.ChangeReason + " [work " + w.Nonce[:8] + " " + signature[:12] + "]")
	// Recovery after a published article but a failed checkpoint uses the exact
	// expected revision, never a newly read page as permission to overwrite it.
	expected, hasExpected := w.Expected[key]
	if required := activeRequiredNote(w); required != nil && required.Expected != nil {
		publication := required.Expected
		live, err := book.LiveWhere(func(p notebook.Page) bool { return p.ID == publication.ID })
		if err != nil {
			return item, nil, err
		}
		if len(live) == 1 && live[0].Revision == publication.Revision && publication.Intent == publicationIntent {
			expected = store.KnowledgeReference{ID: publication.ID, Revision: publication.Revision}
			hasExpected = true
		} else {
			unwritten := false
			if len(live) == 0 && publication.Intent == publicationIntent {
				unwritten, err = requiredPublicationUnwritten(book, publication)
				if err != nil {
					return item, nil, err
				}
			}
			// A never-written preview can retry its same create. Published,
			// changed or missing history still requires the current original target.
			if !unwritten && w.Articles[n.Target].ID != publication.ID {
				return item, nil, fmt.Errorf("the original publication changed, is unavailable or differs from this repair; read article %s and repair that current target", publication.ID)
			}
			hasExpected = false
		}
	}
	if hasExpected {
		live, err := book.LiveWhere(func(p notebook.Page) bool { return p.ID == expected.ID })
		if err != nil {
			return item, nil, err
		}
		if len(live) == 1 && live[0].Revision == expected.Revision {
			if err = commit(func() error { return finishWorkSources(book, d.Sources) }); err != nil {
				return workItem{Kind: "note", ID: expected.ID, Revision: expected.Revision}, nil, err
			}
			item.ID, item.Revision = expected.ID, expected.Revision
			w.Saved[key] = item
			if origin := w.PendingOrigins[n.Ref]; origin != "" {
				w.Saved[origin] = item
			}
			return item, nil, save()
		}
	}
	var preview notebook.Result
	var err error
	if n.Target != "" && n.Target != "new" {
		target, ok := w.Articles[n.Target]
		if !ok {
			return item, nil, errors.New("unknown prepared article target")
		}
		patch := notebook.Patch{ID: target.ID, Revision: target.Revision, Sources: d.Sources, ChangeReason: d.ChangeReason, Edits: n.Edits, Passages: d.Passages, Learning: d.Learning}
		preview, err = book.PreviewPatch(patch)
		if err != nil {
			return item, nil, err
		}
		w.Expected[key] = store.KnowledgeReference{ID: preview.Page.ID, Revision: preview.Page.Revision}
		if origin := w.PendingOrigins[n.Ref]; origin != "" {
			w.Expected[origin] = w.Expected[key]
		}
		bindRequiredPublication(w, w.Expected[key], publicationIntent, key, w.PendingOrigins[n.Ref])
		if err = save(); err != nil {
			return item, nil, err
		}
		if err = workAfterWrite("note-preview"); err != nil {
			return item, nil, err
		}
		var value notebook.Result
		err = commit(func() error { var e error; value, e = book.Patch(patch); return e })
		if value.Page != nil {
			item.ID, item.Revision, item.Warnings = value.Page.ID, value.Page.Revision, value.Warnings
		}
	} else {
		pendingKey := key
		alreadyReviewed := n.Ref != "" && n.Target == "new"
		if !alreadyReviewed {
			placed, e := placeDraftsWithCommit(book.Root, book.Scope, []notebook.Draft{d}, commit)
			if e != nil {
				return item, nil, e
			}
			ambiguous := false
			for _, p := range placed.Placements[0].Candidates {
				ambiguous = ambiguous || !p.SameTopic.Calibrated || p.SameTopic.Band != "discard"
			}
			if ambiguous {
				ref := w.PendingKeys[pendingKey]
				if ref == "" {
					ref, e = nextWorkPendingReference(w)
					if e != nil {
						return item, nil, e
					}
					w.PendingKeys[pendingKey] = ref
					w.Pending[ref] = d
					w.PendingOrigins[ref] = key
				}
				if w.Reasoning != nil && w.ActiveNote != "" {
					w.Reasoning.RequiredNotes[w.ActiveNote].Pending = ref
					w.Reasoning.RequiredNotes[w.ActiveNote].PendingWork = w.Nonce
				}
				candidates := []any{}
				for i, c := range placed.Placements[0].Candidates {
					if i == 3 {
						break
					}
					alias := ""
					for known, p := range w.Articles {
						if p.ID == c.PageID && p.Revision == c.Revision {
							alias = known
							break
						}
					}
					if alias == "" {
						alias = "candidate" + strconv.Itoa(len(w.Articles)+1)
					}
					w.Articles[alias] = store.KnowledgeReference{ID: c.PageID, Revision: c.Revision}
					view, e := book.Query("lesson:" + c.PageID + "@" + c.Revision)
					if e != nil {
						return item, nil, e
					}
					candidates = append(candidates, map[string]any{"ref": alias, "article": view.Lessons, "same_topic": c.SameTopic, "conflict": c.Conflict})
				}
				if e = save(); e != nil {
					return item, nil, e
				}
				return item, map[string]any{"ref": ref, "title": d.Title, "candidates": candidates, "next_action": "Read the candidates. Record this note's ref with target new, or a candidate ref and exact edits; no metadata needs to be rebuilt."}, nil
			}
		}
		d.CreateOnly = true
		preview, err = book.PreviewCreate(d)
		if err != nil {
			return item, nil, err
		}
		w.Expected[key] = store.KnowledgeReference{ID: preview.Page.ID, Revision: preview.Page.Revision}
		if origin := w.PendingOrigins[n.Ref]; origin != "" {
			w.Expected[origin] = w.Expected[key]
		}
		bindRequiredPublication(w, w.Expected[key], publicationIntent, key, w.PendingOrigins[n.Ref])
		if err = save(); err != nil {
			return item, nil, err
		}
		if err = workAfterWrite("note-preview"); err != nil {
			return item, nil, err
		}
		var value notebook.Result
		err = commit(func() error {
			var e error
			value, e = ingestWithPreparedOutcome(book.Root, book.Scope, d, preview.Page)
			return e
		})
		if value.Page != nil {
			item.ID, item.Revision = value.Page.ID, value.Page.Revision
		}
	}
	if err != nil {
		return item, nil, err
	}
	if err = workAfterWrite("note"); err != nil {
		return item, nil, err
	}
	if err = commit(func() error { return finishWorkSources(book, d.Sources) }); err != nil {
		return item, nil, err
	}
	w.Saved[key] = item
	if origin := w.PendingOrigins[n.Ref]; origin != "" {
		w.Saved[origin] = item
	}
	return item, nil, save()
}
func finishWorkSources(book notebook.Book, sources []string) error {
	report, err := book.Lint()
	if err != nil {
		return err
	}
	if len(report.Issues) > 0 {
		return errors.New("the article was saved but wiki lint reports unresolved issues; inspect wiki lint before retrying")
	}
	_, err = book.Ingest(notebook.Draft{Sources: sources})
	return err
}

func uniqueWorkStrings(values []string) []string {
	result := []string{}
	seen := map[string]bool{}
	for _, v := range values {
		if !seen[v] {
			result = append(result, v)
			seen[v] = true
		}
	}
	return result
}
