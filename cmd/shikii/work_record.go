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

func recordWork(root, scope string, req workRequest) (any, error) {
	token, err := workDecode(req.Receipt)
	if err != nil {
		return nil, err
	}
	if req.SessionID != "" || req.TurnID != "" || req.Query != "" || req.Request != "" || req.Proposal != "" || req.Conditions != "" || req.Cursor != "" {
		return nil, errors.New("record uses its receipt; do not replace prepared task identifiers")
	}
	if len(req.Observations) > 4 || len(req.Notes) > 2 || len(req.Reviews) > 128 || len(req.Memory) > 256 || len(req.Reviewed) > 64 || len(req.Maintenance) > 8 || len(req.Discoveries) > 4 || len(req.Outcomes) > 2 {
		return nil, errors.New("the recording batch exceeds its fixed item limits")
	}
	if req.Agreed == nil && req.Open == nil && req.ContinuesPrevious == nil && len(req.Observations)+len(req.Notes)+len(req.Reviews)+len(req.Memory)+len(req.Reviewed)+len(req.Maintenance)+len(req.Discoveries)+len(req.Outcomes) == 0 {
		return nil, errors.New("record needs content that was judged or observed")
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
			if id == "" && ref == "current" && !w.Standalone {
				alias, source, err := captureWorkObservation(book, w, workObservation{Kind: "human", Title: "Current request quoted during work", Content: w.Current, Reference: "current request recorded by the hook"}, save, commit)
				if err != nil {
					return nil, err
				}
				w.Sources[alias] = source
				id = source
			}
			if id == "" {
				return nil, fmt.Errorf("unknown source reference %q; use a prepared source or new observation", ref)
			}
			sources = append(sources, id)
		}
		return sources, nil
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
		sources := append([]string{}, w.Pending[n.Ref].Sources...)
		resolveNote := func(refs []string) ([]string, error) {
			ids, e := resolveSources(refs)
			if e == nil {
				sources = append(sources, ids...)
			}
			return ids, e
		}
		item, pending, e := recordWorkNote(book, w, n, resolveNote, save, commit)
		if e == nil && pending == nil && item.ID != "" {
			noteSources = append(noteSources, sources...)
		}
		if item.Ref == "" {
			item.Ref = "note" + strconv.Itoa(i+1)
		}
		if e != nil {
			item.Error = e.Error()
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
	if len(req.Reviewed) > 0 {
		sources, e := resolveSources(req.Reviewed)
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
		memo = workCapture{Ref: "observed" + strconv.Itoa(len(w.Captures)+1), At: value.ObservedAt, Version: value.Version}
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
		allowed.ChangeReason = ""
		if !reflect.DeepEqual(allowed, notebook.Draft{}) {
			return item, nil, errors.New("an existing article accepts sources, change_reason and exact edits only; use wiki operations for structural changes")
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
	d.ChangeReason = strings.TrimSpace(d.ChangeReason + " [work " + w.Nonce[:8] + " " + signature[:12] + "]")
	// Recovery after a published article but a failed checkpoint uses the exact
	// expected revision, never a newly read page as permission to overwrite it.
	if expected, ok := w.Expected[key]; ok {
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
		patch := notebook.Patch{ID: target.ID, Revision: target.Revision, Sources: d.Sources, ChangeReason: d.ChangeReason, Edits: n.Edits}
		preview, err = book.PreviewPatch(patch)
		if err != nil {
			return item, nil, err
		}
		w.Expected[key] = store.KnowledgeReference{ID: preview.Page.ID, Revision: preview.Page.Revision}
		if origin := w.PendingOrigins[n.Ref]; origin != "" {
			w.Expected[origin] = w.Expected[key]
		}
		if err = save(); err != nil {
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
					ref = "note" + strconv.Itoa(len(w.Pending)+1)
					w.PendingKeys[pendingKey] = ref
					w.Pending[ref] = d
					w.PendingOrigins[ref] = key
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
		if err = save(); err != nil {
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
