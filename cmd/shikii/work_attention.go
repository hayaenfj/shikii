package main

import (
	"errors"
	"github.com/hayaenfj/shikii/internal/notebook"
	"strings"
)

func preparationDepth(input *workReasoning) (string, error) {
	if input == nil {
		return "", nil
	}
	if input.Depth != "brief" && input.Depth != "focused" && input.Depth != "deep" {
		return "", errors.New("prepare reasoning accepts only depth brief, focused or deep")
	}
	if input.Phase != "" || input.Focus != "" || input.Assessment != "" || input.Reason != "" || len(input.Evidence) > 0 || input.NextAction != "" || input.Result != "" || input.NextTrial != "" {
		return "", errors.New("prepare accepts a depth-only reading intent; record the actual judgment after reading")
	}
	return input.Depth, nil
}

// Reading depth changes the amount supplied, never the obligation to review or
// recover sources. An explicit pinned revision always remains fully readable.
type workAttention struct {
	Depth        string   `json:"depth"`
	ExcerptBytes int      `json:"excerpt_bytes_per_article"`
	TotalBytes   int      `json:"total_excerpt_bytes"`
	Reasons      []string `json:"reasons"`
	FullRead     bool     `json:"explicit_full_read,omitempty"`
}

func attentionFor(w *workState, query string, books ...notebook.Book) workAttention {
	depth := "focused"
	reasons := []string{"Use the current judgment's depth; keep required reviews and original-source recovery."}
	if w != nil && w.Reasoning != nil {
		r := w.Reasoning
		if r.AttentionDepth != "" {
			depth = r.AttentionDepth
		}
		if r.Latest != nil && r.Latest.Assessment == "unresolved" {
			depth = "deep"
			reasons = append(reasons, "The current judgment remains unresolved; inspect the deciding conditions.")
		}
		if len(books) > 0 {
			for _, trial := range r.Trials {
				if trial.Result == nil && !trialBasisCurrent(books[0], trial.Plan.Evidence) {
					depth = "deep"
					reasons = append(reasons, "A planned trial has changed or unavailable evidence; read its current premise before selecting it.")
					break
				}
			}
		}
		if r.WikiPending || len(r.RequiredNotes)+len(r.PendingCaptures) > 0 || trialPendingCount(r) > 0 {
			depth = "deep"
			reasons = append(reasons, "Unconfirmed saves or unreviewed discrepancies require deeper checking.")
		}
	}
	a := workAttention{Depth: depth, Reasons: reasons, FullRead: strings.HasPrefix(query, "lesson:") && strings.Contains(query, "@")}
	a.setBudget()
	return a
}

func (a *workAttention) setBudget() {
	switch a.Depth {
	case "brief":
		a.ExcerptBytes, a.TotalBytes = 600, 2400
	case "deep":
		a.ExcerptBytes, a.TotalBytes = 2400, 16000
	default:
		a.ExcerptBytes, a.TotalBytes = 1200, 8000
	}
}

func (a *workAttention) changedPremise() {
	if a.Depth != "deep" {
		a.Depth = "deep"
		a.Reasons = append(a.Reasons, "An article premise changed; read the relevant current conditions before relying on it.")
		a.setBudget()
	}
}
