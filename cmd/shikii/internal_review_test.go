package main

import (
	"testing"

	"github.com/hayaenfj/shikii/internal/decide"
	"github.com/hayaenfj/shikii/internal/store"
)

func TestInternalReviewsNeverBecomeRequiredHumanAnswers(t *testing.T) {
	for _, id := range []string{"needs-plan", "changes-in-scope", "done-as-asked"} {
		for _, truth := range []string{"yes", "no", "unknown"} {
			rec := store.Record{TraceID: "internal-check", SpecID: id, Type: "noul", Band: "ask", Raw: map[string]float64{"yes": .8, "no": .2}, ReviewPending: true, AgentPaired: true, Inputs: map[string]string{"request": "Carry out the agreed task.", "agreed": "Agreed task.", "open": "A question remains."}}
			reviews := map[string]store.Review{rec.TraceID: {TraceID: rec.TraceID, Truth: truth, ModelShown: true}}
			q := queueItemOf(id, rec, nil, reviews, nil)
			if q.show || q.waits {
				t.Fatalf("%s/%s is internal review work, not a person's required score: %+v", id, truth, q)
			}
		}
	}
	ordinary := store.Record{TraceID: "business-check", Type: "noul", Band: "ask"}
	if q := queueItemOf("approve-expense", ordinary, nil, nil, nil); !q.show || !q.waits {
		t.Fatal("an actual business decision must still wait for its owner")
	}
	t.Run("uncertain internal wording", func(t *testing.T) {
		root, _, result := wordingCheckFixture(t)
		if _, err := submitWordingCheck(t, checkBody(result, "unsure")); err != nil {
			t.Fatal(err)
		}
		if notice := wordingSummary(root); notice != "" {
			t.Fatal("an uncertain internal wording must remain unadopted without asking a person to grade it", notice)
		}
	})
}

func TestInternalHookNeverAsksPersonToGrade(t *testing.T) {
	for _, id := range []string{"needs-plan", "changes-in-scope", "done-as-asked"} {
		for _, band := range []decide.Band{decide.Act, decide.Ask, decide.Discard} {
			o := hookOutcome(id, &decide.Decision{IsCalibrated: true, Band: band, P: .1, TraceID: "check"}, hookLoc{})
			if o.System != "" {
				t.Fatal("internal grades must not ask the person to fix or re-score work", id, band, o)
			}
		}
	}
}
