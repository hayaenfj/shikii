package main

import (
	"path/filepath"
	"testing"

	"github.com/hayaenfj/shikii/internal/decide"
	"github.com/hayaenfj/shikii/internal/spec"
	"github.com/hayaenfj/shikii/internal/store"
)

// needsPlanChoiceSpec loads the built-in needs-plan question the way a project copy gets it.
func needsPlanChoiceSpec(t *testing.T) *spec.Spec {
	t.Helper()
	dir := t.TempDir()
	if _, err := seedAgentSpecs(dir); err != nil {
		t.Fatal(err)
	}
	sp, err := spec.Load(filepath.Join(dir, "needs-plan.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	return sp
}

// The organizing answers only yes or no. A model top answer of not_applicable has no organizing match, so it must not agree as "no".
func TestNotApplicableTopIsNotAgreedAsNo(t *testing.T) {
	rec := store.Record{
		TraceID: "choice-not-applicable", SpecID: "needs-plan", SpecHash: "x", Type: "choice", Top: "not_applicable", P: 0.8,
		Raw:         map[string]float64{"yes": 0.1, "no": 0.1, "not_applicable": 0.8},
		Inputs:      map[string]string{"request": "r", "agreed": "Agreed approach.", "open": planningOpenUnknown},
		AgentPaired: true,
	}
	pair, ok := pairOf("needs-plan", rec, nil)
	if !ok {
		t.Fatal("fixture: the record should be paired")
	}
	if pair.agree() {
		t.Fatalf("a not_applicable top agrees with the organizing answer %q (model %q)", pair.Agent, pair.Model)
	}
}

// Control: a choice top of no that the organizing also gives is agreed, and its truth is no.
func TestNoTopAgreesWithOrganizingNo(t *testing.T) {
	rec := store.Record{
		TraceID: "choice-no", SpecID: "needs-plan", SpecHash: "x", Type: "choice", Top: "no", P: 0.8,
		Raw:         map[string]float64{"yes": 0.1, "no": 0.8, "not_applicable": 0.1},
		Inputs:      map[string]string{"request": "r", "agreed": "Agreed approach.", "open": planningOpenUnknown},
		AgentPaired: true,
	}
	pair, ok := pairOf("needs-plan", rec, nil)
	if !ok || !pair.agree() {
		t.Fatalf("control: a no top and an organizing no should agree (ok=%v model %q agent %q)", ok, pair.Model, pair.Agent)
	}
	labels, err := store.LabelsFor(&rec, pair.Agent, 1, store.SourceAgreed)
	if err != nil {
		t.Fatal(err)
	}
	if labels[0].Truth != "no" {
		t.Fatalf("the agreed truth is %q, want no", labels[0].Truth)
	}
}

// The Act advice asks the agent to review the request, which fits a yes answer only. A no or not_applicable answer gets none.
func TestActAdviceOnlyForYesAnswer(t *testing.T) {
	sp := needsPlanChoiceSpec(t)
	yes := &decide.Decision{SpecID: "needs-plan", Type: string(sp.Type), Top: "yes", P: 0.9, Band: decide.Act, IsCalibrated: true, TraceID: "act-yes"}
	if out := hookOutcome("needs-plan", yes, localize(sp, yes, "")); out.Context == "" {
		t.Fatal("control: a yes answer in the Act band gets the review advice")
	}
	for _, top := range []string{"no", "not_applicable"} {
		d := &decide.Decision{SpecID: "needs-plan", Type: string(sp.Type), Top: top, P: 0.9, Band: decide.Act, IsCalibrated: true, TraceID: "act-" + top}
		if out := hookOutcome("needs-plan", d, localize(sp, d, "")); out.Context != "" || out.System != "" {
			t.Fatalf("top %q in the Act band gets the review advice written for yes: %q", top, out.Context)
		}
	}
}

// For a choice question discard means unsure (no answer reaches the ask threshold). An internal review does not wait for a human grade
// for that: only a concrete open decision is asked (wiki rule f51716dd9cbe9863). So an unsure choice is not sent to the person.
func TestUnsureChoiceDoesNotWaitForAGrade(t *testing.T) {
	sp := needsPlanChoiceSpec(t)
	if !decide.DiscardUnsure(sp) {
		t.Fatal("fixture: a choice discard should be unsure")
	}
	d := &decide.Decision{SpecID: "needs-plan", Type: string(sp.Type), Top: "yes", P: 0.4, Band: decide.Discard, TraceID: "unsure"}
	if waitsForPersonFor("needs-plan", d) {
		t.Fatal("an unsure choice is sent to the person as a grade: internal reviews do not wait for a human grade")
	}
}

// Control: for a noul question discard is a confident no, and it does not wait.
func TestConfidentNoulDiscardDoesNotWait(t *testing.T) {
	d := &decide.Decision{SpecID: "needs-plan", Type: "noul", Top: "yes", P: 0.1, Band: decide.Discard, TraceID: "confident-no"} // a noul top is always yes; P is p(yes)
	if waitsForPersonFor("needs-plan", d) {
		t.Fatal("a noul discard is a confident no and must not wait for the person")
	}
}
