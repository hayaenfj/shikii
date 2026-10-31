package main

import (
	"errors"
	"regexp"
	"sort"
	"time"

	"github.com/hayaenfj/shikii/internal/notebook"
)

var notebookCaseID = regexp.MustCompile(`^[0-9a-f]{16}$`)

type diagnosisCase struct {
	File     string                  `json:"file,omitempty"`
	Phrases  []string                `json:"phrases,omitempty"`
	Issues   []notebook.Issue        `json:"issues,omitempty"`
	CaseID   string                  `json:"case_id"`
	At       time.Time               `json:"at"`
	TurnID   string                  `json:"turn_id"`
	Request  string                  `json:"request"`
	Mode     string                  `json:"mode"`
	Reason   string                  `json:"reason"`
	Entry    *notebook.DeliveryEntry `json:"entry,omitempty"`
	Complete bool                    `json:"complete"`
	Failure  string                  `json:"failure,omitempty"`
}

func diagnoseKnowledge(b notebook.Book, id, caseID string) (any, error) {
	if id != "" && !notebookCaseID.MatchString(id) {
		return nil, errors.New("diagnose needs a valid article id")
	}
	if caseID != "" && !notebookCaseID.MatchString(caseID) {
		return nil, errors.New("diagnose needs a valid case_id")
	}
	cases, err := b.DeliveryCases()
	if err != nil {
		return nil, err
	}
	out := []diagnosisCase{}
	for i := len(cases) - 1; i >= 0; i-- {
		c := cases[i]
		if caseID != "" && c.ID != caseID {
			continue
		}
		r := diagnosisCase{CaseID: c.ID, At: c.At, TurnID: c.TurnID, Request: c.Request, Mode: c.Mode, File: c.File, Phrases: c.Phrases, Issues: c.Issues, Complete: c.Complete, Failure: c.Failure, Reason: "not_present"}
		if !c.Complete {
			r.Reason = "unrecorded"
		}
		for _, e := range c.Entries {
			if e.ID == id {
				copy := e
				r.Entry = &copy
				r.Reason = e.Reason
				break
			}
		}
		if id == "" {
			r.Reason = "choose_article"
		}
		out = append(out, r)
	}
	if caseID != "" && len(out) == 0 {
		return nil, errors.New("the delivery case is unavailable in this project")
	}
	return struct {
		Cases  []diagnosisCase `json:"cases"`
		Notice string          `json:"notice"`
	}{out, "Recorded delivery reasons, not usefulness or permission. Missing older records are unknown."}, nil
}

type retrievalChange struct {
	ID     string `json:"id"`
	Before string `json:"before"`
	After  string `json:"after"`
}

type comparisonCase struct {
	CaseID           string            `json:"case_id"`
	Request          string            `json:"request"`
	Mode             string            `json:"mode"`
	BaselineRevision string            `json:"baseline_revision,omitempty"`
	Before           string            `json:"before,omitempty"`
	After            string            `json:"after,omitempty"`
	ObservedReason   string            `json:"observed_reason,omitempty"`
	Changes          []retrievalChange `json:"changes,omitempty"`
	Unavailable      string            `json:"unavailable,omitempty"`
}

func compareKnowledge(b notebook.Book, patch notebook.Patch, ids []string) (any, error) {
	if len(ids) > 8 {
		return nil, errors.New("compare accepts at most eight case ids")
	}
	chosen := map[string]bool{}
	for _, id := range ids {
		if !notebookCaseID.MatchString(id) || chosen[id] {
			return nil, errors.New("compare needs distinct valid case ids")
		}
		chosen[id] = true
	}
	preview, err := b.PreviewPatch(patch)
	if err != nil {
		return nil, err
	}
	cases, err := b.DeliveryCases()
	if err != nil {
		return nil, err
	}
	out := []comparisonCase{}
	for i := len(cases) - 1; i >= 0; i-- {
		c := cases[i]
		if len(ids) > 0 && !chosen[c.ID] {
			continue
		}
		r := comparisonCase{CaseID: c.ID, Request: c.Request, Mode: c.Mode}
		var target *notebook.DeliveryEntry
		for j := range c.Entries {
			if c.Entries[j].ID == patch.ID {
				target = &c.Entries[j]
				break
			}
		}
		if target == nil {
			r.Unavailable = "article_not_recorded"
		} else {
			r.BaselineRevision, r.Before, r.ObservedReason = target.Revision, target.Retrieval, target.Reason
			pages, e := b.HistoricalPages(c)
			if e != nil {
				r.Unavailable = "historical_inventory_unavailable"
			} else {
				found := false
				after := append([]notebook.Page{}, pages...)
				for j, p := range after {
					if p.ID == patch.ID {
						after[j] = *preview.Page
						found = true
						break
					}
				}
				if !found {
					r.Unavailable = "article_was_inactive"
				} else {
					beforeStates := caseRanking(c, pages)
					afterStates := caseRanking(c, after)
					if beforeStates == nil || afterStates == nil {
						r.Unavailable = "unsupported_context"
					} else {
						r.Before, r.After = beforeStates[patch.ID], afterStates[patch.ID]
						// Returning changes to the other articles exposes displaced candidates.
						for id, old := range beforeStates {
							if next := afterStates[id]; next != old {
								r.Changes = append(r.Changes, retrievalChange{id, old, next})
							}
						}
						sort.Slice(r.Changes, func(i, j int) bool { return r.Changes[i].ID < r.Changes[j].ID })
					}
				}
			}
		}
		out = append(out, r)
		delete(chosen, c.ID)
		if len(out) == 8 {
			break
		}
	}
	if len(chosen) > 0 {
		return nil, errors.New("a requested delivery case is unavailable in this project")
	}
	return struct {
		Preview *notebook.Page   `json:"preview"`
		Cases   []comparisonCase `json:"cases"`
		Notice  string           `json:"notice"`
	}{preview.Page, out, "Read-only retrieval comparison using recorded requests and revisions. Candidate does not mean delivered or useful; feedback, session suppression and model judgments are not rerun. No truth, assessment or calibration is saved."}, nil
}

func caseRanking(c notebook.DeliveryCase, pages []notebook.Page) map[string]string {
	out := map[string]string{}
	if c.Mode == "request" || c.Mode == "task" {
		_, states := notebook.RecallRanking(pages, c.Request)
		for id, e := range states {
			out[id] = e.Retrieval
		}
		return out
	}
	if c.Mode == "always" {
		for _, p := range pages {
			state := "no_match"
			if p.Kind == "history" {
				state = "history"
			} else if p.Kind == "rule" && notebook.IsAlways(p.Scene) {
				state = "candidate"
			}
			out[p.ID] = state
		}
		return out
	}
	if c.Mode != "tool" {
		return nil
	}
	target := toolTarget{file: c.File, phrases: c.Phrases}
	for _, p := range pages {
		state := "no_match"
		if p.Kind == "history" {
			state = "history"
		} else if score, _ := toolLessonScore(p, target); score > 0 {
			state = "candidate"
		}
		out[p.ID] = state
	}
	return out
}
