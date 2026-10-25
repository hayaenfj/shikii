package main

import (
	"fmt"
	"io"
	"path/filepath"
	"time"

	"github.com/hayaenfj/shikii/internal/store"
)

// personWork counts "the person's effort": decisions waiting for a person's answer under the current rules, and who decided the truth (a person or the outcome).
// The per-rung counts (rungs' "left to a person") are the unsure ones, and unsure decisions whose truth comes from the outcome do not wait for a person. This is the number a person actually answers
// (unsure ones, samples, and the samples and held items of the AI's provisional scores).
type personWork struct {
	Records, Waits             int // all
	RecentRecords, RecentWaits int // last 7 days
	ByPerson, ByResult         int // number of decisions (traces) with a truth. Machine truths (context / judge) are not counted
}

const personWorkWindow = 7 * 24 * time.Hour

func countPersonWork(specID string, discardUnsure bool, recs []store.Record, labs []store.Label, reviews map[string]store.Review, now time.Time) personWork {
	var w personWork
	person, result := map[string]bool{}, map[string]bool{}
	for _, l := range labs {
		switch {
		case l.Source == "outcome":
			result[l.TraceID] = true
		case store.HumanSource(l.Source):
			person[l.TraceID] = true
		}
	}
	w.ByPerson, w.ByResult = len(person), len(result)
	for _, r := range recs {
		waits := false
		switch r.Band {
		case "ask":
			waits = !outcomeSpecs[specID]
		case "discard":
			waits = discardUnsure && !outcomeSpecs[specID]
		case "act":
			waits = r.IsCalibrated && store.AuditSample(r.TraceID)
		}
		// Of the AI's provisional scores, samples and held items (unknown) are shown to a person. Decisions that got an outcome truth first are not shown (the answer is already in)
		if rv, ok := reviews[r.TraceID]; ok && (rv.Sampled || rv.Truth == "unknown") && !result[r.TraceID] {
			waits = true
		}
		recent := now.Sub(r.At) <= personWorkWindow
		w.Records++
		if recent {
			w.RecentRecords++
		}
		if waits {
			w.Waits++
			if recent {
				w.RecentWaits++
			}
		}
	}
	return w
}

func perThousand(n, of int) float64 {
	if of == 0 {
		return 0
	}
	return float64(n) * 1000 / float64(of)
}

func (w personWork) print(out io.Writer, indent string) {
	fmt.Fprintf(out, "%sperson's work: %d of %d decisions wait for a person under the current rules (%.0f per 1000; last 7 days %.0f per 1000 of %d); truths decided by a person %d, by results %d\n",
		indent, w.Waits, w.Records, perThousand(w.Waits, w.Records), perThousand(w.RecentWaits, w.RecentRecords), w.RecentRecords, w.ByPerson, w.ByResult)
}

func (w *personWork) add(o personWork) {
	w.Records += o.Records
	w.Waits += o.Waits
	w.RecentRecords += o.RecentRecords
	w.RecentWaits += o.RecentWaits
	w.ByPerson += o.ByPerson
	w.ByResult += o.ByResult
}

// printMemoryEffect counts from real use "whether delivered lessons changed behavior" (0.7.3. A synthetic experiment showed no difference, so count in real use):
// the number of quotes delivered (including delivery to subagents), the helpful / unused / harmful / unknown the reviews gave, and how many have no observation yet.
// Delivery of corrections (followed / repeated) is printed separately. The last 7 days are counted by delivery time.
func printMemoryEffect(out io.Writer, st store.Store, scope string, now time.Time) {
	m, err := memoryEffectOf(st, scope, now)
	if err != nil || m.Delivered == 0 {
		fmt.Fprintln(out, "memory: no lessons delivered yet (lessons reach the agent before similar requests, and subagents when they start)")
		return
	}
	fmt.Fprintf(out, "memory: %d quotes delivered; helpful %d, unused %d, harmful %d, unknown %d, not yet assessed %d (%d waiting for the next request)\n", m.Delivered, m.Helpful, m.Unused, m.Harmful, m.Unknown, m.Pending, m.AwaitingFeedback)
	fmt.Fprintf(out, "  last 7 days: delivered %d, helpful %d\n", m.RecentDelivered, m.RecentHelpful)
	if c := m.Corrections; c.Delivered > 0 {
		fmt.Fprintf(out, "  corrections delivered %d: followed %d, repeated %d, not applicable %d, unknown %d, pending %d\n", c.Delivered, c.Followed, c.Repeated, c.NotApplicable, c.Unknown, c.Pending)
	}
}

// storeScope takes the parent of the store (.shikii/) as the project and uses it (real path) as the boundary for conversations and memory.
func storeScope(storeDir string) string {
	abs, err := filepath.Abs(storeDir)
	if err != nil {
		return ""
	}
	return feedbackScope(filepath.Dir(abs))
}

// memoryEffectSummary is the "effect of delivered lessons" counts (the same for CLI status and the page's /v1/status).
type memoryEffectSummary struct {
	Delivered, Helpful, Unused, Harmful, Unknown, Pending, AwaitingFeedback int
	RecentDelivered, RecentHelpful                                          int
	Corrections                                                             store.CorrectionStats
}

func memoryEffectOf(st store.Store, scope string, now time.Time) (memoryEffectSummary, error) {
	var m memoryEffectSummary
	r, err := st.MemoryReport(scope)
	if err != nil {
		return m, err
	}
	s := r.Stats
	m.Delivered, m.Helpful, m.Unused, m.Harmful, m.Unknown, m.Pending, m.AwaitingFeedback = s.Delivered, s.Helpful, s.Unused, s.Harmful, s.Unknown, s.Pending, s.AwaitingFeedback
	m.Corrections = r.Corrections
	for _, o := range r.All {
		if now.Sub(o.At) > personWorkWindow {
			continue
		}
		m.RecentDelivered++
		if o.Assessment != nil && o.Assessment.Result == "helpful" {
			m.RecentHelpful++
		}
	}
	return m, nil
}

func (m memoryEffectSummary) json() map[string]any {
	return map[string]any{"delivered": m.Delivered, "helpful": m.Helpful, "unused": m.Unused, "harmful": m.Harmful, "unknown": m.Unknown, "pending": m.Pending, "awaiting_feedback": m.AwaitingFeedback,
		"recent_delivered": m.RecentDelivered, "recent_helpful": m.RecentHelpful,
		"corrections": map[string]any{"delivered": m.Corrections.Delivered, "followed": m.Corrections.Followed, "repeated": m.Corrections.Repeated, "not_applicable": m.Corrections.NotApplicable, "unknown": m.Corrections.Unknown, "pending": m.Corrections.Pending}}
}

func (w personWork) json() map[string]any {
	return map[string]any{"records": w.Records, "waits": w.Waits, "recent_records": w.RecentRecords, "recent_waits": w.RecentWaits, "by_person": w.ByPerson, "by_result": w.ByResult, "per_thousand": perThousand(w.Waits, w.Records), "recent_per_thousand": perThousand(w.RecentWaits, w.RecentRecords)}
}

// statusSummary is the summary shown at the top of the page (0.7.4): the person's effort, the effect of delivered lessons, blocking checks.
func statusSummary(st store.Store, root, scope string, person personWork, now time.Time) map[string]any {
	out := map[string]any{"person": person.json()}
	if m, err := memoryEffectOf(st, scope, now); err == nil {
		out["memory"] = m.json()
	} else {
		out["memory"] = memoryEffectSummary{}.json()
	}
	out["guards"] = guardSummaryJSON(root)
	return out
}

// storeSpecPaths is the store's decisions/*.yaml (base layout). Read when status is given no spec paths.
func storeSpecPaths(storeDir string) []string {
	paths, _ := filepath.Glob(filepath.Join(specsDirOf(storeDir), "*.yaml"))
	return paths
}
