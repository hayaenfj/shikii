package main

import (
	"maps"
	"slices"
	"sort"
	"strings"

	"github.com/hayaenfj/shikii/internal/notebook"
)

// This state belongs to one receipt. It describes delivery, not use, quality,
// permission, or independent experiments. The existing article limit bounds it.
type workLoopState struct {
	Bodies   map[string]string `json:"bodies"`
	Query    string            `json:"query"`
	Basis    string            `json:"basis"`
	Progress string            `json:"progress"`
	Repeats  int               `json:"repeats"`
}

type workLoopAdvice struct {
	Action          string   `json:"action"`
	SameResultReads int      `json:"same_result_reads"`
	ReusedRefs      []string `json:"reused_refs,omitempty"`
	ReusedBodyBytes int      `json:"reused_body_bytes"`
	RepeatedTrials  []string `json:"repeated_trials,omitempty"`
	Next            string   `json:"next"`
}

func repeatedWorkTrials(book notebook.Book, r *reasoningProgress) []string {
	if r == nil {
		return nil
	}
	groups := map[string][]string{}
	unreviewed := map[string]bool{}
	for _, trial := range r.Trials {
		if trial.Result == nil || trial.ResultSource == "" || trial.Result.Assessment == "supports" {
			continue
		}
		p := trial.Plan
		// Evidence aliases and different effort estimates do not make the
		// reported method/result new. Keep the actual references for review.
		key := workHash([]any{p.Case, p.Question, p.Expected, p.Method, p.Conditions,
			trial.Result.Assessment, trial.Result.Observed, trial.Result.Measurements})
		groups[key] = append(groups[key], trial.Ref)
		unreviewed[key] = unreviewed[key] || !trial.Reviewed
	}
	refs := []string{}
	for key, group := range groups {
		current := true
		for _, ref := range group {
			current = current && trialBasisCurrent(book, r.Trials[ref].Plan.Evidence)
		}
		if len(group) > 1 && unreviewed[key] && current {
			refs = append(refs, group...)
		}
	}
	sort.Strings(refs)
	return refs
}

func prepareWorkLoop(book notebook.Book, w *workState, req workRequest, entries []any, quoted map[string]string) *workLoopAdvice {
	if w.Loop == nil {
		w.Loop = &workLoopState{Bodies: map[string]string{}}
	}
	l := w.Loop
	if l.Bodies == nil {
		l.Bodies = map[string]string{}
	}
	a := &workLoopAdvice{Action: "continue", Next: "Use new evidence to choose the next action. Required reviews and explicit source recovery still apply."}
	a.RepeatedTrials = repeatedWorkTrials(book, w.Reasoning)
	// Explicit recovery and consult/reuse preserve their complete requested
	// payloads. Cursor pages are distinct reads, never stalled retries.
	broad := req.Proposal == "" && req.Conditions == "" && req.Cursor == "" &&
		!strings.HasPrefix(req.Query, "lesson:") && !sourceIDText(req.Query)
	identities := []string{}
	for _, raw := range entries {
		e := raw.(map[string]any)
		id, _ := e["id"].(string)
		revision, _ := e["revision"].(string)
		key := id + "@" + revision
		// Article revisions are immutable. Derived learning and premise
		// warnings can change without a body edit, so compare them too.
		signature := workHash([]any{key, e["learning_context"], e["recheck"]})
		identities = append(identities, signature)
		full := e["body_included"] == true
		canReuse := full && req.Receipt != "" && broad && req.Reasoning == nil &&
			l.Bodies[key] == signature && !loopPremiseChanged(e)
		if canReuse {
			before := workContextSize(e)
			reused := maps.Clone(e)
			delete(reused, "article")
			delete(reused, "text")
			delete(reused, "body_excerpts")
			reused["body_included"], reused["reused"] = false, true
			reused["reading_notice"] = "Reuse the full body supplied earlier under this receipt. Its exact revision query remains available. Delivery does not establish use or quality."
			ref, _ := e["ref"].(string)
			delta := before - workContextSize(reused)
			// Leave enough room for the complete advice envelope as well as
			// the per-row notice. Small complete bodies are often cheaper.
			if delta > 512 {
				clear(e)
				maps.Copy(e, reused)
				delete(quoted, ref)
				a.ReusedRefs = append(a.ReusedRefs, ref)
				a.ReusedBodyBytes += delta
			}
		}
		if full && (len(l.Bodies) < 64 || l.Bodies[key] != "") {
			l.Bodies[key] = signature
		}
	}
	if broad {
		slices.Sort(identities)
		query := workHash([]any{readingTopic(req.Query), req.Reasoning})
		basis := workHash(identities)
		generation := uint64(0)
		if w.Reasoning != nil {
			generation = w.Reasoning.Generation
		}
		progress := workHash([]any{generation, len(w.Sources), len(w.Saved), len(w.Pending), len(w.Expected)})
		if query == l.Query && basis == l.Basis && progress == l.Progress {
			l.Repeats = min(128, l.Repeats+1)
		} else {
			l.Repeats = 0
		}
		l.Query, l.Basis, l.Progress = query, basis, progress
		a.SameResultReads = l.Repeats
	} else {
		// Recovery can supply deciding information outside a broad search.
		// Do not count it as an unchanged retry of that earlier search.
		l.Query, l.Basis, l.Progress, l.Repeats = "", "", "", 0
	}
	if len(a.ReusedRefs) > 0 {
		a.Action = "reuse_previous"
		a.Next = "Reuse the earlier full-body references under this receipt instead of repeating the same search. Inspect current conditions; explicit recovery remains available."
		if w.Reasoning != nil && w.Reasoning.Reading != nil {
			// A changed delivery procedure must not teach a reading-depth
			// preference by silently changing its comparison conditions.
			w.Reasoning.Reading.Mixed = true
		}
	}
	if a.SameResultReads >= 2 || len(a.RepeatedTrials) > 0 {
		a.Action = "change_approach"
		a.Next = "The reported search or trial result repeated without new deciding information. Reuse the cited evidence and change the query, method or distinguishing check before another retry. Record the changed method and actual outcome through existing reasoning/trial/comparison; this is advice, never an execution block or a success label."
	}
	if a.Action == "continue" && a.SameResultReads == 0 {
		return nil
	}
	return a
}

func sourceIDText(s string) bool {
	return len(s) == 16 && strings.Trim(s, "0123456789abcdef") == ""
}

func loopPremiseChanged(e map[string]any) bool {
	if warnings, ok := e["recheck"].([]notebook.PremiseChange); ok && len(warnings) > 0 {
		return true
	}
	if learning, ok := e["learning_context"].(*notebook.LearningContext); ok && learning != nil && len(learning.Recheck) > 0 {
		return true
	}
	return false
}
