package main

import (
	"strings"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/store"
)

// askAgain writes what asking the AI again writes for a decision a person answered: a new record that names the original, and
// the person's answer copied to it with the same source (handleRedo). The model call itself is not made here.
func askAgain(t *testing.T, st store.Store, specID, trace string) string {
	t.Helper()
	rec, err := st.FindRecord(specID, trace)
	if err != nil || rec == nil {
		t.Fatalf("the decision to ask again: %v %v", rec, err)
	}
	labeled, err := st.LabeledTraces(specID)
	if err != nil {
		t.Fatal(err)
	}
	l := labeled[trace]
	again := *rec
	again.TraceID, again.RedoOf, again.At = store.NewTraceID(), trace, time.Now().UTC()
	if err := st.AppendRecord(again); err != nil {
		t.Fatal(err)
	}
	copied, err := store.LabelsFor(&again, l.Truth, l.Weight, l.Source)
	if err == nil {
		err = st.AppendLabels(copied)
	}
	if err != nil {
		t.Fatal(err)
	}
	return again.TraceID
}

func quotedBefore(t *testing.T, st store.Store, scope, turn string) string {
	t.Helper()
	ctx, err := deliverMemory(st.Dir, scope, similarSetupRequest, nil, true, &hookSession{TurnID: turn}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	return ctx
}

// A rejected answer must not come back when its decision is asked again: the new record carries the same answer to the same
// request. It is not quoted, it is not in the list of answered decisions, and the page lists it once.
func TestARejectedAnswerStaysRejectedWhenItsDecisionIsAskedAgain(t *testing.T) {
	st, scope, h := reviewFixture(t)
	rejected := seedPersonAnswer(t, st, "needs-plan", pastSetupRequest, "yes", "ui", false)
	assessAnswer(t, st, scope, rejected, "unused", 1)
	assessAnswer(t, st, scope, rejected, "unused", 2)
	if ctx := quotedBefore(t, st, scope, "cccccccccccccc01"); strings.Contains(ctx, "decision:") {
		t.Fatalf("the rejected answer is not quoted:\n%s", ctx)
	}
	again := askAgain(t, st, "needs-plan", rejected)
	if ctx := quotedBefore(t, st, scope, "cccccccccccccc02"); strings.Contains(ctx, "decision:") {
		t.Errorf("the same answer to the same request is quoted again under the new record %s:\n%s", again, cut(ctx, 400))
	}
	if answered := pageJSON(t, h, "/v1/answered?spec=needs-plan"); strings.Contains(answered, again) || strings.Contains(answered, rejected) {
		t.Errorf("the rejected answer is back in the list of answered decisions:\n%s", cut(answered, 400))
	}
	view := rejectedAnswersOnPage(t, h)
	if len(view.Items) != 1 || view.Items[0].TraceID != again || view.Items[0].Unused != 2 {
		t.Errorf("the page lists the rejected answer once, under its newest record %s, with both assessments: %+v", again, view.Items)
	}
}

// Asked again before the rejection: the copy is quoted and found unused twice. The original carries the same answer, so it is
// not quoted in the copy's place.
func TestTheOriginalOfARejectedAnswerIsNotQuotedInItsPlace(t *testing.T) {
	st, scope, _ := reviewFixture(t)
	original := seedPersonAnswer(t, st, "needs-plan", pastSetupRequest, "yes", "ui", false)
	again := askAgain(t, st, "needs-plan", original)
	// before any assessment the answer is quoted once, under its newest record
	ctx := quotedBefore(t, st, scope, "cccccccccccccc03")
	if !strings.Contains(ctx, "decision:"+again) || strings.Contains(ctx, "decision:"+original) {
		t.Fatalf("one answer is quoted once, under its newest record %s (original %s):\n%s", again, original, cut(ctx, 600))
	}
	assessAnswer(t, st, scope, again, "unused", 1)
	assessAnswer(t, st, scope, again, "unused", 2)
	if ctx := quotedBefore(t, st, scope, "cccccccccccccc04"); strings.Contains(ctx, "decision:") {
		t.Errorf("the rejected answer is still quoted through its original:\n%s", cut(ctx, 400))
	}
}

// One unused assessment under the original and one under the copy are two assessments of one answer.
func TestAssessmentsOfAnAnswerAskedAgainCountTogether(t *testing.T) {
	st, scope, _ := reviewFixture(t)
	original := seedPersonAnswer(t, st, "needs-plan", pastSetupRequest, "yes", "ui", false)
	assessAnswer(t, st, scope, original, "unused", 1)
	again := askAgain(t, st, "needs-plan", original)
	if ctx := quotedBefore(t, st, scope, "cccccccccccccc05"); !strings.Contains(ctx, "decision:"+again) {
		t.Fatalf("one unused assessment does not reject the answer:\n%s", ctx)
	}
	assessAnswer(t, st, scope, again, "unused", 2)
	if ctx := quotedBefore(t, st, scope, "cccccccccccccc06"); strings.Contains(ctx, "decision:") {
		t.Errorf("unused once under the original and once under the copy: the answer is rejected:\n%s", cut(ctx, 400))
	}
}

// The joining is by question, answer and inputs: another answer to another request is not rejected with it, and a helpful
// assessment under the copy keeps the answer.
func TestRejectionDoesNotSpreadToAnotherAnswer(t *testing.T) {
	st, scope, _ := reviewFixture(t)
	rejected := seedPersonAnswer(t, st, "needs-plan", pastSetupRequest, "yes", "ui", false)
	assessAnswer(t, st, scope, rejected, "unused", 1)
	assessAnswer(t, st, scope, rejected, "unused", 2)
	other := seedPersonAnswer(t, st, "needs-plan", "translate the README setup steps into Japanese and keep the badges", "yes", "ui", false)
	if ctx := quotedBefore(t, st, scope, "cccccccccccccc07"); !strings.Contains(ctx, "decision:"+other) || strings.Contains(ctx, "decision:"+rejected) {
		t.Errorf("another answer to another request is still quoted, the rejected one is not:\n%s", cut(ctx, 600))
	}

	st2, scope2, _ := reviewFixture(t)
	original := seedPersonAnswer(t, st2, "needs-plan", pastSetupRequest, "yes", "ui", false)
	assessAnswer(t, st2, scope2, original, "unused", 1)
	assessAnswer(t, st2, scope2, original, "unused", 2)
	again := askAgain(t, st2, "needs-plan", original)
	assessAnswer(t, st2, scope2, again, "helpful", 3)
	if ctx := quotedBefore(t, st2, scope2, "cccccccccccccc08"); !strings.Contains(ctx, "decision:"+again) {
		t.Errorf("an answer that helped once, under any of its records, is not rejected:\n%s", cut(ctx, 400))
	}
}
