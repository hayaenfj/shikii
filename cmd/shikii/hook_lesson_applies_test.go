package main

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/hayaenfj/shikii/internal/notebook"
	"github.com/hayaenfj/shikii/internal/spec"
	"github.com/hayaenfj/shikii/internal/store"
)

// 0.12 item 1b: the hooks ask the decision model the ready-made question lesson-applies ("right before this command or file in this
// request, does this lesson apply?") for the lesson they are about to deliver. Its trace is kept in the delivery, and the reflection's
// assessment becomes the outcome truth: helpful is yes, unused is no, unknown gives none. The person never answers. Once a line is drawn,
// a trusted "no" skips that lesson and the next candidate is looked at; a model that fails or is down changes nothing: the words decide
// and the tool is never held up.

func TestADeliveryCarriesTheLessonAppliesDecisionAndTheAssessmentBecomesItsTruth(t *testing.T) {
	st, scope, h, ids := toolLessonFixture(t)
	edit := map[string]any{"file_path": filepath.Join(scope, "cmd", "shikii", "version.go")}
	traces := map[string]string{}
	for i, session := range []string{"ta", "tb", "tc"} {
		ctx := feedbackContext(t, toolHook(t, scope, session, toolPrompt, "Edit", edit))
		if !strings.Contains(ctx, ids["version"]) {
			t.Fatalf("precondition: delivered in %s:\n%s", session, ctx)
		}
		d := lastToolDelivery(t, st, scope)
		trace := d.Quotes[0].Trace
		if trace == "" {
			t.Fatalf("the delivery carries the trace of the lesson-applies decision: %+v", d)
		}
		rec, err := st.FindRecord("lesson-applies", trace)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(rec.Inputs["target"], "version.go") || rec.Inputs["request"] == "" || rec.Inputs["lesson"] == "" {
			t.Fatalf("the decision's inputs are the moment (target), the request and the lesson: %v", rec.Inputs)
		}
		memoryEffectComplete(t, st, scope, session)
		result := memoryEffectSubmit(t, h, d.ID, ids["version"], []string{"helpful", "unused", "unknown"}[i], "assessed in a test", "a test")
		if receipts, _ := result["memory"].([]any); len(receipts) != 1 || receipts[0].(map[string]any)["error"] != nil {
			t.Fatalf("the assessment is saved: %v", result)
		}
		traces[session] = trace
	}
	labeled, err := st.LabeledTraces("lesson-applies")
	if err != nil {
		t.Fatal(err)
	}
	if l, ok := labeled[traces["ta"]]; !ok || l.Truth != "yes" || l.Source != "outcome" {
		t.Fatalf("helpful becomes the outcome truth yes: %+v", labeled[traces["ta"]])
	}
	if l, ok := labeled[traces["tb"]]; !ok || l.Truth != "no" || l.Source != "outcome" {
		t.Fatalf("unused becomes the outcome truth no: %+v", labeled[traces["tb"]])
	}
	if _, ok := labeled[traces["tc"]]; ok {
		t.Fatalf("unknown gives no truth: %+v", labeled[traces["tc"]])
	}
	// a delivery chosen by the request's words carries a trace too
	memoryEffectPrompt(t, scope, "tr")
	turn := loadSession(st.Dir, "tr").TurnID
	ds, err := st.MemoryDeliveries(scope)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range ds {
		if d.ID == turn {
			if d.Quotes[0].Trace == "" {
				t.Fatalf("the request's delivery carries a trace as well: %+v", d)
			}
			return
		}
	}
	t.Fatal("the request's delivery was recorded")
}

func TestATrustedNoFromLessonAppliesSkipsTheLessonAndAFailureFallsBackToWords(t *testing.T) {
	repo := gitRepo(t)
	// the fake decision model: the version lesson applies (0.9), anything else does not (0.1)
	model := fakeAgentModel(t, func(state string) float64 {
		if strings.Contains(state, "Bump the README badges") {
			return 0.9
		}
		return 0.1
	})
	agent := hookEnv(t, model, repo)
	t.Chdir(repo)
	st, scope := store.Store{Dir: agent}, feedbackScope(repo)
	source, _ := reviewSeed(t, st, scope, 1)
	book := notebook.Book{Root: agent, Scope: scope}
	keys := map[string]string{}
	for name, d := range map[string]notebook.Draft{
		"version": {Title: "Bump the README badges with the version", Lesson: versionLessonText, AppliesTo: "when bumping shikii's version", Kind: "rule", Scene: "when bumping the version"},
		"commit":  {Title: "Check the exit code before committing", Lesson: commitLessonText, AppliesTo: "when committing in shikii", Kind: "rule", Scene: "when committing"},
	} {
		d.Sources = []string{source}
		r, err := book.Ingest(d)
		if err != nil {
			t.Fatal(err)
		}
		keys[name] = "lesson:" + r.Page.ID + ":" + r.Page.Revision
	}
	edit := map[string]any{"file_path": filepath.Join(scope, "cmd", "shikii", "version.go")}
	commit := map[string]any{"command": "git commit -m x"}
	spec := lowerMinN(t, agent, "lesson-applies", 2)
	for i := 0; i < 2; i++ {
		s := string(rune('a' + i))
		if ctx := feedbackContext(t, toolHook(t, scope, "v"+s, toolPrompt, "Edit", edit)); !strings.Contains(ctx, keys["version"]) {
			t.Fatalf("without a line every lesson is delivered by its words:\n%s", ctx)
		}
		if ctx := feedbackContext(t, toolHook(t, scope, "c"+s, toolPrompt, "Bash", commit)); !strings.Contains(ctx, keys["commit"]) {
			t.Fatalf("without a line every lesson is delivered by its words:\n%s", ctx)
		}
	}
	recs, err := st.Records("lesson-applies", 0)
	if err != nil || len(recs) < 4 {
		t.Fatalf("each delivery made a lesson-applies decision: %d %v", len(recs), err)
	}
	for _, r := range recs {
		truth := "no"
		if strings.Contains(r.Inputs["title"], "Bump") {
			truth = "yes"
		}
		capture(t, func() error { return cmdLabel([]string{"--store", agent, "--source", "ui", r.TraceID, truth}) })
	}
	capture(t, func() error { return cmdCalibrate([]string{spec, "--store", agent, "--model", "kev-4b", "--write"}) })
	if out := toolHook(t, scope, "after-c", toolPrompt, "Bash", commit); out != "" {
		t.Fatalf("with a line, a trusted no keeps the lesson from being delivered at that moment:\n%s", out)
	}
	if ctx := feedbackContext(t, toolHook(t, scope, "after-v", toolPrompt, "Edit", edit)); !strings.Contains(ctx, keys["version"]) {
		t.Fatalf("a yes still delivers:\n%s", ctx)
	}
	previous := hookLocalURL
	hookLocalURL = "http://127.0.0.1:1" // the model is down
	t.Cleanup(func() { hookLocalURL = previous })
	if ctx := feedbackContext(t, toolHook(t, scope, "down", toolPrompt, "Bash", commit)); !strings.Contains(ctx, keys["commit"]) {
		t.Fatalf("when the model fails the words decide and the tool is not held up:\n%s", ctx)
	}
}

// 0.12 audit C: the documents say "until a line is drawn the answer is only recorded; once drawn, a trusted no skips the lesson".
// shikii itself draws that line: the Stop hook calibrates lesson-applies on the same cycle as the hook's and the wiki's questions once
// its outcome truths reach min_n, and the line notice names it by its title. Before the fix the Stop loop knew only hookSpecs and
// wikiSpecs, so lesson-applies stayed in the shadow until a person ran shikii calibrate --write by hand.
func TestTheStopHookDrawsTheLineForLessonAppliesLikeTheWikiQuestions(t *testing.T) {
	st, scope, h, ids := toolLessonFixture(t)
	path := lowerMinN(t, st.Dir, "lesson-applies", 2)
	edit := map[string]any{"file_path": filepath.Join(scope, "cmd", "shikii", "version.go")}
	for i, session := range []string{"ca", "cb"} {
		ctx := feedbackContext(t, toolHook(t, scope, session, toolPrompt+" in session "+session, "Edit", edit)) // distinct inputs: one truth per input counts
		if !strings.Contains(ctx, ids["version"]) {
			t.Fatalf("precondition: delivered in %s:\n%s", session, ctx)
		}
		d := lastToolDelivery(t, st, scope)
		memoryEffectComplete(t, st, scope, session)
		memoryEffectSubmit(t, h, d.ID, ids["version"], []string{"helpful", "unused"}[i], "assessed in a test", "a test")
	}
	if labeled, err := st.LabeledTraces("lesson-applies"); err != nil || len(labeled) < 2 {
		t.Fatalf("precondition: min_n outcome truths for lesson-applies: %d %v", len(labeled), err)
	}
	var asked []string
	oldCal, oldGap := hookCalibrate, hookCalibrateGap
	hookCalibrate = func(agent, path, storeDir, model string) { asked = append(asked, filepath.Base(path)) }
	hookCalibrateGap = 0
	t.Cleanup(func() { hookCalibrate, hookCalibrateGap = oldCal, oldGap })
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "cc", "cwd": scope, "prompt": "explain the parser"})
	runHook(t, "stop", map[string]any{"session_id": "cc", "cwd": scope, "last_assistant_message": "Explained."})
	if !strings.Contains(strings.Join(asked, " "), "lesson-applies.yaml") {
		t.Fatalf("the Stop hook draws the line for lesson-applies once its outcome truths reach min_n (it tried: %v)", asked)
	}
	// the notice when the line is drawn names the question by its title, as for the wiki's questions
	sp := mustLoad(t, path)
	sp.Calibration = []spec.Calibration{{Model: "kev-4b", SpecHash: sp.Hash(), Method: "platt", N: 2}}
	if err := sp.Save(); err != nil {
		t.Fatal(err)
	}
	if line := lineSummary(st.Dir); !strings.Contains(line, `a line was drawn for "Does the lesson apply right now"`) {
		t.Fatalf("the line notice names lesson-applies by its title: %q", line)
	}
}
