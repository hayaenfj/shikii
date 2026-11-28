package main

import (
	"fmt"
	"slices"
	"strings"

	"github.com/hayaenfj/shikii/internal/notebook"
)

// lightenKnowledge takes the text off the knowledge entries, the last of the ranking first, until the reply fits (fits says so after
// each entry; a caller that wants every entry lightened passes a fits that is never true). A lesson first keeps its title beside its
// references (a bare id tells an agent nothing about whether to read it), and loses the title too only when that still does not fit.
// One lesson at a time: before, every lesson lost its text at once, and a reply could use half of its bound with all of them down to
// their titles (audit of 0.31.0). It returns omitted with what it named.
func lightenKnowledge(entries []any, quoted map[string]string, omitted []string, fits func() bool) []string {
	for _, keep := range []string{"title", ""} {
		name := "knowledge[] text and summaries, the last entries first (each query reads its lesson whole)"
		if keep == "" {
			name = "knowledge[] titles, the last entries first"
		}
		for i := len(entries) - 1; i >= 0; i-- {
			e := entries[i].(map[string]any)
			ref := e["ref"].(string)
			delete(quoted, ref)
			for key := range e {
				if key != "ref" && key != "id" && key != "revision" && key != "query" && key != keep {
					delete(e, key)
				}
			}
			e["body_included"], e["attention_limited"] = false, true
			e["reading_notice"] = "Original text omitted; read the exact revision query, including its conditions and exceptions."
			if !slices.Contains(omitted, name) {
				omitted = append(omitted, name)
			}
			if fits() {
				return omitted
			}
		}
	}
	return omitted
}

// maintenanceHeadNext is what a maintenance task says once its details are left out (maintenanceHeads).
const maintenanceHeadNext = "Details left out to fit the bound: read this task whole with reflect query before reviewing the article. Its ref stays valid with this receipt."

// The names output gives to the fields leaveOutLastFields leaves out. Each says how to read what went.
const (
	omittedMaintenanceDetails = "maintenance[].task details (reflect query lists each task whole)"
	omittedKnowledgeSentences = "knowledge[].query and reading_notice (the query lesson:<id>@<revision> reads each article whole)"
	omittedCarried            = "carried (the references of the interrupted request stay valid with this receipt, as that request's preparation named them)"
	omittedMaintenance        = "maintenance (reflect query lists the tasks whole)"
)

// leaveOutLastFields is what limitWorkPreparation does where it used to refuse the preparation: every field it can shrink or leave
// out is already gone, and a reply that still does not fit made the hook fall back to a manual prepare (a second call, and a reply of
// 28 KiB). The fields that are left (work_reply_fields.go registers which ones can go) go now, the one that says least about the
// task first, each only as far as the bound needs, and output names it with the way back to it:
//
//	maintenance tasks to their heads, the knowledge entries without the two sentences each repeats, carried, maintenance whole, and
//	then the entries of knowledge, the last of the ranking first.
//
// A reply that fitted before reaches none of this. update makes output again with the names given and reports whether the reply fits
// then; the result is whether it does at the end.
func leaveOutLastFields(reply map[string]any, quoted map[string]string, omitted []string, update func([]string) bool) bool {
	entries, _ := reply["knowledge"].([]any)
	// the lightening above may have stopped early because the reply fitted without output; the fields below go only after it is whole
	omitted = lightenKnowledge(entries, quoted, omitted, func() bool { return false })
	if update(omitted) {
		return true
	}
	for _, step := range []struct {
		name   string
		shrink func(map[string]any) bool
	}{
		{omittedMaintenanceDetails, maintenanceHeads},
		{omittedKnowledgeSentences, compactKnowledge},
		{omittedCarried, func(r map[string]any) bool { return dropField(r, "carried") }},
		{omittedMaintenance, func(r map[string]any) bool {
			if list, _ := r["maintenance"].([]any); len(list) == 0 {
				return false
			}
			return dropField(r, "maintenance")
		}},
	} {
		if step.shrink(reply) {
			omitted = append(omitted, step.name)
			if update(omitted) {
				return true
			}
		}
	}
	for keep := len(entries) - 1; keep >= 0; keep-- {
		reply["knowledge"] = entries[:keep]
		// the entries that went are named by the lesson they hold, as many as fit (at most omittedLessonsListed), then counted
		for listed := min(len(entries)-keep, omittedLessonsListed); listed >= 0; listed-- {
			if update(append(slices.Clone(omitted), omittedEntriesName(keep, entries[keep:], listed))) {
				return true
			}
		}
	}
	return false
}

// omittedLessonsListed is the most lessons the name of the entries that went lists by id and revision; the others are counted.
const omittedLessonsListed = 8

// omittedEntriesName is the name output gives to the entries of knowledge after the first keep, which went: how to read them, then
// the first listed of them as the query reads each ("lesson:<id>@<revision>") and the number of the rest.
func omittedEntriesName(keep int, gone []any, listed int) string {
	name := fmt.Sprintf("knowledge[] after the first %d (the query lesson:<id>@<revision> reads one; prepare with this receipt and topic words in query finds others)", keep)
	names := []string{}
	for _, raw := range gone[:listed] {
		e, _ := raw.(map[string]any)
		id, _ := e["id"].(string)
		revision, _ := e["revision"].(string)
		if id != "" && revision != "" {
			names = append(names, "lesson:"+id+"@"+revision)
		}
	}
	if len(names) == 0 {
		return name + fmt.Sprintf(": %d lessons", len(gone))
	}
	name += ": " + strings.Join(names, ", ")
	if rest := len(gone) - len(names); rest > 0 {
		name += fmt.Sprintf(" and %d more", rest)
	}
	return name
}

func dropField(reply map[string]any, field string) bool {
	if reply[field] == nil {
		return false
	}
	delete(reply, field)
	return true
}

// maintenanceHeads takes each maintenance task down to what names it: the page, its revision, the review's key and the reasons it is
// due, with how many pieces of feedback, missed discoveries and outcomes it holds. Its ref stays, so the review can still be
// recorded with this receipt. A head that would not be smaller than its task is not made.
func maintenanceHeads(reply map[string]any) bool {
	items, _ := reply["maintenance"].([]any)
	changed := false
	for _, raw := range items {
		item, ok := raw.(map[string]any)
		if !ok || item["incomplete"] == true {
			continue
		}
		task, ok := item["task"].(notebook.MaintenanceTask)
		if !ok {
			continue
		}
		head := notebook.MaintenanceTask{PageID: task.PageID, Revision: task.Revision, Key: task.Key, Title: task.Title, Reasons: task.Reasons, FeedbackTotal: task.FeedbackTotal, MissedTotal: task.MissedTotal, OutcomeTotal: task.OutcomeTotal, NextAction: maintenanceHeadNext}
		if workContextSize(head) >= workContextSize(task) {
			continue
		}
		item["task"], item["incomplete"] = head, true
		changed = true
	}
	return changed
}

// compactKnowledge takes from each lightened knowledge entry the two sentences every entry repeats: the query that reads it (it is
// "lesson:" + id + "@" + revision, which output says) and the reading notice. The reference, the id and the exact revision stay, and
// so do body_included and attention_limited: the state of what was read goes by them.
func compactKnowledge(reply map[string]any) bool {
	entries, _ := reply["knowledge"].([]any)
	changed := false
	for _, raw := range entries {
		e, ok := raw.(map[string]any)
		if !ok || e["attention_limited"] != true {
			continue
		}
		for key := range e {
			if key != "ref" && key != "id" && key != "revision" && key != "body_included" && key != "attention_limited" {
				delete(e, key)
				changed = true
			}
		}
	}
	return changed
}
