package main

import (
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/notebook"
	"github.com/hayaenfj/shikii/internal/store"
)

func reasoningFixture(t *testing.T) (string, string, notebook.Book, map[string]any) {
	t.Helper()
	repo, root, _ := planningFixture(t)
	runUnreviewedHook(t, "user-prompt-submit", map[string]any{"session_id": "reasoning", "cwd": repo, "prompt": planningTestRequest})
	b := notebook.Book{Root: root, Scope: feedbackScope(repo)}
	v, err := workCall(t, b, "prepare", map[string]any{"session_id": "reasoning", "turn_id": loadSession(root, "reasoning").TurnID, "query": "CSV conditions"})
	if err != nil {
		t.Fatal(err)
	}
	return repo, root, b, v
}

func reasoningInput(phase string, evidence ...string) map[string]any {
	v := map[string]any{"phase": phase, "focus": "Could the CSV export lose its existing filters?", "assessment": "retain", "reason": "Compare the actual filter conditions before choosing the next action.", "evidence": evidence, "next_action": "Check the synthetic export under the existing owner condition."}
	if phase == "finish" {
		v["result"] = "The synthetic export retained the owner condition; a different input format remains untested."
	}
	return v
}

func saveReasoning(t *testing.T, b notebook.Book, receipt any, reasoning map[string]any, extra map[string]any) map[string]any {
	t.Helper()
	input := map[string]any{"receipt": receipt, "reasoning": reasoning}
	for k, v := range extra {
		input[k] = v
	}
	v, err := workCall(t, b, "record", input)
	if err != nil || v["complete"] != true {
		t.Fatal(v, err)
	}
	return v
}

func TestReasoningHookRequiresWorkEvenWithoutPrepare(t *testing.T) {
	repo, root, _ := planningFixture(t)
	runUnreviewedHook(t, "user-prompt-submit", map[string]any{"session_id": "reasoning", "cwd": repo, "prompt": planningTestRequest})
	out := runUnreviewedHook(t, "stop", map[string]any{"session_id": "reasoning", "cwd": repo, "last_assistant_message": "Checked."})
	if !strings.Contains(out, "reasoning-before-end") || !strings.Contains(out, "work prepare") || loadSession(root, "reasoning").Judged {
		t.Fatalf("a request must retain its reasoning obligation before prepare: %s", out)
	}
	requireRecoveryHalt(t, runUnreviewedHook(t, "stop", map[string]any{"session_id": "reasoning", "cwd": repo, "last_assistant_message": "Not yet."}))
}

func TestReasoningRecordsStartUpdateFinishAndReopensOnEvidence(t *testing.T) {
	_, _, b, v := reasoningFixture(t)
	receipt := v["receipt"]
	if got, err := workCall(t, b, "record", map[string]any{"receipt": receipt, "reasoning": reasoningInput("finish", "current")}); err != nil || got["complete"] != true {
		t.Fatal("the prepared start must support an ending judgment", got, err)
	}
	saveReasoning(t, b, receipt, reasoningInput("start", "current"), nil)
	saved := saveReasoning(t, b, receipt, reasoningInput("finish", "new1"), map[string]any{"observations": []any{map[string]any{"kind": "test", "title": "CSV owner check", "content": "The synthetic CSV retained the owner condition."}}})
	if loopObject(t, saved["reasoning"])["complete"] != true {
		t.Fatal("a supported retain judgment must complete the checkpoint", saved)
	}
	snapshot := ""
	for _, raw := range saved["saved"].([]any) {
		item := loopObject(t, raw)
		if item["kind"] == "reasoning" {
			snapshot, _ = item["id"].(string)
		}
	}
	if snapshot == "" {
		t.Fatal("the ending judgment must survive replacement of session progress", saved)
	}
	sources, err := b.ReadSources([]string{snapshot})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(sources)
	if !strings.Contains(string(raw), "Evidence-linked work judgment") || !strings.Contains(string(raw), "synthetic export retained") {
		t.Fatal("the durable agent report lost the starting judgment or result", string(raw))
	}
	again, err := workCall(t, b, "record", map[string]any{"receipt": receipt, "observations": []any{map[string]any{"kind": "test", "title": "Different input", "content": "A quoted field exposed a different condition."}}})
	if err != nil || loopObject(t, again["reasoning"])["complete"] != false {
		t.Fatal("new checked evidence must reopen the ending review", again, err)
	}
	evidence := ""
	for _, raw := range again["saved"].([]any) {
		item := loopObject(t, raw)
		if item["kind"] == "observation" {
			evidence, _ = item["ref"].(string)
		}
	}
	if evidence == "" {
		t.Fatal("the new observation must have a saved reference", again)
	}
	updated := reasoningInput("update", evidence)
	updated["assessment"], updated["next_action"] = "revise", "Check quoted fields before reusing this procedure."
	saveReasoning(t, b, receipt, updated, nil)
	end := reasoningInput("finish", evidence)
	end["result"] = "The quoted-field condition remains unresolved and is recorded for the next relevant work."
	saveReasoning(t, b, receipt, end, nil)
}

func TestReasoningSurvivesPreparationAndRejectsStaleReceipts(t *testing.T) {
	_, root, b, v := reasoningFixture(t)
	start := reasoningInput("start", "current")
	saveReasoning(t, b, v["receipt"], start, nil)
	next, err := workCall(t, b, "prepare", map[string]any{"session_id": "reasoning", "turn_id": loadSession(root, "reasoning").TurnID, "query": "CSV owner"})
	if err != nil || loopObject(t, next["reasoning"])["started"] != true {
		t.Fatal("preparing again must preserve the saved start", next, err)
	}
	if _, err := workCall(t, b, "record", map[string]any{"receipt": v["receipt"], "reasoning": reasoningInput("finish", "current")}); err == nil {
		t.Fatal("a replaced receipt cannot finish current work")
	}
	for _, ref := range []string{"knowledge999", "review1", "new1"} {
		if got, err := workCall(t, b, "record", map[string]any{"receipt": next["receipt"], "reasoning": reasoningInput("finish", ref)}); err == nil && got["complete"] == true {
			t.Fatalf("unavailable evidence %q was accepted: %v", ref, got)
		}
	}
	// Finish through the real CLI decoder; preparation and the earlier writes
	// used the notebook transport. Both must retain the same receipt and schema.
	raw, _ := json.Marshal(map[string]any{"receipt": next["receipt"], "reasoning": reasoningInput("finish", "current")})
	old := stdin
	stdin = strings.NewReader(string(raw))
	defer func() { stdin = old }()
	output := capture(t, func() error { return cmdWork([]string{"record"}) })
	var result map[string]any
	if err := json.Unmarshal([]byte(output), &result); err != nil || result["complete"] != true {
		t.Fatal(output, err)
	}
	if _, ok := workSchema["properties"].(map[string]any)["reasoning"]; !ok {
		t.Fatal("the MCP schema omits reasoning")
	}
}

func TestReasoningReplayCannotCertifyLaterEvidence(t *testing.T) {
	repo, root, b, v := reasoningFixture(t)
	saveReasoning(t, b, v["receipt"], reasoningInput("start", "current"), nil)
	finish := reasoningInput("finish", "current")
	saveReasoning(t, b, v["receipt"], finish, nil)
	out := runUnreviewedHook(t, "stop", map[string]any{"session_id": "reasoning", "cwd": repo, "last_assistant_message": "Checked."})
	if strings.Contains(out, "block") || !loadSession(root, "reasoning").Judged {
		t.Fatal(out)
	}
	// The same request continues after a background notification.
	runUnreviewedHook(t, "user-prompt-submit", map[string]any{"session_id": "reasoning", "cwd": repo, "prompt": "<task-notification>synthetic continuation</task-notification>"})
	if _, err := workCall(t, b, "record", map[string]any{"receipt": v["receipt"], "observations": []any{map[string]any{"kind": "test", "title": "Later evidence", "content": "Another condition is still unresolved."}}}); err != nil {
		t.Fatal(err)
	}
	again := saveReasoning(t, b, v["receipt"], finish, nil)
	if loopObject(t, again["reasoning"])["complete"] != false {
		t.Fatal("an old finish was applied to later evidence", again)
	}
	out = runUnreviewedHook(t, "stop", map[string]any{"session_id": "reasoning", "cwd": repo, "last_assistant_message": "Another condition remains."})
	if !strings.Contains(out, "reasoning-before-end") {
		t.Fatal(out)
	}
}

func TestReasoningFailedWikiCannotBeClearedByPreparingAgain(t *testing.T) {
	_, root, b, v := reasoningFixture(t)
	saveReasoning(t, b, v["receipt"], reasoningInput("start", "current"), nil)
	failed, err := workCall(t, b, "record", map[string]any{"receipt": v["receipt"], "notes": []any{map[string]any{"title": "CSV filter procedure", "lesson": "Keep the owner condition.", "applies_to": "CSV export", "kind": "howto", "scene": "exporting", "sources": []string{"missing"}}}, "reasoning": reasoningInput("finish", "current")})
	if err != nil || failed["complete"] != false || loopObject(t, failed["reasoning"])["wiki_pending"] != true {
		t.Fatal(failed, err)
	}
	next, err := workCall(t, b, "prepare", map[string]any{"session_id": "reasoning", "turn_id": loadSession(root, "reasoning").TurnID, "query": "CSV"})
	if err != nil || loopObject(t, next["reasoning"])["wiki_pending"] != true {
		t.Fatal("replacing preparation must retain the obligation", next, err)
	}
	if got, err := workCall(t, b, "record", map[string]any{"receipt": next["receipt"], "reasoning": reasoningInput("finish", "current")}); err == nil && got["complete"] == true {
		t.Fatal(got)
	}
	// Correct the same intended procedure and save its real source.
	saveReasoning(t, b, next["receipt"], reasoningInput("finish", "current"), map[string]any{"notes": []any{map[string]any{"title": "CSV filter procedure", "lesson": "Keep the owner condition.", "applies_to": "CSV export", "kind": "howto", "scene": "exporting", "sources": []string{"current"}}}})
}

func TestReasoningEndingArticleIsRecheckedAtStop(t *testing.T) {
	repo, root, b, v := reasoningFixture(t)
	captured, err := b.CaptureObserved(notebook.Observation{Kind: "test", Title: "CSV procedure evidence", Content: "The synthetic export retained ownership.", Reference: "synthetic fixture", Version: "test", ObservedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	source := captured.ID
	body := "Check ownership before export."
	page, err := b.Ingest(notebook.Draft{Title: "CSV owner procedure", Lesson: "Keep ownership.", Body: &body, AppliesTo: "CSV export", Kind: "howto", Scene: "exporting", Sources: []string{source}})
	if err != nil {
		t.Fatal(err)
	}
	read, err := workCall(t, b, "prepare", map[string]any{"receipt": v["receipt"], "query": "lesson:" + page.Page.ID})
	if err != nil {
		t.Fatal(err)
	}
	ref := read["knowledge"].([]any)[0].(map[string]any)["ref"].(string)
	saveReasoning(t, b, v["receipt"], reasoningInput("start", ref), nil)
	saveReasoning(t, b, v["receipt"], reasoningInput("finish", ref), nil)
	body = "Check ownership and quoted input before export."
	draft := notebook.Draft{ID: page.Page.ID, Title: page.Page.Title, Lesson: "Check quoted input as well.", Body: &body, AppliesTo: page.Page.AppliesTo, Kind: "howto", Scene: "exporting", Sources: []string{source}}
	if _, err = b.Ingest(draft); err != nil {
		t.Fatal(err)
	}
	view, err := workCall(t, b, "prepare", map[string]any{"receipt": v["receipt"], "query": "CSV owner"})
	if err != nil || loopObject(t, view["reasoning"])["complete"] != false {
		t.Fatal("the work context must expose the stale ending evidence", view, err)
	}
	out := runUnreviewedHook(t, "stop", map[string]any{"session_id": "reasoning", "cwd": repo, "last_assistant_message": "Checked."})
	var stopped map[string]any
	if err := json.Unmarshal([]byte(out), &stopped); err != nil {
		t.Fatal(out, err)
	}
	if (stopped["decision"] != "block" && stopped["continue"] != false) || loadSession(root, "reasoning").Judged {
		t.Fatal("a changed ending premise must require review", out)
	}
}

func TestReasoningNewEvidenceDuringCompletionCannotPublishJudgment(t *testing.T) {
	var armed atomic.Bool
	entered, resume := make(chan struct{}), make(chan struct{})
	model := fakeAgentModel(t, func(state string) float64 {
		if strings.Contains(state, "FINISH_REASONING_MARKER") && armed.CompareAndSwap(true, false) {
			close(entered)
			<-resume
		}
		return .9
	})
	repo := gitRepo(t)
	root := hookEnv(t, model, repo)
	usePlanningSpec(t, root)
	runUnreviewedHook(t, "user-prompt-submit", map[string]any{"session_id": "reasoning", "cwd": repo, "prompt": planningTestRequest})
	b := notebook.Book{Root: root, Scope: feedbackScope(repo)}
	prepared, err := workCall(t, b, "prepare", map[string]any{"session_id": "reasoning", "turn_id": loadSession(root, "reasoning").TurnID, "query": "CSV"})
	if err != nil {
		t.Fatal(err)
	}
	saveReasoning(t, b, prepared["receipt"], reasoningInput("start", "current"), nil)
	saveReasoning(t, b, prepared["receipt"], reasoningInput("finish", "current"), nil)
	snapshot := loadSession(root, "reasoning")
	type stopResult struct {
		out       hookOut
		mutations []func(*hookSession)
		err       error
	}
	done := make(chan stopResult, 1)
	armed.Store(true)
	go func() {
		out, muts, err := hookStop(root, &hookInput{SessionID: "reasoning", CWD: repo, LastAssistantMessage: "FINISH_REASONING_MARKER"}, snapshot)
		done <- stopResult{out, muts, err}
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		close(resume)
		t.Fatal("completion did not reach the model")
	}
	_, writeErr := workCall(t, b, "record", map[string]any{"receipt": prepared["receipt"], "observations": []any{map[string]any{"kind": "test", "title": "Concurrent evidence", "content": "A new condition was discovered while completion was waiting."}}})
	close(resume)
	result := <-done
	if writeErr != nil {
		t.Fatal(writeErr)
	}
	if err := updateSession(root, "reasoning", func(s *hookSession) {
		for _, mut := range result.mutations {
			mut(s)
		}
	}); err != nil {
		t.Fatal(err)
	}
	records, err := (store.Store{Dir: root}).Records("done-as-asked", 0)
	if err != nil || len(records) != 0 || loadSession(root, "reasoning").Judged || (!result.out.Block && result.out.Halt == "") {
		t.Fatal("stale reasoning published completion", len(records), result.out, result.err, err)
	}
}
