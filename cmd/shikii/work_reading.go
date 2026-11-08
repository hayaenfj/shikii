package main

import (
	"encoding/json"
	"errors"
	"slices"
	"sort"
	"strings"

	"github.com/hayaenfj/shikii/internal/notebook"
	"github.com/hayaenfj/shikii/internal/store"
)

type workReadingFeedback struct {
	Ref        string   `json:"ref,omitempty"`
	Check      string   `json:"check"`
	Sufficient *bool    `json:"sufficient"`
	Evidence   []string `json:"evidence"`
}

type workReadingState struct {
	Ref        string                  `json:"ref"`
	Details    notebook.ReadingDetails `json:"details"`
	Source     string                  `json:"source,omitempty"`
	CaptureKey string                  `json:"capture_key,omitempty"`
	Indexed    bool                    `json:"indexed"`
}

// Keep measured delivery with the canonical turn across receipt replacement.
type workReadingTrace struct {
	Topic      string                      `json:"topic"`
	Stage      string                      `json:"stage,omitempty"`
	Case       string                      `json:"case"`
	Depth      string                      `json:"depth"`
	Articles   []notebook.ArticleReference `json:"articles"`
	Bytes      int                         `json:"bytes"`
	Reads      int                         `json:"reads"`
	Recoveries int                         `json:"recoveries"`
	Mixed      bool                        `json:"mixed,omitempty"`
}

func readingArticles(entries []any) []notebook.ArticleReference {
	ps := []notebook.ArticleReference{}
	for _, raw := range entries {
		e := raw.(map[string]any)
		p := notebook.ArticleReference{ID: e["id"].(string), Revision: e["revision"].(string)}
		if !slices.Contains(ps, p) {
			ps = append(ps, p)
		}
	}
	sort.Slice(ps, func(i, j int) bool { return ps[i].ID < ps[j].ID })
	return ps
}

func readingTopic(query string) string {
	return strings.ToLower(strings.Join(strings.Fields(query), " "))
}

func prepareReadingAttention(book notebook.Book, w *workState, entries []any, query, stage string) (workAttention, error) {
	a := attentionFor(w, query, book)
	if a.FullRead || len(entries) == 0 || len(entries) > 8 {
		return a, nil
	}
	p, err := book.ReadingPreference(readingTopic(query), stage, readingArticles(entries))
	if err != nil {
		return a, err
	}
	a.Learning = p
	if p != nil && p.Status == "adopted" && a.Depth == "focused" && w.Reasoning.AttentionDepth == "" {
		a.Depth = p.Depth
		a.Reasons = append(a.Reasons, "Use the depth supported by two separately checked tasks for these exact revisions, topic and stage; mandatory escalation and full recovery remain required.")
		a.setBudget()
	}
	return a, nil
}

func trackWorkReading(w *workState, entries []any, a workAttention, query, task, stage string) {
	if w.Reasoning == nil {
		return
	}
	r := w.Reasoning.Reading
	if r == nil {
		r = &workReadingTrace{Topic: readingTopic(query), Stage: stage, Case: workHash(task)[:16], Depth: a.Depth, Articles: readingArticles(entries)}
		r.Mixed = a.FullRead || len(entries) > 8 || strings.TrimSpace(task) == ""
		w.Reasoning.Reading = r
	} else if a.FullRead {
		for _, p := range readingArticles(entries) {
			if !slices.Contains(r.Articles, p) {
				r.Mixed = true
			}
		}
		r.Recoveries++
	} else if len(r.Articles) == 0 && len(entries) > 0 {
		// An empty search still carries its stage and delivery cost. The first
		// article search establishes the comparison basis without discarding it.
		r.Topic, r.Depth, r.Articles = readingTopic(query), a.Depth, readingArticles(entries)
	} else if r.Topic != readingTopic(query) || r.Depth != a.Depth || !slices.Equal(r.Articles, readingArticles(entries)) {
		r.Mixed = true
	}
	r.Bytes += workContextSize(entries)
	r.Reads++
	if r.Reads > 128 || r.Bytes > 16<<20 || len(r.Articles) > 8 {
		r.Mixed = true
	}
}

func recordWorkReading(book notebook.Book, w *workState, input workReadingFeedback, resolve func([]string) ([]string, error), save func() error, commit workCommitter) (workItem, error) {
	item := workItem{Ref: "reading", Kind: "reading"}
	if w.Reasoning == nil {
		return item, errors.New("prepare the reading context first")
	}
	progress := w.Reasoning
	if progress.Readings == nil {
		progress.Readings = map[string]*workReadingState{}
	}
	var state *workReadingState
	if input.Ref != "" {
		item.Ref = input.Ref
		if input.Check != "" || input.Sufficient != nil || len(input.Evidence) != 0 {
			return item, errors.New("reading recovery takes only its original reference")
		}
		state = progress.Readings[input.Ref]
		if state == nil {
			return item, errors.New("unknown reading execution reference for this turn")
		}
	} else {
		if input.Sufficient == nil || !assignmentText(input.Check, 1000, true) || len(input.Evidence) < 1 || len(input.Evidence) > 4 {
			return item, errors.New("reading needs check, explicit sufficient and one to four captured evidence references")
		}
		if progress.Reading == nil || progress.Reading.Mixed {
			return item, errors.New("reading feedback needs one measured topic and initial depth; mixed or absent reads cannot teach a default")
		}
		r := progress.Reading
		ids, err := resolve(input.Evidence)
		if err != nil {
			return item, err
		}
		evidence, err := book.OutcomeSources(ids)
		if err != nil {
			return item, err
		}
		d := notebook.ReadingDetails{Topic: r.Topic, Stage: r.Stage, Case: r.Case, Work: workHash([]string{progress.SessionID, progress.TurnID})[:16], Check: input.Check, Depth: r.Depth, Bytes: r.Bytes, Reads: r.Reads, Recoveries: r.Recoveries, Sufficient: *input.Sufficient, Articles: r.Articles, Evidence: evidence}
		if _, err := book.ReadingEvidence(d); err != nil {
			return item, err
		}
		ref := "reading:" + workHash(d)
		state = progress.Readings[ref]
		if state == nil {
			if len(progress.Readings) >= 8 {
				return item, errors.New("a turn accepts at most eight reading executions")
			}
			if err := workSavedCapacity(w, ref); err != nil {
				return item, err
			}
			w.Expected[ref] = store.KnowledgeReference{}
			state = &workReadingState{Ref: ref, Details: d}
			progress.Readings[ref] = state
			if err := save(); err != nil {
				return item, err
			}
		}
	}
	item.Ref = state.Ref
	if state.Indexed {
		item.ID = state.Source
		return item, nil
	}
	if state.Source == "" {
		data, _ := json.Marshal(state.Details)
		observation := workObservation{Kind: "agent", Title: "Measured reading execution", Content: string(data), Reference: state.Ref}
		_, key, err := prepareWorkObservation(w, observation)
		if err != nil {
			return item, err
		}
		state.CaptureKey = key
		if err := captureWorkTrialSource(book, w, observation, &state.Source, state.Ref, workItem{Ref: state.Ref, Kind: "reading"}, save, commit); err != nil {
			return item, err
		}
	}
	item.ID = state.Source
	refs, err := book.OutcomeSources([]string{state.Source})
	if err != nil {
		return item, err
	}
	if err = commit(func() error {
		return book.RecordReading(notebook.ReadingSample{ReadingDetails: state.Details, Snapshot: refs[0]})
	}); err != nil {
		return item, err
	}
	if err = workAfterWrite("reading"); err != nil {
		return item, err
	}
	state.Indexed = true
	w.Saved[state.Ref] = item
	delete(w.Expected, state.Ref)
	if !state.Details.Sufficient {
		progress.AttentionDepth = "deep"
	}
	return item, save()
}

// Carry accepted but unfinished executions across human turns. Their immutable
// details retain the original task identity; the new receipt only authorizes
// recovery. Completed history and the previous task's trace are not carried.
func nextTurnReasoning(previous *reasoningProgress, turn string) *reasoningProgress {
	r := &reasoningProgress{TurnID: turn, Required: true}
	if previous == nil {
		return r
	}
	// Keep publication obligations and their immutable original bindings, but
	// do not carry the old task's completion or current human source.
	r.RequiredNotes = previous.RequiredNotes
	r.PendingCaptures = previous.PendingCaptures
	r.PendingSource = previous.PendingSource
	r.WikiPending = previous.WikiPending || len(r.RequiredNotes) > 0 || len(r.PendingCaptures) > 0
	for _, p := range append(previous.PendingProcedures, previous.Procedure) {
		if p != nil && p.Pending != nil && p.Indexed != p.Pending.Snapshot.ID {
			r.PendingProcedures = append(r.PendingProcedures, p)
		}
	}
	for ref, state := range previous.Readings {
		if state.Indexed {
			continue
		}
		if r.Readings == nil {
			r.Readings = map[string]*workReadingState{}
		}
		r.Readings[ref] = state
		if memo, ok := previous.TrialCaptures[state.CaptureKey]; ok {
			if r.TrialCaptures == nil {
				r.TrialCaptures = map[string]workCapture{}
			}
			r.TrialCaptures[state.CaptureKey] = memo
		}
	}
	return r
}
