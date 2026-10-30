package main

// lesson-applies (0.12): the ready-made question the hooks ask the decision model about the lesson they are about to deliver: "right
// before this command or file in this request, does this lesson apply?". Word matching picks the candidates (cheap, no model); this
// question is asked once for the one about to go out. Until a line is drawn the answer is only recorded (shadow). Once drawn, a
// trusted "no" skips the lesson and the next candidate is looked at. The truth comes from the agent's own reflection on the delivery
// (helpful = yes, unused or harmful = no, unknown = none): the person never answers, as with wiki-place. A model that fails or is down
// changes nothing: the words decide, and a tool call is never held up for it.

import (
	"path/filepath"
	"strings"

	"github.com/hayaenfj/shikii/internal/decide"
	"github.com/hayaenfj/shikii/internal/notebook"
	"github.com/hayaenfj/shikii/internal/spec"
	"github.com/hayaenfj/shikii/internal/store"
)

const lessonAppliesSpec = "lesson-applies"

// deliverySpecs are the ready-made questions about delivering lessons (ids of the embedded specs), beside hookSpecs and wikiSpecs.
var deliverySpecs = []string{lessonAppliesSpec}

// lessonAppliesMaxAsks is how many candidates one tool call asks the model about at most (audit A of 0.12: with every candidate a
// trusted no, six lessons naming git commit cost six calls; beyond this many, nothing is delivered rather than asking on).
const lessonAppliesMaxAsks = 3

// lessonAppliesRequestTarget is the target when the lesson is chosen by the request's words, not before a tool call.
const lessonAppliesRequestTarget = "the request itself (before any command or file)"

// lessonApplies asks whether page applies to this moment (target: "Bash git commit", "Edit cmd/shikii/version.go", or the request).
// It returns the decision's trace (empty when none was made) and whether the lesson is to be skipped: only a trusted, confident "no"
// (the band discard, with a line whose confident side was right often enough) skips. Errors skip nothing.
func lessonApplies(agent, scope, request, target string, page notebook.Page) (trace string, skip bool) {
	inputs := map[string]string{
		"request":    cut(strings.TrimSpace(request), requestMax),
		"target":     cut(target, 400),
		"title":      page.Title,
		"lesson":     page.Lesson,
		"applies_to": page.AppliesTo,
	}
	if page.DiscoveryContext() != "" {
		inputs["applies_to"] += "\nEarlier evidenced manual use (an agent observation, not permission): " + cut(page.DiscoveryContext(), 1000)
	}
	if hookSkipsStopped(agent, lessonAppliesSpec, inputs) {
		return "", false // the question could not tell its answers apart on this model: asked on 1 request in 20 only, like the hook's questions
	}
	d, _, err := hookDecide(agent, lessonAppliesSpec, inputs, filepath.Base(scope))
	if err != nil || d == nil || d.Band == decide.Abstain {
		return "", false
	}
	resumeOnNewModel(agent, lessonAppliesSpec, d.Model) // a stop measured on another model is lifted once this one answers (confirmation audit 1)
	if d.Band == decide.Discard {
		if sp, err := spec.Load(hookSpecPath(agent, lessonAppliesSpec)); err == nil && discardTrusted(sp, d) {
			return d.TraceID, true
		}
	}
	return d.TraceID, false
}

// labelLessonApplies turns the reflection's assessment of one delivered quote into the outcome truth of its lesson-applies decision:
// helpful is yes, unused and harmful are no, anything else gives none. Each truth lands once (AppendLabelsOnce). A delivery without a
// trace (an always rule, a person's answer, a record from before 0.12) gets nothing.
func labelLessonApplies(st store.Store, scope string, a store.MemoryAssessment) error {
	truth := ""
	switch a.Result {
	case "helpful":
		truth = "yes"
	case "unused", "harmful":
		truth = "no"
	default:
		return nil
	}
	deliveries, err := st.MemoryDeliveries(scope)
	if err != nil {
		return err
	}
	for _, d := range deliveries {
		if d.ID != a.SourceID {
			continue
		}
		for _, q := range d.Quotes {
			if q.Key == a.Key && q.Trace != "" {
				return labelTrace(st, lessonAppliesSpec, q.Trace, truth, 1, "outcome")
			}
		}
	}
	return nil
}
