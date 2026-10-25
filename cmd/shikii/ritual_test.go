package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Ritual check (0.8.0): when files are changed in a request where the hook asked to sort out the plan, and the agent tries to finish without submit, Stop is blocked once.
// The Stop after a block is not blocked, but a record stays. If submit was done it does not block, and a passing record stays.
func TestStopStopsOnceWhenPlanningWasAskedButNotSubmitted(t *testing.T) {
	repo, root, _ := planningFixture(t)
	out := runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": planningTestRequest})
	if !strings.Contains(feedbackContext(t, out), "hook plan query") {
		t.Fatalf("asks to sort out the plan: %s", out)
	}
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("# changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out = runHook(t, "stop", map[string]any{"session_id": "planning", "cwd": repo, "last_assistant_message": "Done."})
	var parsed map[string]any
	if json.Unmarshal([]byte(out), &parsed) != nil || parsed["decision"] != "block" {
		t.Fatalf("a Stop that ends after changing files without sorting out the plan is blocked: %s", out)
	}
	reason, _ := parsed["reason"].(string)
	if !strings.Contains(reason, "hook plan query") || !strings.Contains(reason, `"session_id":"planning"`) || !strings.Contains(reason, "rituals are not done") {
		t.Fatalf("the reason says what to do (command and id): %s", reason)
	}
	if recs := guardRecordsFor(root, ritualPlan); len(recs) != 1 || recs[0].Pass || recs[0].Recheck {
		t.Fatalf("the block is recorded: %+v", recs)
	}
	// The Stop after a block without a fix (stop_hook_active): not blocked, but a not-fixed record stays
	out = runHook(t, "stop", map[string]any{"session_id": "planning", "cwd": repo, "last_assistant_message": "まだ", "stop_hook_active": true})
	if strings.Contains(out, "block") {
		t.Fatalf("the Stop after a block is not blocked even if not fixed: %s", out)
	}
	if recs := guardRecordsFor(root, ritualPlan); len(recs) != 2 || recs[1].Pass || !recs[1].Recheck {
		t.Fatalf("a not-fixed record stays: %+v", recs)
	}
	// Even on the Stop after a background-task notification (no stop_hook_active), the same turn is not blocked again. The ask count stays 1
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": "<task-notification>裏の作業が終わりました</task-notification>"})
	out = runHook(t, "stop", map[string]any{"session_id": "planning", "cwd": repo, "last_assistant_message": "通知を受けた"})
	if strings.Contains(out, "block") {
		t.Fatalf("the Stop after a notification does not block the same turn twice: %s", out)
	}
	if st := countRitualStats(readGuardRecords(root)); st.Plan.Asked != 1 {
		t.Fatalf("the same turn is counted only once: %+v", st.Plan)
	}
	// Right after being blocked, the sorted plan can be submitted (accepted while it is still missing, even if the turn's decision is done)
	if _, err := planningCall(t, "query", map[string]string{"session_id": "planning", "turn_id": loadSession(root, "planning").TurnID}); err != nil {
		t.Fatalf("query after the block: %v", err)
	}
	if _, err := planningCall(t, "submit", planningPayload(root)); err != nil {
		t.Fatalf("submit after the block: %v", err)
	}
	out = runHook(t, "stop", map[string]any{"session_id": "planning", "cwd": repo, "last_assistant_message": "整理した", "stop_hook_active": true})
	if strings.Contains(out, "block") {
		t.Fatalf("the second Stop is not blocked: %s", out)
	}
	if recs := guardRecordsFor(root, ritualPlan); len(recs) < 3 || !recs[len(recs)-1].Pass || !recs[len(recs)-1].Recheck {
		t.Fatalf("a record of doing it after the block (fixed) stays: %+v", recs)
	}
	// Next request: if the plan is submitted and then files are changed, finishing is not blocked
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": planningTestRequest + " (again)"})
	if _, err := planningCall(t, "submit", planningPayload(root)); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("# changed again\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out = runHook(t, "stop", map[string]any{"session_id": "planning", "cwd": repo, "last_assistant_message": "Done again."})
	if strings.Contains(out, "block") {
		t.Fatalf("a request whose plan was submitted is not blocked: %s", out)
	}
	if recs := guardRecordsFor(root, ritualPlan); !recs[len(recs)-1].Pass || recs[len(recs)-1].Recheck {
		t.Fatalf("a passing record stays: %+v", recs)
	}
	if sOut := capture(t, func() error { return cmdStatus([]string{"--store", root}) }); !strings.Contains(sOut, "rituals: plan-before-work done 1 of 2 asked (after a stop 1)") {
		t.Fatalf("status shows how often the ritual was done (1 done after a block):\n%s", sOut)
	}
	// A request that changes no files (an explanation etc.) is not blocked even without a sorted plan
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": "explain the parser"})
	if out = runHook(t, "stop", map[string]any{"session_id": "planning", "cwd": repo, "last_assistant_message": "It parses."}); strings.Contains(out, "plan has not been sorted out") {
		t.Fatalf("a request that changes no files is not blocked for the plan (the reflection check is separate): %s", out)
	}
	// Built-in checks appear in the list and cannot be removed
	list := capture(t, func() error { t.Chdir(repo); return cmdGuard([]string{"list"}) })
	if !strings.Contains(list, ritualPlan) || !strings.Contains(list, ritualReflect) {
		t.Fatalf("the built-in ritual checks appear in the list:\n%s", list)
	}
	if err := cmdGuard([]string{"disable", ritualPlan}); err == nil || !strings.Contains(err.Error(), "built-in") {
		t.Fatalf("built-in checks cannot be removed: %v", err)
	}
}

// Ritual check (0.8.0): when conversations to reflect on were listed before the request and the agent tries to finish without saving, Stop is blocked once. Saving lets it pass.
func TestStopStopsOnceWhenReflectionWasAskedButNotSaved(t *testing.T) {
	model := fakeAgentModel(t, func(string) float64 { return .1 })
	repo := gitRepo(t)
	root := hookEnv(t, model, repo)
	scope := feedbackScope(repo)
	// Finish 2 conversations first (the request to reflect appears when 2 or more are unsorted)
	for i, req := range []string{"README の導入手順を更新してください", "画面の色を直してください"} {
		sid := []string{"a", "b"}[i]
		runHook(t, "user-prompt-submit", map[string]any{"session_id": sid, "cwd": repo, "prompt": req})
		runHook(t, "stop", map[string]any{"session_id": sid, "cwd": repo, "last_assistant_message": "やりました。"})
		runHook(t, "user-prompt-submit", map[string]any{"session_id": sid, "cwd": repo, "prompt": "ありがとう、次は別の件です。"})
	}
	out := runHook(t, "user-prompt-submit", map[string]any{"session_id": "c", "cwd": repo, "prompt": "ガイドの quick start を直して"})
	ctx := feedbackContext(t, out)
	if !strings.Contains(ctx, "wrap up the reflection") {
		t.Fatalf("with 2 unsorted conversations it asks for reflection: %s", ctx)
	}
	asked := loadSession(root, "c").ReflectAsked
	if len(asked) == 0 {
		t.Fatal("the asked conversations are noted")
	}
	out = runHook(t, "stop", map[string]any{"session_id": "c", "cwd": repo, "last_assistant_message": "直しました。"})
	var parsed map[string]any
	if json.Unmarshal([]byte(out), &parsed) != nil || parsed["decision"] != "block" {
		t.Fatalf("a Stop that ends without saving the reflection is blocked: %s", out)
	}
	reason, _ := parsed["reason"].(string)
	if !strings.Contains(reason, "reflect query") || !strings.Contains(reason, asked[0]) || !strings.Contains(reason, "reflection has not been saved") {
		t.Fatalf("the reason names the target conversations and the steps: %s", reason)
	}
	if recs := guardRecordsFor(root, ritualReflect); len(recs) != 1 || recs[0].Pass {
		t.Fatalf("the block is recorded: %+v", recs)
	}
	// Save the reflection (provisional scores and the wiki sources)
	h := newServer(serveOpts{storeDir: root, projectScope: scope})
	q := reflectCall(t, h, map[string]any{"operation": "query"}) // the tool's query picks its own targets (the same conversations the hook listed)
	var reviews []map[string]any
	var wiki []map[string]any
	for _, s := range q["sources"].([]any) {
		src := s.(map[string]any)
		sm := src["source_material"].(map[string]any)
		id := sm["id"].(string)
		for _, tg := range src["targets"].([]any) {
			target := tg.(map[string]any)
			reviews = append(reviews, map[string]any{"source_id": id, "trace_id": target["trace_id"], "truth": "yes", "reason": "次の依頼にやり直しの指示が無い"})
		}
		wiki = append(wiki, map[string]any{"sources": []string{id}})
	}
	res := reflectCall(t, h, map[string]any{"operation": "submit", "reviews": reviews, "wiki": wiki})
	if res["complete"] != true {
		t.Fatalf("saving the reflection: %v", res)
	}
	out = runHook(t, "stop", map[string]any{"session_id": "c", "cwd": repo, "last_assistant_message": "直しました。", "stop_hook_active": true})
	if strings.Contains(out, "block") {
		t.Fatalf("the second Stop is not blocked: %s", out)
	}
	if recs := guardRecordsFor(root, ritualReflect); len(recs) != 2 || !recs[1].Pass || !recs[1].Recheck {
		t.Fatalf("the recheck after saving passes (a fixed record): %+v", recs)
	}
}

// audit R5: even in a turn a person's check (guard) stopped, the ritual check is evaluated, recorded, and its reason is appended (so it does not vanish uncounted).
func TestRitualIsRecordedEvenWhenAGuardStopsTheTurnFirst(t *testing.T) {
	repo, root, _ := planningFixture(t)
	writeGuard(t, root, guard{ID: "no-ne", Kind: guardKindForbiddenWords, Title: "根を使わない", Words: []string{"根"}, Enabled: true})
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": planningTestRequest})
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("# shikii\n\nプロジェクトの根に置く\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := runHook(t, "stop", map[string]any{"session_id": "planning", "cwd": repo, "last_assistant_message": "Done."})
	var parsed map[string]any
	if json.Unmarshal([]byte(out), &parsed) != nil || parsed["decision"] != "block" {
		t.Fatalf("stopped by the guard: %s", out)
	}
	reason, _ := parsed["reason"].(string)
	if !strings.Contains(reason, "no-ne") || !strings.Contains(reason, "plan has not been sorted out") {
		t.Fatalf("the reason has both the person's check and the ritual: %s", reason)
	}
	if recs := guardRecordsFor(root, ritualPlan); len(recs) != 1 || recs[0].Pass || recs[0].Recheck {
		t.Fatalf("the ritual record stays too: %+v", recs)
	}
	if st := countRitualStats(readGuardRecords(root)); st.Plan.Asked != 1 {
		t.Fatalf("counted as asked: %+v", st.Plan)
	}
}

// audit R5-3: a turn that did not ask for a plan (no Planning) is not blocked. A turn that did not ask for reflection is not blocked by leftovers from before.
func TestRitualsOnlyApplyToTheTurnThatAskedThem(t *testing.T) {
	model := fakeAgentModel(t, func(string) float64 { return .1 })
	repo := gitRepo(t)
	root := hookEnv(t, model, repo) // needs-plan stays in the old format → no plan context is asked for
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "n", "cwd": repo, "prompt": "add a flag to the status command"})
	if s := loadSession(root, "n"); s.Planning != nil {
		t.Skip("this setup asks for a plan")
	}
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("# changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if out := runHook(t, "stop", map[string]any{"session_id": "n", "cwd": repo, "last_assistant_message": "Done."}); strings.Contains(out, "plan has not been sorted out") {
		t.Fatalf("a turn that did not ask is not blocked: %s", out)
	}
	if recs := guardRecordsFor(root, ritualPlan); len(recs) != 0 {
		t.Fatalf("a turn that did not ask is not counted: %+v", recs)
	}
	// Reflection: even if conversations asked about in an earlier turn (still unsorted) remain in the notes, a turn that did not ask is not blocked
	for i, req := range []string{"README の導入手順を更新してください", "画面の色を直してください"} {
		sid := []string{"ra", "rb"}[i]
		runHook(t, "user-prompt-submit", map[string]any{"session_id": sid, "cwd": repo, "prompt": req})
		runHook(t, "stop", map[string]any{"session_id": sid, "cwd": repo, "last_assistant_message": "やりました。"})
		runHook(t, "user-prompt-submit", map[string]any{"session_id": sid, "cwd": repo, "prompt": "ありがとう、次は別の件です。"})
	}
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "n", "cwd": repo, "prompt": "ガイドの quick start を直して"})
	asked := loadSession(root, "n").ReflectAsked
	if len(asked) == 0 {
		t.Fatal("asks when 2 are unsorted")
	}
	updateSession(root, "n", func(s *hookSession) { s.ReflectAskedTurn = "old-turn" }) // make it a leftover from an earlier turn
	if out := runHook(t, "stop", map[string]any{"session_id": "n", "cwd": repo, "last_assistant_message": "直した"}); strings.Contains(out, "reflection has not been saved") {
		t.Fatalf("not blocked by leftovers from another turn: %s", out)
	}
}

// 0.8.1 (found by measuring in a project without work rules): so it arrives even without CLAUDE.md / AGENTS.md, in a turn where the hook says something,
// how to keep work records and rules is added once per session. When a ritual check blocks, it says to restate the result of the original work in the final reply.
func TestHooksCarryTheWorkRecordHabitsWithoutProjectInstructions(t *testing.T) {
	repo, _, _ := planningFixture(t)
	out := runHook(t, "user-prompt-submit", map[string]any{"session_id": "habits", "cwd": repo, "prompt": planningTestRequest})
	ctx := feedbackContext(t, out)
	if !strings.Contains(ctx, ".shikii/handoff/") || !strings.Contains(ctx, "wiki の教訓") {
		t.Fatalf("the first turn that says something adds how to keep work records and rules: %s", ctx)
	}
	out = runHook(t, "user-prompt-submit", map[string]any{"session_id": "habits", "cwd": repo, "prompt": planningTestRequest + " もう一度"})
	if strings.Contains(feedbackContext(t, out), ".shikii/handoff/<仕事>/") {
		t.Fatalf("not added a second time in the same session: %s", out)
	}
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("# changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out = runHook(t, "stop", map[string]any{"session_id": "habits", "cwd": repo, "last_assistant_message": "Done."})
	if !strings.Contains(out, "result of the work the person asked for") {
		t.Fatalf("when blocking, it says to give the result of the original work in the reply after doing the rituals: %s", out)
	}
	// Subagents: even with no lesson to deliver, where to write reports is delivered
	sub := runHook(t, "subagent-start", map[string]any{"session_id": "habits", "cwd": repo, "agent_id": "a1", "agent_type": "general-purpose"})
	if !strings.Contains(sub, ".shikii/handoff/") {
		t.Fatalf("subagents also get where to write reports: %s", sub)
	}
}
