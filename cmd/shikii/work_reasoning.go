package main

import (
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/hayaenfj/shikii/internal/notebook"
)

// Reasoning records a brief, evidence-linked judgment, not private deliberation.
// Its presence establishes neither usefulness nor success nor a human truth.
type workReasoning struct {
	Depth      string   `json:"depth,omitempty"`
	NextTrial  string   `json:"next_trial,omitempty"`
	Phase      string   `json:"phase"`
	Focus      string   `json:"focus"`
	Assessment string   `json:"assessment"`
	Reason     string   `json:"reason"`
	Evidence   []string `json:"evidence"`
	NextAction string   `json:"next_action"`
	Result     string   `json:"result,omitempty"`
}

type reasoningEntry struct {
	workReasoning
	Key        string    `json:"key"`
	Source     string    `json:"source,omitempty"`
	Generation uint64    `json:"generation"`
	At         time.Time `json:"at"`
}

type reasoningProgress struct {
	Trials          map[string]*workTrialState     `json:"trials,omitempty"`
	Comparisons     map[string]*workComparisonView `json:"comparisons,omitempty"`
	AttentionDepth  string                         `json:"attention_depth,omitempty"`
	SelectedTrial   string                         `json:"selected_trial,omitempty"`
	SessionID       string                         `json:"session_id,omitempty"`
	CurrentSource   string                         `json:"current_source,omitempty"`
	RequiredNotes   map[string]*workRequiredNote   `json:"required_notes,omitempty"`
	PendingCaptures map[string]workCapture         `json:"pending_captures,omitempty"`
	TrialCaptures   map[string]workCapture         `json:"trial_captures,omitempty"`
	TurnID          string                         `json:"turn_id"`
	Required        bool                           `json:"required"`
	Generation      uint64                         `json:"generation"`
	WikiPending     bool                           `json:"wiki_pending,omitempty"`
	PendingSource   string                         `json:"pending_source,omitempty"`
	Start           *reasoningEntry                `json:"start,omitempty"`
	Latest          *reasoningEntry                `json:"latest,omitempty"`
	Finish          *reasoningEntry                `json:"finish,omitempty"`
}

type reasoningStatus struct {
	StaleTrials        []string               `json:"stale_trials,omitempty"`
	Comparisons        []*workComparisonView  `json:"comparisons,omitempty"`
	Trials             []*workTrialState      `json:"trials,omitempty"`
	NextTrial          *workTrialChoice       `json:"next_trial,omitempty"`
	UnreviewedTrials   []string               `json:"unreviewed_trials,omitempty"`
	PendingNoteTargets map[string]string      `json:"pending_note_targets,omitempty"`
	PendingCaptures    map[string]workCapture `json:"pending_captures,omitempty"`
	PendingNotes       map[string]string      `json:"pending_notes,omitempty"`
	Required           bool                   `json:"required"`
	Started            bool                   `json:"started"`
	Complete           bool                   `json:"complete"`
	WikiPending        bool                   `json:"wiki_pending"`
	PendingSource      string                 `json:"pending_source,omitempty"`
	Latest             *reasoningEntry        `json:"latest,omitempty"`
	Next               string                 `json:"next"`
}

func reasoningView(r *reasoningProgress) reasoningStatus {
	v := reasoningStatus{Next: "Record reasoning start with a relevant premise, evidence and next_action; after work record finish with the checked result. Use update when evidence changes your judgment. Retain and unresolved judgments are valid; no new discovery or article is required."}
	if r == nil {
		return v
	}
	v.Required, v.Started, v.WikiPending, v.Latest = r.Required, r.Start != nil, r.WikiPending || len(r.RequiredNotes) > 0 || len(r.PendingCaptures) > 0, r.Latest
	for ref, note := range r.RequiredNotes {
		if v.PendingNotes == nil {
			v.PendingNotes = map[string]string{}
		}
		v.PendingNotes[ref] = note.Source
		target := note.TargetID
		if note.Expected != nil {
			target = note.Expected.ID
		}
		if target != "" {
			if v.PendingNoteTargets == nil {
				v.PendingNoteTargets = map[string]string{}
			}
			v.PendingNoteTargets[ref] = target
		}
	}
	v.PendingSource = r.PendingSource
	v.PendingCaptures = r.PendingCaptures
	v.Complete = r.Start != nil && r.Finish != nil && r.Finish.Generation == r.Generation && !v.WikiPending
	v.Trials, v.NextTrial, v.UnreviewedTrials = workTrialView(r)
	for _, c := range r.Comparisons {
		v.Comparisons = append(v.Comparisons, c)
	}
	slices.SortFunc(v.Comparisons, func(a, b *workComparisonView) int { return strings.Compare(a.Ref, b.Ref) })
	if trialPendingCount(r) > 0 {
		v.Complete = false
	}
	if v.WikiPending {
		v.Next = "Resolve each pending_notes reference with its corrected note (or retry the same content with corrected sources); read its immutable original source when needed. Read pending_note_targets article IDs with prepare before repairing their current revision. Retry pending_captures with their original content to confirm the same source and time. Then record reasoning finish. Unrelated successful work does not resolve these obligations."
		if r.PendingSource != "" {
			v.Next += " Read the archived original notes and reference bindings with wiki query " + r.PendingSource + "; restore still-needed edits using original sources and current article revisions. Preparing again does not resolve them."
		}
	} else if v.Started && !v.Complete {
		v.Next = "Review the current evidence and record reasoning finish with result; use update first if the next action changes."
	} else if v.Complete {
		v.Next = "The review is saved. New observations, actual uses, outcomes or wiki edits require an updated finish; recording alone is not proof of improvement."
	}
	if trialPendingCount(r) > 0 {
		v.Next = "Recover pending trial/comparison captures with the same reference. Cite challenged trial results in reasoning update or finish; a report is not proof of success."
	}
	return v
}

func currentReasoningView(book notebook.Book, r *reasoningProgress) reasoningStatus {
	v := reasoningView(r)
	v.NextTrial = currentTrialChoice(book, r)
	if r != nil {
		for _, trial := range v.Trials {
			if !trialBasisCurrent(book, trial.Plan.Evidence) {
				v.StaleTrials = append(v.StaleTrials, trial.Ref)
			}
		}
	}
	if v.Complete {
		current, err := reasoningEvidenceCurrent(book, r)
		if err != nil || !current {
			v.Complete = false
			v.Next = "The ending evidence changed or is unavailable. Read its current sources and article revisions, then record a new reasoning finish."
		}
	}
	return v
}

// Preparation can recover at the session-size limit without erasing the
// outstanding wiki obligation or its original draft/evidence bindings.
func archiveReasoningPending(book notebook.Book, s *hookSession) error {
	r := s.Reasoning
	if r == nil || !r.WikiPending || s.Planning.Work == nil {
		return nil
	}
	w := s.Planning.Work
	if len(w.Pending)+len(w.Expected) == 0 {
		return nil
	}
	data, err := json.Marshal(struct {
		Pending  map[string]notebook.Draft `json:"pending"`
		Sources  map[string]string         `json:"sources"`
		Articles any                       `json:"articles"`
		Expected any                       `json:"expected"`
		Saved    any                       `json:"saved"`
		Previous string                    `json:"previous_archive,omitempty"`
	}{w.Pending, w.Sources, w.Articles, w.Expected, w.Saved, r.PendingSource})
	if err != nil {
		return err
	}
	observation, err := book.Capture(notebook.Observation{Kind: "agent", Title: "Pending wiki work before replacing preparation", Content: string(data), Reference: "pending wiki work for turn " + r.TurnID, Version: "work-reasoning", ObservedAt: s.Planning.At.UTC()})
	if err != nil {
		return err
	}
	r.PendingSource = observation.ID
	return nil
}

func reasoningLeft(r *reasoningProgress) int {
	if r == nil || !r.Required {
		return 0
	}
	n := 0
	if r.Start == nil {
		n++
	}
	if r.Finish == nil || r.Finish.Generation != r.Generation {
		n++
	}
	pending := len(r.RequiredNotes) + len(r.PendingCaptures)
	if pending == 0 && r.WikiPending {
		pending = 1
	}
	n += pending
	n += trialPendingCount(r)
	return n
}

func reasoningPublicationsPending(w *workState) bool {
	for key := range w.Expected {
		if _, saved := w.Saved[key]; !saved {
			return true
		}
	}
	for ref := range w.Pending {
		if _, saved := w.Saved[w.PendingOrigins[ref]]; !saved {
			return true
		}
	}
	return false
}

func reasoningEvidenceCurrent(book notebook.Book, r *reasoningProgress) (bool, error) {
	if r.Finish == nil {
		return false, nil
	}
	for _, ref := range r.Finish.Evidence {
		if strings.HasPrefix(ref, "article:") {
			parts := strings.SplitN(strings.TrimPrefix(ref, "article:"), "@", 2)
			if len(parts) != 2 {
				return false, nil
			}
			live, err := book.LiveWhere(func(p notebook.Page) bool { return p.ID == parts[0] })
			if err != nil {
				return false, err
			}
			if len(live) != 1 || live[0].Revision != parts[1] {
				return false, nil
			}
		} else if strings.HasPrefix(ref, "source:") && !prepareWorkSource(book, strings.TrimPrefix(ref, "source:")).Complete {
			return false, nil
		}
	}
	return true, nil
}

func verifyStopReasoning(root string, current, snapshot *hookSession) error {
	if current.Scope != snapshot.Scope || current.TurnID != snapshot.TurnID || workHash(current.Reasoning) != workHash(snapshot.Reasoning) {
		return errors.New("the work evidence changed during completion; review it and save reasoning finish again")
	}
	if r := current.Reasoning; r != nil && r.Required {
		valid, err := reasoningEvidenceCurrent(notebook.Book{Root: root, Scope: current.Scope}, r)
		if err != nil {
			return err
		}
		if reasoningLeft(r) != 0 || !valid {
			return errors.New("the ending review is no longer current; review the evidence and save reasoning finish")
		}
	}
	return nil
}

func commitStopReasoning(root, session string, snapshot *hookSession, action func() error) error {
	unlock, ok := lockSessionWithin(root, session, hookLockWait)
	if !ok {
		return errors.New("completion could not check the current session lock")
	}
	defer unlock()
	current, err := readSession(root, session)
	if err != nil {
		return err
	}
	if current.Scope != snapshot.Scope || current.TurnID != snapshot.TurnID {
		return errors.New("the work evidence changed during completion; review it and save reasoning finish again")
	}
	return (notebook.Book{Root: root, Scope: current.Scope}).WithStablePages(func() error {
		if err := verifyStopReasoning(root, current, snapshot); err != nil {
			return err
		}
		if action != nil {
			return action()
		}
		return nil
	})
}

// Only newly saved work evidence invalidates an ending review. Reflection and
// delivery acknowledgements do not recursively create more reasoning work.
func reasoningMaterial(w *workState) string {
	items := map[string]workItem{}
	if w != nil {
		for k, v := range w.Saved {
			if slices.Contains([]string{"observation", "note", "outcome", "discovery"}, v.Kind) {
				items[k] = v
			}
		}
	}
	if w == nil {
		return workHash(nil)
	}
	return workHash([]any{items, w.Captures, w.Pending, w.Expected})
}

func cloneReasoning(r *reasoningProgress) *reasoningProgress {
	if r == nil {
		return nil
	}
	data, _ := json.Marshal(r)
	var copy reasoningProgress
	_ = json.Unmarshal(data, &copy)
	return &copy
}

func validateReasoning(r workReasoning) error {
	if r.Depth != "" && !slices.Contains([]string{"brief", "focused", "deep"}, r.Depth) {
		return errors.New("reasoning depth must be brief, focused or deep")
	}
	if !assignmentText(r.NextTrial, 100, false) {
		return errors.New("reasoning next_trial must fit 100 bytes")
	}
	if !slices.Contains([]string{"start", "update", "finish"}, r.Phase) || !slices.Contains([]string{"revise", "retain", "unresolved"}, r.Assessment) {
		return errors.New("reasoning needs phase start/update/finish and assessment revise/retain/unresolved")
	}
	for _, text := range []string{r.Focus, r.Reason, r.NextAction} {
		if !assignmentText(text, 1200, true) {
			return errors.New("reasoning focus, reason and next_action must each contain 1-1200 UTF-8 bytes")
		}
	}
	if !assignmentText(r.Result, 1200, r.Phase == "finish") || r.Phase != "finish" && r.Result != "" || len(r.Evidence) < 1 || len(r.Evidence) > 4 {
		return errors.New("reasoning needs one to four evidence references and result only when finishing")
	}
	for _, ref := range r.Evidence {
		if !assignmentText(ref, 200, true) {
			return errors.New("reasoning evidence references must fit 200 bytes")
		}
	}
	return nil
}

func recordWorkReasoning(book notebook.Book, w *workState, input workReasoning, resolve func([]string) ([]string, error), save func() error, commit workCommitter) (workItem, error) {
	item := workItem{Ref: "reasoning", Kind: "reasoning"}
	if err := validateReasoning(input); err != nil {
		return item, err
	}
	r := w.Reasoning
	if r == nil {
		return item, errors.New("the reasoning context is missing; prepare again")
	}
	if input.Phase != "start" && r.Start == nil {
		return item, errors.New("save reasoning start before update or finish")
	}
	if input.Phase == "finish" && (r.WikiPending || len(r.RequiredNotes) > 0 || len(r.PendingCaptures) > 0 || reasoningPublicationsPending(w)) {
		return item, errors.New("resolve pending wiki notes, captures and uncertain publications before reasoning finish")
	}
	if input.NextTrial != "" {
		t := r.Trials[input.NextTrial]
		if t == nil || t.Source == "" || t.Result != nil || !trialBasisCurrent(book, t.Plan.Evidence) {
			return item, errors.New("next_trial needs a current unresolved trial reference")
		}
	}
	// Resolve to durable identities. A local reference from another preparation
	// cannot silently name the same evidence or endorse a newer article revision.
	bound := input
	var err error
	bound.Evidence, err = bindWorkEvidence(book, w, input.Evidence, resolve)
	if err != nil {
		return item, err
	}
	if input.Phase == "finish" {
		if err := checkTrialReview(r, bound.Evidence); err != nil {
			return item, err
		}
	}
	key := "reasoning:" + workHash(bound)
	if err := preserveReasoningHistory(book, w, false, commit); err != nil {
		return item, err
	}
	previous, err := readReasoningHistory(book, r, key)
	if err != nil {
		return item, err
	}
	if previous != nil {
		return restoreReasoningHistory(w, previous, save)
	}
	if prior, ok := w.Saved[key]; ok {
		return prior, nil
	}
	if input.Phase == "start" && r.Start != nil {
		return item, errors.New("reasoning start is already saved; use update for a changed judgment")
	}
	if err := workSavedCapacity(w, key); err != nil {
		return item, err
	}
	if input.Phase == "finish" {
		// Keep one immutable agent report across the next request's replacement
		// of session progress. Exclude mutable generations from retry identity.
		snapshot := struct {
			Start    workReasoning `json:"start"`
			Previous workReasoning `json:"previous"`
			Finish   workReasoning `json:"finish"`
		}{r.Start.workReasoning, r.Latest.workReasoning, bound}
		content, _ := json.Marshal(snapshot)
		_, source, err := captureWorkObservation(book, w, workObservation{Kind: "agent", Title: "Evidence-linked work judgment and ending review", Content: string(content), Reference: "work reasoning for turn " + r.TurnID}, save, commit)
		if err != nil {
			return item, err
		}
		item.ID = source
	}
	entry := &reasoningEntry{workReasoning: bound, Key: key, Source: item.ID, Generation: r.Generation, At: time.Now().UTC()}
	stored, err := saveReasoningHistory(book, r, *entry, false, commit)
	if err != nil {
		return item, err
	}
	entry = &stored.Entry
	item.ID = entry.Source
	r.Latest = entry
	applyReasoningEffects(r, bound)
	switch input.Phase {
	case "start":
		r.Start = entry
	case "update":
		r.Finish = nil
	case "finish":
		r.Finish = entry
	}
	w.Saved[key] = item
	return item, save()
}

func applyReasoningEffects(r *reasoningProgress, entry workReasoning) {
	if entry.Depth != "" {
		r.AttentionDepth = entry.Depth
	}
	if entry.NextTrial != "" {
		r.SelectedTrial = entry.NextTrial
	}
	if entry.Phase != "start" {
		markTrialReviews(r, entry.Evidence)
	}
}
