package main

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hayaenfj/shikii/internal/notebook"
)

func TestWorkTrialRejectsMismatchedCaseAndRewrittenResult(t *testing.T) {
	_, _, b, p := reasoningFixture(t)
	saveReasoning(t, b, p["receipt"], reasoningInput("start", "current"), nil)
	refs := []string{}
	for _, fixture := range []string{"csv-owner-v1", "csv-owner-v2"} {
		plan := trialPlan("small", "current")
		plan["case"] = fixture
		v := trialRecord(t, b, p["receipt"], map[string]any{"plan": plan}, nil)
		ref := trialSavedRef(t, v, "trial")
		refs = append(refs, ref)
		trialRecord(t, b, p["receipt"], map[string]any{"ref": ref, "result": map[string]any{"assessment": "supports", "observed": "This fixture retained its owner.", "evidence": []string{"new1"}, "measurements": map[string]any{"missed_conditions": 0}}}, map[string]any{"observations": []any{trialObservation(fixture + " output: owner A")}})
	}
	for _, fields := range []map[string]any{
		{"comparison": map[string]any{"before": refs[0], "after": refs[1], "reason": "Different inputs must not become a matched improvement."}},
		{"trial": map[string]any{"ref": refs[0], "result": map[string]any{"assessment": "challenges", "observed": "Replace the original success with another result.", "evidence": []string{"current"}}}},
	} {
		fields["receipt"] = p["receipt"]
		if v, err := workCall(t, b, "record", fields); err == nil && v["complete"] == true {
			t.Fatal("mismatched comparisons and rewritten observations must be refused")
		}
	}
}

func TestWorkTrialStaleArticleCannotSelectTheNextCheck(t *testing.T) {
	_, root, b, p := reasoningFixture(t)
	saveReasoning(t, b, p["receipt"], reasoningInput("start", "current"), nil)
	ref := reasoningHistoryArticle(t, b, p["receipt"])
	v := trialRecord(t, b, p["receipt"], map[string]any{"plan": trialPlan("small", ref)}, nil)
	trial := trialSavedRef(t, v, "trial")
	bound := loadSession(root, "reasoning").Planning.Work.Articles[ref]
	old, err := b.Query("lesson:" + bound.ID)
	if err != nil || len(old.Lessons) != 1 {
		t.Fatal(old, err)
	}
	page := old.Lessons[0]
	body := page.Body + "\nThe fixture's input format changed."
	_, err = b.Ingest(notebook.Draft{ID: page.ID, Title: page.Title, Lesson: page.Lesson, Body: &body, AppliesTo: page.AppliesTo, Sources: []string{page.Sources[0].ID}})
	if err != nil {
		t.Fatal(err)
	}
	brief := reasoningInput("update", "current")
	brief["depth"] = "brief"
	saveReasoning(t, b, p["receipt"], brief, nil)
	v, err = workCall(t, b, "prepare", map[string]any{"receipt": p["receipt"], "query": "lesson:" + page.ID})
	if err != nil {
		t.Fatal(err)
	}
	status := loopObject(t, v["reasoning"])
	stale, _ := status["stale_trials"].([]any)
	if status["next_trial"] != nil || len(stale) != 1 || stale[0] != trial || loopObject(t, v["attention"])["depth"] != "deep" {
		t.Fatal("changed article evidence must require re-planning, not a stale next action")
	}
}

func TestWorkTrialBoundsAndTurnOwnership(t *testing.T) {
	repo, root, b, p := reasoningFixture(t)
	saveReasoning(t, b, p["receipt"], reasoningInput("start", "current"), nil)
	for i := 0; i < 8; i++ {
		plan := trialPlan("small", "current")
		plan["case"] = fmt.Sprintf("owner-fixture-%d", i)
		trialRecord(t, b, p["receipt"], map[string]any{"plan": plan}, nil)
	}
	before := len(loadSession(root, "reasoning").Reasoning.Trials)
	bad := trialPlan("small", "current")
	bad["case"] = "ninth"
	if v, err := workCall(t, b, "record", map[string]any{"receipt": p["receipt"], "trial": map[string]any{"plan": bad}}); err == nil && v["complete"] == true {
		t.Fatal("a turn cannot grow unbounded trials")
	}
	if len(loadSession(root, "reasoning").Reasoning.Trials) != before {
		t.Fatal("rejected content must not consume trial capacity")
	}
	var oldRef string
	for ref := range loadSession(root, "reasoning").Reasoning.Trials {
		oldRef = ref
		break
	}
	runUnreviewedHook(t, "user-prompt-submit", map[string]any{"session_id": "reasoning", "cwd": repo, "prompt": "Inspect a different synthetic CSV task."})
	next := reasoningHistoryPrepare(t, root, b)
	saveReasoning(t, b, next["receipt"], reasoningInput("start", "current"), nil)
	if v, err := workCall(t, b, "record", map[string]any{"receipt": next["receipt"], "trial": map[string]any{"ref": oldRef}}); err == nil && v["complete"] == true {
		t.Fatal("a trial reference cannot resolve work from another turn")
	}
}

func TestWorkTrialCLIHTTPAndMCPShareTheExecution(t *testing.T) {
	repo, root, b, p := reasoningFixture(t)
	saveReasoning(t, b, p["receipt"], reasoningInput("start", "current"), nil)
	old := stdin
	defer func() { stdin = old }()
	raw, _ := json.Marshal(map[string]any{"receipt": p["receipt"], "trial": map[string]any{"plan": trialPlan("small", "current")}})
	stdin = strings.NewReader(string(raw))
	output := capture(t, func() error { return cmdWork([]string{"record"}) })
	var v map[string]any
	if err := json.Unmarshal([]byte(output), &v); err != nil || v["complete"] != true {
		t.Fatal(output, err)
	}
	ref := trialSavedRef(t, v, "trial")
	h := newServer(serveOpts{storeDir: root, projectScope: feedbackScope(repo)})
	payload, _ := json.Marshal(map[string]any{"operation": "record", "work": map[string]any{"receipt": p["receipt"], "trial": map[string]any{"ref": ref, "result": map[string]any{"assessment": "supports", "observed": "The synthetic owner fixture passed.", "evidence": []string{"new1"}}}, "observations": []any{trialObservation("Only owner A was observed.")}}})
	code, body, err := callTool(h, "shikii_wiki", payload)
	if err != nil || code != 200 || !strings.Contains(string(body), `"trial_result"`) {
		t.Fatal(code, string(body), err)
	}
	state := loadSession(root, "reasoning").Reasoning
	source, generation := state.Trials[ref].ResultSource, state.Generation
	request := httptest.NewRequest("POST", "/v1/wiki/notebook", strings.NewReader(string(payload)))
	request.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, request)
	state = loadSession(root, "reasoning").Reasoning
	if w.Code != 200 || state.Trials[ref].ResultSource != source || state.Generation != generation {
		t.Fatal("HTTP replay must retain the MCP execution and evidence generation", w.Code, w.Body.String())
	}
	props := workSchema["properties"].(map[string]any)
	if props["trial"] == nil || props["comparison"] == nil {
		t.Fatal("the shared MCP work schema must expose both execution operations")
	}
}
