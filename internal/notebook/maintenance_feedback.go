package notebook

import (
	"encoding/json"
	"sort"

	"github.com/hayaenfj/shikii/internal/store"
)

// Feedback asks the agent to inspect applicability and sources; it never edits a lesson or supplies a human truth.
func (b Book) maintenanceTasks(pages []Page) ([]MaintenanceTask, error) {
	tasks := maintenanceTasks(pages)
	// Supersession depends on every stored article, including replacements
	// whose evidence is currently unavailable. Losing evidence must not revive
	// the earlier procedure. Share this rule across queries, keys and saves.
	all, _, err := b.pages()
	if err != nil {
		return nil, err
	}
	retired := map[string]bool{}
	for _, p := range all {
		for _, id := range p.Replaces {
			retired[id] = true
		}
	}
	active := tasks[:0]
	for _, task := range tasks {
		if !retired[task.PageID] {
			active = append(active, task)
		}
	}
	tasks = active
	if len(tasks) == 0 {
		return tasks, nil
	}
	assessments, err := (store.Store{Dir: b.Root}).MemoryAssessments(b.Scope)
	if err != nil {
		return nil, err
	}
	byQuote := map[string][]store.MemoryAssessment{}
	for _, a := range assessments {
		if a.Result != "unknown" {
			byQuote[a.Key] = append(byQuote[a.Key], a)
		}
	}
	for i := range tasks {
		task := &tasks[i]
		for _, p := range pages {
			if p.ID == task.PageID {
				task.ChangedFiles = b.premiseChanges(p)
				break
			}
		}
		if len(task.ChangedFiles) > 0 {
			data, _ := json.Marshal(struct {
				Content string
				Files   []PremiseChange
			}{task.Key, task.ChangedFiles})
			task.Key = digest(data)[:16]
		}
		task.ContentKey = task.Key
		all := byQuote["lesson:"+task.PageID+":"+task.Revision]
		counts := map[string]store.PairEffect{}
		for _, a := range all {
			c := counts[a.Phrase]
			switch a.Result {
			case "helpful":
				c.Helpful++
			case "unused":
				c.Unused++
			}
			counts[a.Phrase] = c
		}
		feedback := []store.MemoryAssessment{}
		for _, a := range all {
			c := counts[a.Phrase]
			if a.Result == "harmful" || (a.Result == "unused" && a.Phrase != "" && c.Unused >= 2 && c.Helpful == 0) {
				feedback = append(feedback, a)
			}
		}
		if len(feedback) == 0 {
			continue
		}
		sort.Slice(feedback, func(i, j int) bool {
			if (feedback[i].Result == "harmful") != (feedback[j].Result == "harmful") {
				return feedback[i].Result == "harmful"
			}
			if !feedback[i].At.Equal(feedback[j].At) {
				return feedback[i].At.After(feedback[j].At)
			}
			return feedback[i].SourceID < feedback[j].SourceID
		})
		data, _ := json.Marshal(struct {
			Content  string
			Feedback []store.MemoryAssessment
		}{task.Key, feedback})
		task.Key = digest(data)[:16]
		for _, assessment := range feedback {
			data, _ := json.Marshal(assessment)
			task.FeedbackKeys = append(task.FeedbackKeys, digest(data)[:16])
		}
		sort.Strings(task.FeedbackKeys)
		task.FeedbackTotal = len(feedback)
		// The key covers all evidence; show up to three examples, harmful first, then newest, without cutting their evidence.
		task.Feedback = feedback[:min(3, len(feedback))]
	}
	missed, err := b.missedDiscoveries()
	if err != nil {
		return nil, err
	}
	for i := range tasks {
		task := &tasks[i]
		all := []MissedDiscovery{}
		for _, d := range missed {
			if d.PageID == task.PageID && d.Revision == task.Revision {
				all = append(all, d)
				raw, _ := json.Marshal(d)
				task.FeedbackKeys = append(task.FeedbackKeys, digest(raw)[:16])
			}
		}
		if len(all) == 0 {
			continue
		}
		raw, _ := json.Marshal(struct {
			Previous string
			Missed   []MissedDiscovery
		}{task.Key, all})
		task.Key = digest(raw)[:16]
		sort.Strings(task.FeedbackKeys)
		task.MissedTotal = len(all)
		// Show newest evidence first; all evidence contributes to the review key.
		for j := len(all) - 1; j >= 0 && len(task.Missed) < 3; j-- {
			task.Missed = append(task.Missed, all[j])
		}
		task.NextAction = "Read the article and missed evidence; diagnose the recorded case, capture supporting evidence, compare an applicability patch before saving it, or maintain with a reason for no change. Do not reset suppression by changing the revision. Check usefulness in subsequent actual work."
	}
	if err := b.addOutcomeReviews(tasks, pages); err != nil {
		return nil, err
	}
	return tasks, nil
}
