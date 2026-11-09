package main

import (
	"github.com/hayaenfj/shikii/internal/decide"
	"github.com/hayaenfj/shikii/internal/store"
)

// settlePending attaches outcome truths to the kept prompt-kind decisions: if the tree at request time differs from the current tree (now), edit (weight 1); if the same, !edit (partial, weight 0.7).
// Called right after a request (when the next request arrives) and at Stop. If now is "" (outside git, or git too slow), they are dropped without a label (nothing to compare).
// only is the pending the caller saw outside the lock: only those still present inside the lock are labeled (pending added later by an overlapping hook is settled on that hook's turn).
// final means "the person's next request arrived" (the request boundary): pending is always cleared here. At Stop (final=false), if changed, edit is attached and it is cleared;
// if unchanged, !edit (partial) is attached once and it is carried over (if it changed by the Stop after a background notification, it is replaced with edit).
func settlePending(agent string, sess *hookSession, now string, only []pendingItem, final bool) error {
	if len(sess.Pending) == 0 || len(only) == 0 {
		return nil
	}
	mine := map[string]bool{}
	for _, p := range only {
		mine[p.Trace] = true
	}
	st := store.Store{Dir: agent}
	var firstErr error
	var rest []pendingItem
	for _, p := range sess.Pending {
		if !mine[p.Trace] {
			rest = append(rest, p)
			continue
		}
		if now == "" {
			continue // nothing to compare: drop without a label
		}
		if p.Tree != now {
			if err := labelTrace(st, "prompt-kind", p.Trace, "edit", 1, "outcome"); err != nil && firstErr == nil {
				firstErr = err
			}
			continue
		}
		if !p.Settled {
			if err := labelTrace(st, "prompt-kind", p.Trace, "!edit", 0.7, "outcome"); err != nil && firstErr == nil {
				firstErr = err
			}
			p.Settled = true
		}
		if !final {
			rest = append(rest, p)
		}
	}
	sess.Pending = rest
	return firstErr
}

// noteAsked is the change that adds a "decision waiting for the person's answer" to the record (applied inside the lock).
func noteAsked(specID string, d *decide.Decision) func(*hookSession) {
	return func(s *hookSession) {
		if waitsForPersonFor(specID, d) {
			s.Asked = append(s.Asked, askedItem{Spec: specID, Trace: d.TraceID})
		}
	}
}

// unanswered is the traces in asked that have no answer yet.
func unanswered(st store.Store, asked []askedItem) []string {
	labeled := map[string]map[string]store.Label{}
	reviews := map[string]map[string]store.Review{}
	records := map[string]map[string]store.Record{}
	var out []string
	for _, a := range asked {
		if _, ok := labeled[a.Spec]; !ok {
			m, err := st.LabeledTraces(a.Spec)
			if err != nil {
				m = map[string]store.Label{}
			}
			labeled[a.Spec] = m
			reviews[a.Spec], _ = st.Reviews(a.Spec)
			records[a.Spec] = map[string]store.Record{}
			all, _ := st.Records(a.Spec, 0)
			for _, rec := range all {
				records[a.Spec][rec.TraceID] = rec
			}
		}
		rec, ok := records[a.Spec][a.Trace]
		if !ok {
			continue
		}
		if show, _, _ := reviewQueueState(a.Spec, rec, labeled[a.Spec], reviews[a.Spec]); show {
			out = append(out, a.Trace)
		}
	}
	return out
}

// hasNew reports whether waiting has a trace not in reported.
func hasNew(waiting, reported []string) bool {
	seen := map[string]bool{}
	for _, r := range reported {
		seen[r] = true
	}
	for _, w := range waiting {
		if !seen[w] {
			return true
		}
	}
	return false
}

func labelTrace(st store.Store, specID, trace, truth string, weight float64, source string) error {
	rec, err := st.FindRecord(specID, trace)
	if err != nil {
		return err
	}
	labels, err := store.LabelsFor(rec, truth, weight, source)
	if err != nil {
		return err
	}
	if source == "outcome" {
		return st.AppendLabelsOnce(labels) // decided by hooks that can race or retry: each outcome truth lands once (0.9.3)
	}
	return st.AppendLabels(labels)
}
