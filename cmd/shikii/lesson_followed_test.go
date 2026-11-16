package main

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/hayaenfj/shikii/internal/store"
)

// The delivery's question (lesson-applies) asks whether a lesson applies to the moment, not whether it changed the work. An agent
// often sees that its work went the way the lesson says and cannot show that the lesson caused it: that was recorded as unknown
// and taught the delivery nothing. A lesson that is not a correction now takes correction_result followed with unknown (and a
// decision): the assessment stays unknown for the lesson's effect, and becomes the truth yes of the delivery's question. It is
// not counted as helpful and does not silence or keep the lesson at that door.

// followedSubmit assesses one delivery through the reflection, with a behaviour observation.
func followedSubmit(t *testing.T, st store.Store, scope string, d store.MemoryDelivery, key, result, behaviour, decision string) error {
	t.Helper()
	return st.SaveMemoryAssessment(scope, store.MemoryAssessment{SourceID: d.ID, Key: key, Result: result, CorrectionResult: behaviour, Decision: decision, Evidence: "The reply lists the bumped badges; the record does not show what the work would have done without the lesson."})
}

func TestUnknownWithFollowedOnAPlainLessonBecomesTheTruthYes(t *testing.T) {
	st, scope, h, ids := toolLessonFixture(t)
	edit := map[string]any{"file_path": filepath.Join(scope, "cmd", "shikii", "version.go")}
	deliver := func(session string) (store.MemoryDelivery, string) {
		t.Helper()
		if ctx := feedbackContext(t, toolHook(t, scope, session, toolPrompt, "Edit", edit)); !strings.Contains(ctx, ids["version"]) {
			t.Fatalf("precondition: delivered in %s:\n%s", session, ctx)
		}
		d := lastToolDelivery(t, st, scope)
		if d.Quotes[0].Trace == "" {
			t.Fatalf("precondition: the delivery carries the trace of the lesson-applies decision: %+v", d)
		}
		memoryEffectComplete(t, st, scope, session)
		return d, d.Quotes[0].Trace
	}
	truth := func(trace string) string {
		t.Helper()
		labeled, err := st.LabeledTraces("lesson-applies")
		if err != nil {
			t.Fatal(err)
		}
		return labeled[trace].Truth
	}
	// through the reflection, as an agent sends it
	d, trace := deliver("fa")
	result := reflectCall(t, h, map[string]any{"operation": "submit", "memory": []map[string]string{{"source_id": d.ID, "key": ids["version"], "result": "unknown", "correction_result": "followed", "decision": "Bumped the README badges together with the version.", "evidence": "The diff changes both; nothing shows the lesson caused it."}}})
	if receipts, _ := result["memory"].([]any); len(receipts) != 1 || receipts[0].(map[string]any)["error"] != nil {
		t.Fatalf("unknown with followed is saved for a lesson that is not a correction: %v", result)
	}
	if got := truth(trace); got != "yes" {
		t.Fatalf("the work went the way the lesson says, so the lesson applied there: the truth of lesson-applies is yes, got %q", got)
	}
	// it teaches the door nothing: neither helpful nor unused, so the lesson is neither kept nor silenced by it
	if effect := st.PairEffects(scope)[store.PairKey(ids["version"], d.Phrase)]; effect.Helpful != 0 || effect.Unused != 0 {
		t.Fatalf("followed with unknown is not counted as helpful or unused at the door: %+v", effect)
	}
	// a second one, and the lesson still comes: unknown never silences
	d2, trace2 := deliver("fb")
	if err := followedSubmit(t, st, scope, d2, ids["version"], "unknown", "followed", "Bumped the README badges together with the version."); err != nil {
		t.Fatal(err)
	}
	if got := truth(trace2); got != "" {
		t.Fatalf("an assessment saved without the reflection gives no truth (only the reflection labels): %q", got)
	}
	if ctx := feedbackContext(t, toolHook(t, scope, "fc", toolPrompt, "Edit", edit)); !strings.Contains(ctx, ids["version"]) {
		t.Fatalf("followed twice with unknown does not silence the lesson:\n%s", ctx)
	}
	// a lesson that is not a correction is never counted among the delivered corrections
	report, err := st.MemoryReport(scope)
	if err != nil {
		t.Fatal(err)
	}
	if c := report.Corrections; c.Delivered != 0 || c.Followed != 0 {
		t.Fatalf("followed on a lesson that is not a correction is not a correction taking effect: %+v", c)
	}
	// nor among the helpful and the unused of the status: both assessments are unknown
	if s := report.Stats; s.Helpful != 0 || s.Unused != 0 || s.Unknown != 2 || s.Followed != 2 {
		t.Fatalf("unknown with followed is counted as unknown, never as helpful or unused: %+v", s)
	}
	// the count beside the unknown
	out := capture(t, func() error { return cmdStatus([]string{"--store", st.Dir}) })
	if !strings.Contains(out, "unknown 2 (the work followed the lesson in 2)") {
		t.Fatalf("status counts, of the unknown, those whose work followed the lesson:\n%s", out)
	}
}

// followed goes with unknown only. helpful already says the lesson applied, and a helpful lesson that is not a correction is not
// scored as a correction taking effect (as before); unused and harmful say the work did not follow the lesson. The other
// behaviour observations describe what happened to a correction. Each refusal names the field and what to send instead, and the
// counts of delivered corrections never include a lesson that is not one.
func TestFollowedOnAPlainLessonGoesWithUnknownOnly(t *testing.T) {
	st, scope, _, ids := toolLessonFixture(t)
	edit := map[string]any{"file_path": filepath.Join(scope, "cmd", "shikii", "version.go")}
	if ctx := feedbackContext(t, toolHook(t, scope, "ga", toolPrompt, "Edit", edit)); !strings.Contains(ctx, ids["version"]) {
		t.Fatalf("precondition: delivered:\n%s", ctx)
	}
	d := lastToolDelivery(t, st, scope)
	memoryEffectComplete(t, st, scope, "ga")
	for _, c := range []struct{ result, behaviour, decision, want string }{
		{"helpful", "followed", "Bumped the badges because the lesson named them.", "result unknown"},
		{"unused", "followed", "Bumped the badges.", "result unknown"},
		{"harmful", "followed", "Bumped the badges.", "result unknown"},
		{"unknown", "repeated", "Bumped the badges.", "send followed"},
		{"unknown", "not_applicable", "Bumped the badges.", "send followed"},
		{"unknown", "unknown", "", "send followed"},
	} {
		err := followedSubmit(t, st, scope, d, ids["version"], c.result, c.behaviour, c.decision)
		if err == nil || !strings.Contains(err.Error(), "correction_result") || !strings.Contains(err.Error(), c.want) {
			t.Fatalf("result %s with correction_result %s on a plain lesson is refused, naming the field and what to send (%q): %v", c.result, c.behaviour, c.want, err)
		}
	}
	if err := followedSubmit(t, st, scope, d, ids["version"], "unknown", "followed", ""); err == nil {
		t.Fatal("followed says what the work did: without a decision it is refused")
	}
	if err := followedSubmit(t, st, scope, d, ids["version"], "helpful", "", "Bumped the README badges because the lesson named them."); err != nil {
		t.Fatalf("helpful without a behaviour observation is saved as before: %v", err)
	}
	out := capture(t, func() error { return cmdStatus([]string{"--store", st.Dir}) })
	if strings.Contains(out, "the work followed the lesson") {
		t.Fatalf("the count is of the unknown only; with none the line says nothing of it:\n%s", out)
	}
}
