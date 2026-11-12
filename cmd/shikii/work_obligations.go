package main

import (
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/hayaenfj/shikii/internal/notebook"
	"github.com/hayaenfj/shikii/internal/store"
)

func nextWorkPendingReference(w *workState) (string, error) {
	if w.NextPending == 0 {
		retain := func(ref string) {
			if n, err := strconv.ParseUint(strings.TrimPrefix(ref, "note"), 10, 64); err == nil && n > w.NextPending {
				w.NextPending = n
			}
		}
		for ref := range w.Pending {
			retain(ref)
		}
		for _, ref := range w.PendingKeys {
			retain(ref)
		}
		for _, item := range w.Saved {
			retain(item.Ref)
		}
		if w.Reasoning != nil {
			for _, note := range w.Reasoning.RequiredNotes {
				if note.PendingWork == w.Nonce {
					retain(note.Pending)
				}
			}
		}
	}
	for {
		if w.NextPending == ^uint64(0) {
			return "", errors.New("pending note references are exhausted; prepare again")
		}
		w.NextPending++
		ref := "note" + strconv.FormatUint(w.NextPending, 10)
		if _, exists := w.Pending[ref]; !exists {
			return ref, nil
		}
	}
}

// A failed publication belongs to its original intent, not to the next batch.
// The draft and its prepared bindings live in an immutable source so replacing
// a full preparation can free space without discarding the recovery material.
type workNotePublication struct {
	ID       string `json:"id"`
	Revision string `json:"revision"`
	Intent   string `json:"intent"`
}

type workRequiredNote struct {
	ExpectedWork string               `json:"expected_work,omitempty"`
	ExpectedKeys []string             `json:"expected_keys,omitempty"`
	Expected     *workNotePublication `json:"expected,omitempty"`
	TargetID     string               `json:"target_id,omitempty"`
	Signature    string               `json:"signature"`
	Source       string               `json:"source,omitempty"`
	Pending      string               `json:"pending,omitempty"`
	PendingWork  string               `json:"pending_work,omitempty"`
	Sources      map[string]string    `json:"sources,omitempty"`
	Inline       *workNoteIntent      `json:"inline,omitempty"`
	At           time.Time            `json:"at"`
}

type workNoteIntent struct {
	Note     workNote                            `json:"note"`
	Sources  map[string]string                   `json:"sources"`
	Articles map[string]store.KnowledgeReference `json:"articles"`
}

func requiredNoteSignature(w *workState, n workNote) string {
	// Corrected sources and a freshly read revision still repair the same
	// content. Different content needs the explicit returned required-note ref.
	n.Ref, n.Sources, n.ChangeReason = "", nil, ""
	if article, ok := w.Articles[n.Target]; ok {
		n.Target = article.ID
	}
	if n.Target == "new" {
		n.Target = ""
	}
	return workHash(n)
}

func reserveRequiredNote(book notebook.Book, w *workState, n workNote, save func() error, commit workCommitter) (string, workNote, error) {
	if w.Reasoning == nil {
		return "", n, nil
	}
	r := w.Reasoning
	if r.RequiredNotes == nil {
		r.RequiredNotes = map[string]*workRequiredNote{}
	}
	ref := ""
	for key, entry := range r.RequiredNotes {
		if n.Ref == key || n.Ref != "" && n.Ref == entry.Pending && entry.PendingWork == w.Nonce || n.Ref == "" && entry.Signature == requiredNoteSignature(w, n) {
			ref = key
			break
		}
	}
	if ref == "" {
		if _, known := w.Pending[n.Ref]; n.Ref != "" && !known {
			return "", n, errors.New("unknown pending or required note reference")
		}
		if len(r.RequiredNotes) >= 128 {
			return "", n, errors.New("resolve the existing required notes before adding another")
		}
		if slices.Contains(n.Sources, "current") && !w.Standalone {
			id, err := resolveCurrentWorkSource(book, w, save, commit)
			if err != nil {
				return "", n, err
			}
			w.Sources["current"] = id
		}
		signature := requiredNoteSignature(w, n)
		ref = "required" + signature
		intent := &workNoteIntent{Note: n, Sources: map[string]string{}, Articles: map[string]store.KnowledgeReference{}}
		for key, id := range w.Sources {
			intent.Sources[key] = id
		}
		for key, article := range w.Articles {
			intent.Articles[key] = article
		}
		r.RequiredNotes[ref] = &workRequiredNote{TargetID: w.Articles[n.Target].ID, Signature: signature, Pending: n.Ref, PendingWork: w.Nonce, Inline: intent, At: time.Now().UTC()}
		r.WikiPending = true
		if err := save(); err != nil {
			return ref, n, err
		}
	}
	entry := r.RequiredNotes[ref]
	if n.Ref != "" && entry.TargetID != "" && entry.Expected == nil && w.Articles[n.Target].ID != entry.TargetID {
		return ref, n, errors.New("repair the same article with a freshly prepared target")
	}
	if entry.Source == "" {
		data, err := json.Marshal(entry.Inline)
		if err != nil {
			return ref, n, err
		}
		var source notebook.Observation
		if err = commit(func() error {
			var e error
			source, e = book.CaptureWork(notebook.WorkNoteIntent, string(data), "required wiki note for turn "+r.TurnID, "work-note-intent", entry.At)
			return e
		}); err != nil {
			return ref, n, err
		}
		entry.Source, entry.Inline = source.ID, nil
		if err = save(); err != nil {
			return ref, n, err
		}
	}
	_, pendingHere := w.Pending[n.Ref]
	if n.Ref == ref || n.Ref != "" && n.Ref == entry.Pending && !pendingHere {
		// A full corrected note is explicit. A ref-only retry restores the
		// original draft, keeping its sources even after aliases are replaced.
		if reflect.DeepEqual(n.Draft, notebook.Draft{}) {
			if n.Target == "" {
				return ref, n, errors.New("resolving a required note needs target new or a current prepared article reference")
			}
			material, err := book.ReadSources([]string{entry.Source})
			if err != nil {
				return ref, n, err
			}
			observations, ok := material["observations"].([]notebook.Observation)
			if !ok || len(observations) != 1 {
				return ref, n, errors.New("the original required note is unavailable")
			}
			var original workNoteIntent
			if err := json.Unmarshal([]byte(observations[0].Content), &original); err != nil {
				return ref, n, err
			}
			if original.Sources == nil {
				var archive struct {
					Work *workState `json:"work"`
				}
				if err := json.Unmarshal([]byte(observations[0].Content), &archive); err != nil || archive.Work == nil || archive.Work.Reasoning == nil {
					return ref, n, errors.New("the required note archive is incomplete")
				}
				prior := archive.Work.Reasoning.RequiredNotes[ref]
				if prior == nil || prior.Inline == nil {
					return ref, n, errors.New("the original required note is absent from its archive")
				}
				original = *prior.Inline
			}
			for source, id := range entry.Sources {
				original.Sources[source] = id
			}
			n.Draft = original.Note.Draft
			if len(n.Edits) == 0 {
				n.Edits = original.Note.Edits
			}
			rebindRequiredNote(w, &n.Draft, original, ref)
		}
		n.Ref = ""
	}
	return ref, n, nil
}

func rebindRequiredNote(w *workState, draft *notebook.Draft, original workNoteIntent, ref string) {
	source := func(old string) string {
		alias := ref + ":source:" + old
		if id := original.Sources[old]; id != "" {
			w.Sources[alias] = id
			return alias
		}
		if old == "current" {
			return old
		}
		// An unresolved old alias must not acquire another source's meaning.
		return alias
	}
	article := func(old string) string {
		alias := ref + ":article:" + old
		if page, ok := original.Articles[old]; ok {
			w.Articles[alias] = page
		}
		return alias
	}
	for i, old := range draft.Sources {
		draft.Sources[i] = source(old)
	}
	for i, old := range draft.Related {
		draft.Related[i] = article(old)
	}
	if draft.Correction != nil {
		draft.Correction.SourceID = source(draft.Correction.SourceID)
	}
	if draft.Learning == nil {
		return
	}
	for i := range draft.Learning.Connections {
		connection := &draft.Learning.Connections[i]
		if _, ok := original.Articles[connection.ID]; ok {
			connection.ID = article(connection.ID)
		}
	}
	if draft.Learning.Probe != nil {
		for i := range draft.Learning.Probe.Results {
			for j, old := range draft.Learning.Probe.Results[i].Sources {
				draft.Learning.Probe.Results[i].Sources[j] = source(old)
			}
		}
	}
	if draft.Learning.Practice != nil {
		for i := range draft.Learning.Practice.Steps {
			for j, old := range draft.Learning.Practice.Steps[i].Sources {
				draft.Learning.Practice.Steps[i].Sources[j] = source(old)
			}
		}
	}
}

func syncReasoningCaptures(w *workState) {
	if w.Reasoning == nil {
		return
	}
	w.Reasoning.PendingCaptures = nil
	for key, capture := range w.Captures {
		if capture.ID == "" {
			if w.Reasoning.PendingCaptures == nil {
				w.Reasoning.PendingCaptures = map[string]workCapture{}
			}
			w.Reasoning.PendingCaptures[key] = capture
		}
	}
}

func restoreReasoningCaptures(w *workState) {
	for key, capture := range w.Reasoning.TrialCaptures {
		w.Captures[key] = capture
	}
	for key, capture := range w.Reasoning.PendingCaptures {
		w.Captures[key] = capture
	}
}

// Called with the session lock held, before any rejected note/capture can be
// forgotten. Preserve the full context outside the bounded session and retire
// only its receipt; original obligations remain attached to the same turn.
func archiveWorkCapacity(book notebook.Book, s *hookSession) error {
	w := s.Planning.Work
	if err := preserveReasoningHistory(book, w, true, func(action func() error) error {
		if action == nil {
			return nil
		}
		return action()
	}); err != nil {
		return err
	}
	data, err := json.Marshal(struct {
		Work *workState `json:"work"`
	}{w})
	if err != nil {
		return err
	}
	observation, err := book.CaptureWork(notebook.WorkCapacity, string(data), "work capacity recovery for turn "+s.TurnID, "work-capacity", w.At)
	if err != nil {
		return err
	}
	r := w.Reasoning
	for _, note := range r.RequiredNotes {
		if note.Inline != nil {
			note.Source, note.Inline = observation.ID, nil
		}
	}
	for key, capture := range r.PendingCaptures {
		capture.Archive, capture.Original = observation.ID, nil
		r.PendingCaptures[key] = capture
	}
	r.PendingSource = observation.ID
	s.Reasoning, s.Planning.Work = r, nil
	return nil
}

// Use durable article/source bindings so a changed preparation cannot change
// the meaning of an already published note. New content is a new repair.
func requiredPublicationIntent(w *workState, n workNote, draft notebook.Draft) string {
	draft.ChangeReason = ""
	n.Draft, n.Ref = draft, ""
	if article, ok := w.Articles[n.Target]; ok {
		n.Target = article.ID
	} else if n.Target == "new" {
		n.Target = ""
	}
	return workHash(n)
}

func activeRequiredNote(w *workState) *workRequiredNote {
	if w.Reasoning == nil || w.ActiveNote == "" {
		return nil
	}
	return w.Reasoning.RequiredNotes[w.ActiveNote]
}

func bindRequiredPublication(w *workState, expected store.KnowledgeReference, intent string, keys ...string) {
	if note := activeRequiredNote(w); note != nil {
		owned := map[string]bool{}
		if note.ExpectedWork != w.Nonce {
			note.ExpectedKeys = nil
		}
		note.ExpectedWork = w.Nonce
		for _, key := range append(note.ExpectedKeys, keys...) {
			if key != "" {
				if _, exists := w.Expected[key]; exists {
					owned[key] = true
				}
			}
		}
		note.ExpectedKeys = nil
		for key := range owned {
			note.ExpectedKeys = append(note.ExpectedKeys, key)
		}
		slices.Sort(note.ExpectedKeys)
		note.Expected = &workNotePublication{ID: expected.ID, Revision: expected.Revision, Intent: intent}
	}
}

// Only a completed repair can retire its earlier uncertain reservations.
// Other required notes and their pending placement drafts remain untouched.
func finishRequiredPublication(w *workState, note *workRequiredNote) {
	if note.ExpectedWork != w.Nonce {
		return
	}
	owned := map[string]bool{}
	for _, key := range note.ExpectedKeys {
		owned[key] = true
		delete(w.Expected, key)
	}
	for ref, origin := range w.PendingOrigins {
		if owned[origin] {
			delete(w.Pending, ref)
			delete(w.PendingOrigins, ref)
			delete(w.PendingKeys, origin)
		}
	}
}

// Preview has no revision history. A missing live article is retryable only
// when neither a retained article nor its immutable revision was written.
func requiredPublicationUnwritten(book notebook.Book, publication *workNotePublication) (bool, error) {
	current, err := book.Query("lesson:" + publication.ID)
	if err != nil {
		return false, err
	}
	if len(current.Issues) != 0 {
		return false, errors.New("cannot verify the original publication; inspect wiki lint before retrying")
	}
	if len(current.Lessons) != 0 {
		return false, nil
	}
	_, err = book.Query("lesson:" + publication.ID + "@" + publication.Revision)
	if err == nil {
		return false, nil
	}
	if os.IsNotExist(err) {
		return true, nil
	}
	return false, err
}
