package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/hayaenfj/shikii/internal/decide"
	"github.com/hayaenfj/shikii/internal/spec"
	"github.com/hayaenfj/shikii/internal/store"
)

// handleAnswered lists decisions a person answered (newest first). It is the material for correcting an answer (another /v1/label on the same trace: the new answer wins) and for asking the AI again (/v1/redo).
func (s *server) handleAnswered(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("spec")
	sp, view, limit, ok := s.decisionList(w, r, id)
	if !ok {
		return
	}
	recs, labeled := view.Records, view.Labeled
	superseded := supersededRecords(recs)
	// an answer the agent's reflection rejected (quoted twice, never useful) leaves this list; the knowledge section lists it, and its
	// truth stays in the labels
	rejected := func(string) bool { return false }
	if containsString(answerSpecs, id) {
		answers, err := s.st.PersonAnswers(answerSpecs)
		if err != nil {
			writeErr(w, 500, err)
			return
		}
		rejected = readAnswerRejection(s.o.storeDir, s.o.projectScope).withAnswers(answers).rejected // counted with the decisions asked again
	}
	var answered []store.Record
	for _, rec := range recs {
		l, ok := labeled[rec.TraceID]
		if ok && store.HumanSource(l.Source) && !superseded[rec.TraceID] && !rejected(rec.TraceID) {
			answered = append(answered, rec)
		}
	}
	start, end, next, ok := decisionPage(w, r, len(answered), limit, []any{s.o.storeDir, id, sp.Hash(), sp.ContextHash(), answered, labeled})
	if !ok {
		return
	}
	items := []map[string]any{}
	for _, rec := range answered[start:end] {
		l := labeled[rec.TraceID]
		items = append(items, map[string]any{
			"trace_id": rec.TraceID, "at": rec.At, "band": rec.Band, "top": rec.Top, "p": rec.P, "model": rec.Model,
			"inputs": rec.Inputs, "inputs_hash": rec.InputsHash, "stale": !sp.LiveHash(rec.SpecHash), "rung": rec.RungOf(), "redo_of": rec.RedoOf,
			"answerable": len(rec.Inputs) > 0, "choices": truthChoices(sp), "project": rec.Project,
			"truth": l.Truth, "truth_at": l.At, "source": l.Source, "correct": store.Correct(rec.Top, l.Truth),
		})
		if legacyPlanningRecord(sp, rec) {
			items[len(items)-1]["legacy_planning"] = true
			if rec.Display != nil {
				items[len(items)-1]["record_question"] = rec.Display.Question
			}
			items[len(items)-1]["choices"] = []map[string]any{{"truth": "yes", "label": "yes", "labels": map[string]string{"ja": "はい"}}, {"truth": "no", "label": "no", "labels": map[string]string{"ja": "いいえ"}}}
		}
		items[len(items)-1]["original_inputs"], items[len(items)-1]["text_status"] = s.st.RecordText(rec)
	}
	writeJSON(w, 200, map[string]any{"spec": id, "items": items, "next_cursor": next})
}

// handleRedo decides a past decision's text again with the current wording (spec) and line. It keeps a new record (redo_of = the original trace), and the original record leaves the queue.
// If a person already answered, that answer is the truth for the same text, so it is copied to the new record too (no need to answer again). Returns before and now side by side.
func (s *server) handleRedo(w http.ResponseWriter, r *http.Request) {
	if !acceptJSONPost(w, r) {
		return
	}
	var req struct {
		Spec    string `json:"spec"`
		TraceID string `json:"trace_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, 400, fmt.Errorf("bad json: %w", err))
		return
	}
	sp, err := s.spec(req.Spec)
	if errors.Is(err, errNotFound) {
		writeUnknownSpec(w, req.Spec)
		return
	} else if err != nil {
		writeErr(w, 500, err)
		return
	}
	rec, err := s.st.FindRecord(req.Spec, req.TraceID)
	if err != nil {
		writeErr(w, 404, err)
		return
	}
	if len(rec.Inputs) == 0 {
		writeErr(w, 409, errors.New("this decision kept no text, so it cannot be asked again"))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	d, err := decide.Decide(ctx, sp, rec.Inputs, decide.Session{Tainted: rec.Tainted || s.o.ef.tainted}, s.o.ef.endpoints())
	if err != nil {
		writeErr(w, 502, err)
		return
	}
	attachReceipt(sp, s.st, d)
	resp := map[string]any{"before": map[string]any{"trace_id": rec.TraceID, "band": rec.Band, "top": rec.Top, "p": rec.P, "model": rec.Model, "stale": !sp.LiveHash(rec.SpecHash)}, "after": d, "recorded": false}
	if d.Band == decide.Abstain {
		writeJSON(w, 200, resp)
		return
	}
	newRec := recordOf(sp, d, rec.Inputs)
	newRec.RedoOf = rec.TraceID
	if err := recordDecisionRec(s.st, sp, d, newRec); err != nil { // same manners as /v1/decide (if act on a context rung, the machine truth for the minimal rung too)
		writeErr(w, 500, err)
		return
	}
	resp["recorded"] = true
	labeled, err := s.st.LabeledTraces(req.Spec)
	if err != nil {
		writeErr(w, 500, err)
		return
	}
	if l, ok := labeled[rec.TraceID]; ok && store.HumanSource(l.Source) {
		copied, err := store.LabelsFor(&newRec, l.Truth, l.Weight, l.Source)
		if err == nil {
			err = s.st.AppendLabels(copied)
		}
		if err != nil {
			writeErr(w, 500, fmt.Errorf("carry the person's answer to the new decision: %w", err))
			return
		}
		resp["truth"] = l.Truth
		resp["correct_before"] = store.Correct(rec.Top, l.Truth)
		resp["correct_after"] = store.Correct(d.Top, l.Truth)
	}
	writeJSON(w, 200, resp)
}

func (s *server) handleLabel(w http.ResponseWriter, r *http.Request) {
	if !acceptJSONPost(w, r) {
		return
	}
	var req struct {
		TraceID string  `json:"trace_id"`
		Spec    string  `json:"spec"`
		Truth   string  `json:"truth"`
		Weight  float64 `json:"weight"`
		Source  string  `json:"source"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, 400, fmt.Errorf("bad json: %w", err))
		return
	}
	if req.TraceID == "" || req.Truth == "" {
		writeErr(w, 400, errors.New("trace_id and truth are required"))
		return
	}
	rec, err := s.st.FindRecord(req.Spec, req.TraceID)
	if err != nil {
		writeErr(w, 404, err)
		return
	}
	if req.Source == "" {
		req.Source = "ui"
	}
	labels, err := store.LabelsFor(rec, req.Truth, req.Weight, req.Source)
	if err != nil {
		writeErr(w, 400, err)
		return
	}
	if err := s.st.AppendLabels(labels); err != nil { // write both rungs in one go (so only one never remains)
		writeErr(w, 500, err)
		return
	}
	l := labels[0]
	// An answer to a decision settled after adding context also goes to the minimal question (rungs = 2). Lines are drawn per rung
	resp := map[string]any{"trace_id": l.TraceID, "spec": l.SpecID, "model": l.Model, "truth": l.Truth, "weight": l.Weight, "source": l.Source, "rungs": len(labels), "calibrated": false}
	if s.o.autoCalibrate {
		if report, did := s.maybeCalibrate(rec.SpecID, rec.SpecHash, rec.Model); did {
			resp["calibrated"] = true
			resp["calibration"] = report
		}
	}
	writeJSON(w, 200, resp)
}

// maybeCalibrate calibrates and writes back only when the (hash, model) truths reach min_n and there is no line yet (or it is old).
// Even if it fails, the label handling stays successful (it is only noted in the report).
func (s *server) maybeCalibrate(specID, hash, model string) (string, bool) {
	sp, err := s.spec(specID)
	if err != nil || !sp.LiveHash(hash) || model == "" {
		return "", false
	}
	rs, rung := sp, "minimal" // this record's rung. A line is drawn only for this rung (other rungs' lines are not touched)
	if hash == sp.ContextHash() {
		rs, rung = sp.ContextSpec(), "context"
	}
	if c := rs.CalibrationFor(model); c != nil && c.SpecHash == hash {
		return "", false // a line already exists. Redraw it when drift says it got worse (not touched here)
	}
	labels, err := s.st.Labels(specID, hash, model)
	if err != nil || len(labels) < sp.Eval.MinN {
		return "", false
	}
	defer s.lockCalibration()()
	var buf bytes.Buffer
	ef := s.o.ef
	ef.model = model
	err = func() (err error) {
		defer func() {
			if r := recover(); r != nil {
				err = fmt.Errorf("calibrate panicked: %v", r)
			}
		}()
		return runCalibrate(sp.Path(), calibrateOpts{output: &buf, ef: ef, storeDir: s.o.storeDir, labeled: "none", bins: 10, write: true, rung: rung})
	}()
	report := strings.TrimSpace(buf.String())
	result := calibrationResult{Reason: "written", MinN: rs.Eval.MinN, Hash: hash, Model: model, At: time.Now().Format("2006-01-02 15:04:05"), TriesWordings: containsString(hookSpecs, specID)}
	if err != nil {
		report += "\nERROR: " + err.Error()
		result.Reason = "error"
		var refused *calibrationRefusal
		if errors.As(err, &refused) {
			result.Reason, result.HumanTruths, result.MinN = refused.reason(), refused.humanTruths, refused.minN
			// every truth on one side is told as one kind of answer (as the per-model marker one_answer), not as "this wording cannot tell them apart"
			if result.Reason == "quality" && lineStateOf(rs, rs.Eval.MinN, model, hash, labels, false).Kind == lineOneAnswer {
				result.Reason = "one_answer"
			}
		}
	} else if after, loadErr := s.spec(specID); loadErr == nil {
		if rung == "context" {
			after = after.ContextSpec()
		}
		if after != nil {
			if c := after.CalibrationFor(model); c != nil {
				result.HumanTruths = c.NHuman
			}
		}
	}
	s.mu.Lock()
	s.lastCal[specID] = calibrationReport{log: result.At + "\n" + report, result: result}
	s.mu.Unlock()
	log.Printf("auto-calibrate %s (%s): %s", specID, model, lastLine(report))
	return report, err == nil
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return lines[len(lines)-1]
}

// handleQueue lists records without a truth. ask band first, newest first within it.
func (s *server) handleQueue(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("spec")
	sp, view, limit, ok := s.decisionList(w, r, id)
	if !ok {
		return
	}
	recs, labeled := view.Records, view.Labeled
	rank := map[string]int{"ask": 0, "audit": 1, "act": 2, "discard": 3} // unsure → spot checks (1 in 20 of act) → the rest
	reviews, err := s.st.Reviews(id)
	if err != nil {
		writeErr(w, 500, err)
		return
	}
	superseded := supersededRecords(recs)
	type queued struct {
		record store.Record
		queueItem
	}
	var pending []queued
	settled := map[string]bool{}
	if sp.ID == "wiki-conflict" {
		settled = settledDrafts(s.st)
	}
	checkOutcome := outcomeChecksOf(sp, view.Labels)
	for _, rec := range recs {
		if legacyPlanningRecord(sp, rec) || undecidedConflict(sp.ID, rec, labeled, settled) {
			continue
		}
		if q := queueItemOf(id, rec, labeled, reviews, checkOutcome); q.show && !superseded[rec.TraceID] {
			pending = append(pending, queued{rec, q})
		}
	}
	order := func(it queued) int {
		if it.audit {
			return rank["audit"]
		}
		return rank[it.record.Band]
	}
	sort.SliceStable(pending, func(i, j int) bool { return order(pending[i]) < order(pending[j]) })
	// Hash ordered eligibility and display state before reading originals.
	ordered := make([]any, 0, len(pending))
	for _, item := range pending {
		ordered = append(ordered, []any{item.record, item.show, item.audit, item.waits, item.reason, item.pair})
	}
	start, end, next, ok := decisionPage(w, r, len(pending), limit, []any{s.o.storeDir, id, sp.Hash(), sp.ContextHash(), ordered, reviews})
	if !ok {
		return
	}
	pending = pending[start:end]
	// Reading original text and building display data are limited to the records actually returned.
	items := make([]map[string]any, 0, len(pending))
	for _, item := range pending {
		rec := item.record
		// A person cannot answer an (old) record without text. Give no button material and say answerable=false (truths from a program that knows the outcome come by another path)
		answerable := len(rec.Inputs) > 0
		choices := []map[string]any{}
		if answerable {
			choices = truthChoices(sp)
		}
		items = append(items, map[string]any{
			"trace_id": rec.TraceID, "at": rec.At, "band": rec.Band, "top": rec.Top, "p": rec.P, "model": rec.Model,
			"inputs": rec.Inputs, "inputs_hash": rec.InputsHash, "raw": rec.Raw, "stale": !sp.LiveHash(rec.SpecHash),
			"answerable": answerable, "choices": choices, "rung": rec.RungOf(), "project": rec.Project,
			"audit": item.audit, "review_reason": item.reason, "waits_for_person": item.waits,
		})
		if item.pair != nil && (item.reason == "disagree" || item.reason == "uncertain") { // both answers, for the person to choose between (0.11)
			items[len(items)-1]["pair"] = item.pair
		}
		if grade, ok := reviews[rec.TraceID]; ok {
			items[len(items)-1]["review"] = grade
		}
		items[len(items)-1]["original_inputs"], items[len(items)-1]["text_status"] = s.st.RecordText(rec)
	}
	writeJSON(w, 200, map[string]any{"spec": id, "items": items, "next_cursor": next})
}

func (s *server) decisionList(w http.ResponseWriter, r *http.Request, id string) (*spec.Spec, store.DecisionLog, int, bool) {
	limit := 50
	if v := r.URL.Query().Get("limit"); v != "" {
		fmt.Sscanf(v, "%d", &limit)
	}
	if limit < 1 || limit > 1000 {
		limit = 50
	}
	sp, err := s.spec(id)
	if errors.Is(err, errNotFound) {
		writeUnknownSpec(w, id)
		return nil, store.DecisionLog{}, 0, false
	}
	if err != nil {
		writeErr(w, 500, err)
		return nil, store.DecisionLog{}, 0, false
	}
	view, err := s.st.ReadDecisionLog(r.Context(), id)
	if err != nil {
		writeErr(w, 500, err)
		return nil, store.DecisionLog{}, 0, false
	}
	view.NewestFirst()
	return sp, view, limit, true
}

func supersededRecords(records []store.Record) map[string]bool {
	out := map[string]bool{}
	for _, rec := range records {
		if rec.RedoOf != "" {
			out[rec.RedoOf] = true
		}
	}
	return out
}
