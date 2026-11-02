package notebook

import (
	"encoding/json"
	"os"
	"sort"
	"strings"
)

type OutcomeReview struct {
	Outcome            ProcedureOutcomeView `json:"outcome"`
	CurrentPassages    []TextExcerpt        `json:"current_passages"`
	ProposedCorrection string               `json:"proposed_correction"`
}

func (b Book) addOutcomeReviews(tasks []MaintenanceTask, pages []Page) error {
	dir, err := b.directory(false, "searches")
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	j, err := readSearchJournal(dir)
	if err != nil {
		return err
	}
	byID := map[string]Page{}
	for _, p := range pages {
		byID[p.ID] = p
	}
	for i := range tasks {
		task := &tasks[i]
		p := byID[task.PageID]
		if p.Kind != "howto" || p.Status == "deleted" {
			continue
		}
		all := []ProcedureOutcome{}
		for _, o := range j.Outcomes {
			if o.PageID == p.ID && strings.TrimSpace(o.Failure) != "" && strings.TrimSpace(o.Correction) != "" {
				all = append(all, o)
				// The immutable snapshot and its evidence bind this review to
				// the actual execution, not a new lookup of the same result.
				task.FeedbackKeys = append(task.FeedbackKeys, digest(mustOutcomeJSON(o))[:16])
			}
		}
		if len(all) == 0 {
			continue
		}
		data, _ := json.Marshal(struct {
			Previous string
			Outcomes []ProcedureOutcome
		}{task.Key, all})
		task.Key = digest(data)[:16]
		sort.Strings(task.FeedbackKeys)
		task.OutcomeTotal = len(all)
		for n := len(all) - 1; n >= 0 && len(task.outcomeRefs) < 2; n-- {
			task.outcomeRefs = append(task.outcomeRefs, all[n])
		}
		task.NextAction += " Compare the reported failure, correction and evidence with the current procedure passages and conditions. Treat these as revision candidates, not proof the procedure is wrong. Recheck changed revisions and premises; save a source-backed patch with the exact read revision or maintain a reason for no change."
	}
	return nil
}

// Expand only the selected review tasks: unrelated procedures must not force
// source reads or consume an unbounded evidence budget on each record call.
func (b Book) expandOutcomeReview(task *MaintenanceTask, p Page) error {
	for _, o := range task.outcomeRefs {
		view, err := b.outcomeView(o, p, o.Action+" "+o.Failure+" "+o.Correction)
		if err != nil {
			return err
		}
		task.Outcomes = append(task.Outcomes, OutcomeReview{Outcome: view, CurrentPassages: FocusExcerpts(p.Body, o.Action+" "+o.Failure, 1200), ProposedCorrection: o.Correction})
	}
	return nil
}
