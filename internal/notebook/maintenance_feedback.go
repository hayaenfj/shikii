package notebook

import (
	"encoding/json"
	"sort"

	"github.com/hayaenfj/shikii/internal/store"
)

// Feedback asks the agent to inspect applicability and sources; it never edits a lesson or supplies a human truth.
func (b Book) maintenanceTasks(pages []Page) ([]MaintenanceTask, error) {
	tasks := maintenanceTasks(pages)
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
		task.FeedbackTotal = len(feedback)
		// The key covers all evidence; show up to three examples, harmful first, then newest, without cutting their evidence.
		task.Feedback = feedback[:min(3, len(feedback))]
	}
	return tasks, nil
}
