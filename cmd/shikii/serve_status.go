package main

import (
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/hayaenfj/shikii/internal/decide"
	"github.com/hayaenfj/shikii/internal/store"
	"github.com/hayaenfj/shikii/internal/systemone"
)

func (s *server) handleStatus(w http.ResponseWriter, r *http.Request) {
	specs, err := loadSpecs(s.o.specsDir)
	if err != nil {
		writeErr(w, 500, err)
		return
	}
	var page map[string]any // where this page is (0.8.0): so an agent can answer the URL and port over MCP too
	if common, err := agentHome(); err == nil {
		if abs, err := filepath.Abs(s.o.storeDir); err == nil {
			if rec, ok := readPageRecord(common, filepath.Dir(abs)); ok && rec.PID == os.Getpid() { // only our own mark (do not answer with the mark of another serve started by hand)
				page = map[string]any{"url": rec.url() + "/", "port": rec.Port, "pid": rec.PID}
			}
		}
	}
	out := []map[string]any{}
	var person personWork // the summary at the top of the page (0.7.4): the person's effort summed over all specs
	for _, sp := range specs {
		titles := map[string]string{}
		questions := map[string]string{} // the page's "what it asks" (in the person's words)
		for lang, d := range sp.Display {
			if d.Title != "" {
				titles[lang] = d.Title
			}
			if d.Question != "" {
				questions[lang] = d.Question
			}
		}
		view, err := s.st.ReadDecisionLog(r.Context(), sp.ID)
		if err != nil {
			writeErr(w, 500, err)
			return
		}
		nRec, labs := view.RawRecords, view.Labels
		counts := newStatusLineCounts(labs)
		var checkOutcome *outcomeChecks
		if outcomeCheckSpecs[sp.ID] {
			checkOutcome = &outcomeChecks{report: counts.agreement, act: sp.Thresholds.Act}
		}
		byKey := map[string]bool{}
		for _, l := range labs {
			byKey[l.Model+"\x00"+l.SpecHash] = true
		}
		keys := []map[string]any{} // [] even if empty (null makes the page crash)
		for k := range byKey {
			parts := strings.SplitN(k, "\x00", 2)
			model, hash := parts[0], parts[1]
			state := "collecting"
			method, rung := "", "minimal"
			rs := sp // this hash's rung
			if hash == sp.ContextHash() {
				rs, rung = sp.ContextSpec(), "context"
			}
			// n is the count calibration uses (0.9.3): the latest truth per input, from people and results only (machine truths and re-labels do not raise it)
			n := counts.state(rs, sp.Eval.MinN, model, hash, false).N
			switch {
			case !sp.LiveHash(hash):
				state = "stale"
			case rs.CalibrationFor(model) != nil && rs.CalibrationFor(model).SpecHash == hash:
				state, method = "calibrated", rs.CalibrationFor(model).Method
			default:
				// Do not say "complete" from counts alone (0.8.3): separate having answers on only one side, tried and could not separate, and partway through automatic drawing
				tried := (hash == sp.Hash() || hash == sp.ContextHash()) && triedWithoutLine(s.st.Dir, sp, model)
				if _, ok := readStopped(s.st.Dir, sp); ok && hash == sp.Hash() { // the wording's stop, on every model's key
					state = "stopped" // the stop rule asks it on 1 request in 20 only (0.10)
					break
				}
				switch counts.state(rs, sp.Eval.MinN, model, hash, tried).Kind {
				case lineReady:
					state = "ready"
				case lineOneAnswer:
					state = "one_answer"
				case lineNotSeparable:
					state = "not_separable"
				}
			}
			keys = append(keys, map[string]any{"model": model, "hash": hash, "n": n, "state": state, "method": method, "rung": rung})
		}
		sort.Slice(keys, func(i, j int) bool {
			if keys[i]["model"] != keys[j]["model"] {
				return keys[i]["model"].(string) < keys[j]["model"].(string)
			}
			return keys[i]["rung"].(string) > keys[j]["rung"].(string) // minimal first
		})
		labeled := view.Labeled
		reviews, rerr := s.st.Reviews(sp.ID)
		if rerr != nil {
			writeErr(w, 500, rerr)
			return
		}
		records := view.Records
		todo, reviewPending, legacyPlanning, waitingPerson, undecided := 0, 0, 0, 0, 0
		settled := map[string]bool{}
		if sp.ID == "wiki-conflict" {
			settled = settledDrafts(s.st)
		}
		for _, rec := range records {
			if legacyPlanningRecord(sp, rec) {
				legacyPlanning++
				continue
			}
			if undecidedConflict(sp.ID, rec, labeled, settled) { // the draft is saved, but it did not touch this page: settled without a truth (0.8.0)
				undecided++
				continue
			}
			if q := queueItemOf(sp.ID, rec, labeled, reviews, checkOutcome); q.show { // the same rule as the queue
				todo++
				if q.waits {
					waitingPerson++
				}
			}
			if _, graded := reviews[rec.TraceID]; rec.ReviewPending && !graded {
				if _, answered := labeled[rec.TraceID]; !answered {
					reviewPending++
				}
			}
		}
		// answered count = the number of texts (inputs_hash) a person answered. Answers copied by a redo and corrections to an answer on the same record are not counted
		answered := map[string]bool{}
		for _, l := range labeled {
			if store.HumanSource(l.Source) {
				answered[l.InputsHash] = true
			}
		}
		funnel := view.Funnel(decide.DiscardUnsure(sp))
		person.add(countPersonWork(sp.ID, decide.DiscardUnsure(sp), records, labs, reviews, time.Now()))
		s.mu.Lock()
		last := s.lastCal[sp.ID]
		s.mu.Unlock()
		var result *calibrationResult
		if last.log != "" {
			result = &last.result
		}
		truthSource, personAnswers := "person", !machineOnlySpecs[sp.ID] // decisions people do not answer (wiki contradictions etc.): in 0.8.0, 60 items looked like they were waiting for confirmation
		if outcomeSpecs[sp.ID] {
			truthSource = "results"
		}
		awaiting, settledByAI := todo-waitingPerson, 0 // waiting for an outcome (the next request or the review's result gives the truth)
		if !outcomeSpecs[sp.ID] && personAnswers {     // act on a question a person gives truths to (outside spot checks) gets no outcome, so it counts as "the AI decided (no check needed)" (audit R5)
			settledByAI, awaiting = awaiting, 0
		}
		out = append(out, map[string]any{"id": sp.ID, "type": string(sp.Type), "hash": sp.Hash(), "titles": titles, "questions": questions, "question": strings.TrimSpace(sp.Question), "records": nRec, "labels": len(answered), "label_rows": len(labs),
			"unlabeled": todo, "waiting_for_person": waitingPerson, "awaiting_results": awaiting, "settled_by_ai": settledByAI, "undecided": undecided, "truth_source": truthSource, "person_answers": personAnswers, "review_pending": reviewPending, "review_quality": store.ReviewQuality(reviews, labeled), "min_n": sp.Eval.MinN, "keys": keys, "last_calibration": last.log, "calibration_result": result, "planning_context": needsPlanningContext(sp), "legacy_planning_records": legacyPlanning,
			"funnel": map[string]any{"minimal": funnel.Minimal, "context": funnel.Context, "human": funnel.Human, "human_per_thousand": funnel.HumanPerThousand()}})
	}
	sort.Slice(out, func(i, j int) bool { return out[i]["id"].(string) < out[j]["id"].(string) })
	modelName, endpoint := "local", s.o.ef.local // the AI's state (0.8.0): which model is deciding
	if usesHosted() {
		modelName, endpoint = "TypeSafe Jev", systemone.RedactURL(s.o.ef.hosted)
	}
	writeJSON(w, 200, map[string]any{"specs": out, "project_name": filepath.Base(s.o.projectScope), "auto_calibrate": s.o.autoCalibrate, "store": s.o.storeDir, "model": map[string]any{"name": modelName, "endpoint": endpoint},
		"summary": statusSummary(s.st, s.o.storeDir, s.o.projectScope, person, time.Now()), "page": page})
}
