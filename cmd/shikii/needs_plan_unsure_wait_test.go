package main

import (
	"testing"

	"github.com/hayaenfj/shikii/internal/decide"
	"github.com/hayaenfj/shikii/internal/store"
)

// unsureChoiceRecord is a needs-plan choice the model could not settle (discard band) and that the agent answered with the given open text.
func unsureChoiceRecord(top, open string) (*decide.Decision, store.Record) {
	d := &decide.Decision{SpecID: "needs-plan", Type: "choice", Top: top, P: 0.4, Band: decide.Discard, TraceID: "unsure-" + top}
	rec := store.Record{
		TraceID: d.TraceID, SpecID: "needs-plan", SpecHash: "x", Type: "choice", Band: "discard", Top: top, P: 0.4,
		Raw:         map[string]float64{"yes": 0.4, "no": 0.3, "not_applicable": 0.3},
		Inputs:      map[string]string{"request": "Draft a short note.", "agreed": "Write the note in plain words.", "open": open},
		AgentPaired: true,
	}
	return d, rec
}

// The changelog says an unsure choice is not sent to the person for a grade: only a concrete open decision is asked. With nothing
// open (the agent's open is unknown) the queue must not show it, even though the model's top yes and the agent's organizing no differ.
func TestUnsureChoiceWithNothingOpenIsNotInTheQueue(t *testing.T) {
	_, rec := unsureChoiceRecord("yes", planningOpenUnknown)
	show, _, reason := reviewQueueState("needs-plan", rec, nil, nil)
	if show {
		t.Fatalf("an unsure choice with nothing open is asked of the person: show=%v reason=%q", show, reason)
	}
}

// The planning hook must not wait for the person in that case either.
func TestUnsureChoiceWithNothingOpenDoesNotWaitInThePlanningHook(t *testing.T) {
	d, rec := unsureChoiceRecord("yes", planningOpenUnknown)
	if planningWaits(d, rec) {
		t.Fatal("an unsure choice with nothing open waits for the person in the planning hook")
	}
}

// Control: an unsure choice with a concrete open decision that disagrees with the model's top answer is asked, as that decision.
func TestUnsureChoiceWithConcreteOpenDisagreementWaits(t *testing.T) {
	d, rec := unsureChoiceRecord("no", "Ask whether the note should be in English.")
	if !planningWaits(d, rec) {
		t.Fatal("control: a concrete open decision that disagrees must wait for the person")
	}
}

// Control: an unsure choice with a concrete open decision that agrees with the model's top answer does not wait.
func TestUnsureChoiceWithConcreteOpenAgreementDoesNotWait(t *testing.T) {
	d, rec := unsureChoiceRecord("yes", "Ask whether the note should be in English.")
	if planningWaits(d, rec) {
		t.Fatal("control: a concrete open decision that agrees must not wait for the person")
	}
}
