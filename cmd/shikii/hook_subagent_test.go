package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/notebook"
	"github.com/hayaenfj/shikii/internal/store"
)

// startSubagent runs from the parent passing a task to a child with the Agent tool (PreToolUse agent-task) to that child starting (SubagentStart) (0.9.1)
func startSubagent(t *testing.T, session, scope, agentID, agentType, prompt string) string {
	t.Helper()
	if out := runHook(t, "agent-task", map[string]any{"session_id": session, "cwd": scope, "tool_name": "Agent", "tool_input": map[string]any{"description": "d", "prompt": prompt, "subagent_type": agentType}}); out != "" {
		t.Fatalf("the hook that keeps the child's task outputs nothing (does not stop the tool): %q", out)
	}
	return runHook(t, "subagent-start", map[string]any{"session_id": session, "cwd": scope, "agent_id": agentID, "agent_type": agentType})
}

func subagentContext(t *testing.T, out string) string {
	t.Helper()
	var parsed map[string]any
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("SubagentStart returns JSON: %q", out)
	}
	hs, _ := parsed["hookSpecificOutput"].(map[string]any)
	if hs["hookEventName"] != "SubagentStart" {
		t.Fatalf("returns it in the SubagentStart context: %s", out)
	}
	ctx, _ := hs["additionalContext"].(string)
	return ctx
}

// A subagent's (Claude Code's Agent tool) task text arrives in neither UserPromptSubmit nor SubagentStart. Since 0.7.3 lessons are delivered at SubagentStart.
// 0.9.1: the task text the parent passed to the Agent tool is kept at PreToolUse and lessons are picked with it (picking by the parent's request delivered lessons unrelated to the child's work).
// The parent session's record (request, delivered memory versions) is not changed, and the delivery is recorded under an ID separate from the parent's (so reflection can mark it helpful / unused). Nothing is output without a parent request.
func TestSubagentStartDeliversLessonsForItsOwnTask(t *testing.T) {
	st, scope, _, draft, _ := memoryEffectFixture(t)
	if out := runHook(t, "subagent-start", map[string]any{"session_id": "sub", "cwd": scope, "agent_id": "agent-1", "agent_type": "Explore"}); out != "" {
		t.Fatalf("delivers nothing if there is no parent request yet: %s", out)
	}
	// the parent's request is unrelated to README. The child is given the README work
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "sub", "cwd": scope, "prompt": "画面の色を直して、監査も頼みます"})
	before := loadSession(st.Dir, "sub")
	ctx := subagentContext(t, startSubagent(t, "sub", scope, "agent-1", "Explore", "README の導入手順を更新してください"))
	if !strings.Contains(ctx, draft.Lesson) || !strings.Contains(ctx, "close to its task") {
		t.Fatalf("delivers lessons close to the task text given to the child: %s", ctx)
	}
	after := loadSession(st.Dir, "sub")
	if after.Request != before.Request || after.TurnID != before.TurnID || len(after.Memories) != len(before.Memories) || len(after.SubagentTasks) != 0 {
		t.Fatalf("the parent's record is unchanged except for dropping the used child task: before=%+v after=%+v", before, after)
	}
	deliveries, err := st.MemoryDeliveries(scope)
	if err != nil {
		t.Fatal(err)
	}
	var sub store.MemoryDelivery
	for _, d := range deliveries {
		if d.Parent == before.TurnID {
			sub = d
		}
	}
	if sub.Agent != "agent-1" || sub.Request != "README の導入手順を更新してください" || len(sub.Quotes) == 0 || sub.Quotes[0].ID != draft.ID {
		t.Fatalf("records the delivery to the child under an ID separate from the parent's, with the child's task text and the quoted versions: %+v", deliveries)
	}
	// the same child starting again does not fail (if the task is passed again, the same ID and the same quotes are not saved again)
	if ctx2 := subagentContext(t, startSubagent(t, "sub", scope, "agent-1", "Explore", "README の導入手順を更新してください")); !strings.Contains(ctx2, draft.Lesson) {
		t.Fatalf("delivers the second time too: %s", ctx2)
	}
}

// 0.9.1: a child with no kept task (no PreToolUse, as in Codex; a different type; too old) does not get lessons picked by the parent's request,
// only the rules that apply to every request (rules with the scene "いつも"). Kept tasks of the same type are used one at a time, oldest first
func TestSubagentWithoutItsTaskGetsOnlyAlwaysRules(t *testing.T) {
	st, scope, _, draft, _ := memoryEffectFixture(t)
	always := draft
	always.ID, always.Title, always.Lesson, always.Kind, always.Scene, always.AppliesTo = "", "見える文は日本語", "報告も含めて見える文は日本語で書く。", "rule", "いつも", "どの依頼でも"
	page, err := (notebook.Book{Root: st.Dir, Scope: scope}).Ingest(always)
	if err != nil {
		t.Fatal(err)
	}
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "s4", "cwd": scope, "prompt": "README の導入手順を更新してください"})
	ctx := subagentContext(t, runHook(t, "subagent-start", map[string]any{"session_id": "s4", "cwd": scope, "agent_id": "a1", "agent_type": "Explore"}))
	if !strings.Contains(ctx, always.Lesson) || strings.Contains(ctx, draft.Lesson) || !strings.Contains(ctx, "the rules that apply to every request") {
		t.Fatalf("a child with no kept task gets only the \"いつも\" rules, not lessons close to the parent's request: %s", ctx)
	}
	// a kept task for a child of another type is not used. Kept tasks of the same type are used oldest first
	runHook(t, "agent-task", map[string]any{"session_id": "s4", "cwd": scope, "tool_name": "Agent", "tool_input": map[string]any{"prompt": "README の導入手順を更新してください", "subagent_type": "Plan"}})
	// WebFetch also has a prompt in its input. Tools other than Agent / Task are not kept even with a prompt
	runHook(t, "agent-task", map[string]any{"session_id": "s4", "cwd": scope, "tool_name": "WebFetch", "tool_input": map[string]any{"url": "https://example.com", "prompt": "README の導入手順を更新してください"}})
	if ctx := subagentContext(t, runHook(t, "subagent-start", map[string]any{"session_id": "s4", "cwd": scope, "agent_id": "a2", "agent_type": "Explore"})); strings.Contains(ctx, draft.Lesson) {
		t.Fatalf("a task for a child of another type (Plan) is not used for an Explore child: %s", ctx)
	}
	if got := loadSession(st.Dir, "s4").SubagentTasks; len(got) != 1 || got[0].Type != "Plan" {
		t.Fatalf("only Agent tool tasks are kept (not WebFetch), and unused records stay: %+v", got)
	}
	if ctx := subagentContext(t, runHook(t, "subagent-start", map[string]any{"session_id": "s4", "cwd": scope, "agent_id": "a3", "agent_type": "Plan"})); !strings.Contains(ctx, draft.Lesson) {
		t.Fatalf("a child of the same type is picked with the kept task text: %s", ctx)
	}
	// a child started without a type starts as general-purpose
	runHook(t, "agent-task", map[string]any{"session_id": "s4", "cwd": scope, "tool_name": "Task", "tool_input": map[string]any{"prompt": "README の導入手順を更新してください"}})
	if ctx := subagentContext(t, runHook(t, "subagent-start", map[string]any{"session_id": "s4", "cwd": scope, "agent_id": "a4", "agent_type": "general-purpose"})); !strings.Contains(ctx, draft.Lesson) {
		t.Fatalf("a record without a type is used for a general-purpose child: %s", ctx)
	}
	_ = page
}

// 0.9.1: history pages are not delivered automatically (they tell why things came to be; read them when searching). Pages from the old version without a kind are delivered as before
func TestHistoryLessonsAreNotDeliveredAutomatically(t *testing.T) {
	st, scope, _, draft, _ := memoryEffectFixture(t)
	book := notebook.Book{Root: st.Dir, Scope: scope}
	hist := draft
	hist.ID, hist.Title, hist.Lesson, hist.Kind, hist.Scene = "", "README の導入手順を変えた経緯", "README の導入手順は 2026-10 に書き直した経緯がある。", "history", "README を直す時"
	if _, err := book.Ingest(hist); err != nil {
		t.Fatal(err)
	}
	ctx, err := memoryContext(st.Dir, scope, "README の導入手順を更新してください", &hookSession{TurnID: "bbbbbbbbbbbbbbbb"})
	if err != nil || !strings.Contains(ctx, draft.Lesson) || strings.Contains(ctx, hist.Lesson) {
		t.Fatalf("history pages are not delivered automatically, while rules decided in this project are: %v %q", err, ctx)
	}
	v, err := book.Query("README の導入手順")
	found := false
	for _, p := range v.Lessons {
		found = found || p.Lesson == hist.Lesson
	}
	if err != nil || !found {
		t.Fatalf("history pages can still be read by searching: %v %+v", err, v.Lessons)
	}
}

// Audit (2026-10-23): deliveries to subagents can be assessed in reflection alongside the parent's conversation, and are not silent on a restart after the lesson's version changed.
func TestSubagentDeliveriesAreAssessedWithTheParentAndSurviveRevisions(t *testing.T) {
	st, scope, h, draft, _ := memoryEffectFixture(t)
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "sub2", "cwd": scope, "prompt": "README の導入手順を更新してください"})
	parent := loadSession(st.Dir, "sub2").TurnID
	startSubagent(t, "sub2", scope, "agent-x", "Explore", "README の導入手順を更新してください")
	deliveries, _ := st.MemoryDeliveries(scope)
	var sub store.MemoryDelivery
	for _, d := range deliveries {
		if d.Parent != "" {
			sub = d
		}
	}
	if sub.Parent != parent || sub.Agent != "agent-x" {
		t.Fatalf("a delivery to a subagent carries the parent's turn and the agent: %+v", deliveries)
	}
	// after the lesson's version changed, it is delivered even when the same subagent starts again (it used to be refused as another quote for the same id, and stayed silent)
	draft.Lesson = draft.Lesson + " 変更後も確認する。"
	if _, err := (notebook.Book{Root: st.Dir, Scope: scope}).Ingest(draft); err != nil {
		t.Fatal(err)
	}
	out := startSubagent(t, "sub2", scope, "agent-x", "Explore", "README の導入手順を更新してください")
	if !strings.Contains(out, "変更後も確認する") {
		t.Fatalf("delivered on a restart after the version changed: %q", out)
	}
	// reflection: the parent's conversation (once the reply and the next message are there) also lists the delivery to the subagent
	runHook(t, "stop", map[string]any{"session_id": "sub2", "cwd": scope, "last_assistant_message": "更新しました。"})
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "sub2", "cwd": scope, "prompt": "次は画面の色を直して。"})
	q := reflectCall(t, h, map[string]any{"operation": "query"})
	sources, _ := q["sources"].([]any)
	found := 0
	for _, s := range sources {
		src := s.(map[string]any)
		if src["source_material"].(map[string]any)["id"] != parent {
			continue
		}
		for _, m := range src["memories"].([]any) {
			if m.(map[string]any)["parent"] == parent {
				found++
			}
		}
	}
	if found < 1 {
		t.Fatalf("the delivery to the subagent shows up for assessment alongside the parent's conversation: %v", q["sources"])
	}
}

// Audit (2026-10-23): cut to 2 after removing already delivered versions. Cutting to 2 before removing lets delivered versions take the seats so the 3rd lesson never arrives.
func TestRecallCutsToTwoAfterRemovingAlreadyDeliveredRevisions(t *testing.T) {
	st, scope, _, draft, _ := memoryEffectFixture(t)
	book := notebook.Book{Root: st.Dir, Scope: scope}
	var keys []string
	for i := 0; i < 2; i++ {
		d := draft
		d.ID, d.Title = "", "README の導入手順を更新してください "+string(rune('A'+i)) // make the request itself the title so it ranks above the original lesson
		d.Lesson = "README の導入手順では確認の手順 " + string(rune('A'+i)) + " も書く。"
		d.AppliesTo = "README の導入手順を更新してください"
		r, err := book.Ingest(d)
		if err != nil {
			t.Fatal(err)
		}
		keys = append(keys, "lesson:"+r.Page.ID+":"+r.Page.Revision)
	}
	sess := &hookSession{TurnID: "aaaaaaaaaaaaaaaa"}
	for _, k := range keys {
		sess.Memories = append(sess.Memories, memoryDelivery{Key: k, At: time.Now().UTC()})
	}
	ctx, err := memoryContext(st.Dir, scope, "README の導入手順を更新してください", sess)
	if err != nil || !strings.Contains(ctx, draft.Lesson) {
		t.Fatalf("after removing the 2 delivered ones, the 3rd arrives: %v %q", err, ctx)
	}
}

// Audit R2: a delivery to a subagent can be assessed by the source_id (delivery id) in the reflection output and leaves the observation queue.
// If the parent's conversation (reply and next message) is complete, the delivery id need not be a conversation id. Submit's conversation count also folds into the parent.
func TestSubagentDeliveryCanBeAssessedByItsOwnSourceID(t *testing.T) {
	st, scope, h, draft, revision := memoryEffectFixture(t)
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "sub3", "cwd": scope, "prompt": "README の導入手順を更新してください"})
	parent := loadSession(st.Dir, "sub3").TurnID
	startSubagent(t, "sub3", scope, "agent-y", "Explore", "README の導入手順を更新してください")
	runHook(t, "stop", map[string]any{"session_id": "sub3", "cwd": scope, "last_assistant_message": "更新しました。"})
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "sub3", "cwd": scope, "prompt": "次は画面の色を直して。"})
	deliveries, _ := st.MemoryDeliveries(scope)
	var subID string
	for _, d := range deliveries {
		if d.Parent == parent {
			subID = d.ID
		}
	}
	if subID == "" {
		t.Fatal("there is a delivery to the subagent")
	}
	key := "lesson:" + draft.ID + ":" + revision
	// before assessment: the parent's conversation is complete, so the delivery to the subagent is waiting for assessment, not for the "next request" (audit R3)
	if rep, err := st.MemoryReport(scope); err != nil || rep.Stats.Pending != 2 || rep.Stats.AwaitingFeedback != 0 {
		t.Fatalf("once the parent's conversation is complete, the delivery to the subagent is not waiting for the next request either: %+v %v", rep.Stats, err)
	}
	// even with one more delivery from another session, the parent's and the subagent's deliveries can be assessed at once (up to 0.9.0 this checked that parent and subagent fold into "at most 2 conversations". audit R3)
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "other", "cwd": scope, "prompt": "README の導入手順を更新してください"})
	other := loadSession(st.Dir, "other").TurnID
	runHook(t, "stop", map[string]any{"session_id": "other", "cwd": scope, "last_assistant_message": "更新しました。"})
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "other", "cwd": scope, "prompt": "次は画面の色を直して。"})
	out := reflectCall(t, h, map[string]any{"operation": "submit", "memory": []map[string]any{
		{"source_id": parent, "key": key, "result": "helpful", "decision": "手順に確認を足した", "evidence": "返答に確認の手順が入った"},
		{"source_id": subID, "key": key, "result": "unused", "decision": "サブエージェントは手順を読まなかった", "evidence": "返答に手順への言及が無い"},
		{"source_id": other, "key": key, "result": "helpful", "decision": "別のセッションでも手順に確認を足した", "evidence": "返答に確認の手順が入った"},
	}})
	if out["complete"] != true {
		t.Fatalf("the parent's and the subagent's deliveries can be assessed at once as the same conversation (2 conversations with one other added): %v", out)
	}
	rep, err := st.MemoryReport(scope)
	if err != nil || rep.Stats.Pending != 0 || rep.Stats.AwaitingFeedback != 0 || rep.Stats.Helpful != 2 || rep.Stats.Unused != 1 {
		t.Fatalf("once assessed they leave the observation queue: %+v %v", rep.Stats, err)
	}
}

// Audit 0.9.1 (test gaps GM2-4, 14): kept child tasks are used oldest first within the same type, and ones older than 10 minutes are not used.
// When adding, expired ones are dropped and at most 8 are kept
func TestSubagentTasksAreOldestFirstWithinLimits(t *testing.T) {
	now := time.Now()
	tasks := []subagentTask{
		{Type: "Explore", Prompt: "古すぎる", At: now.Add(-11 * time.Minute)},
		{Type: "Explore", Prompt: "古い", At: now.Add(-2 * time.Second)},
		{Type: "Explore", Prompt: "新しい", At: now.Add(-1 * time.Second)},
	}
	if got, ok := pickSubagentTask(tasks, "Explore", now); !ok || got.Prompt != "古い" {
		t.Fatalf("uses the oldest record within the time limit (not ones older than 10 minutes): %+v %v", got, ok)
	}
	add := func(s *hookSession, prompt string) {
		in := &hookInput{ToolName: "Agent", ToolInput: json.RawMessage(`{"prompt":"` + prompt + `","subagent_type":"Plan"}`)}
		for _, m := range hookAgentTask(in) {
			m(s)
		}
	}
	// when adding, expired ones (even of another type) are dropped
	expired := &hookSession{SubagentTasks: []subagentTask{{Type: "Explore", Prompt: "期限切れ", At: now.Add(-11 * time.Minute)}}}
	add(expired, "仕事 X")
	if len(expired.SubagentTasks) != 1 || expired.SubagentTasks[0].Prompt != "仕事 X" {
		t.Fatalf("drops expired records when adding: %+v", expired.SubagentTasks)
	}
	// dropping records older than 30 seconds applies only to the same type (records of other types stay. re-audit GB)
	other := &hookSession{SubagentTasks: []subagentTask{{Type: "Explore", Prompt: "1 分前の Explore", At: now.Add(-time.Minute)}}}
	add(other, "仕事 Y")
	if len(other.SubagentTasks) != 2 || other.SubagentTasks[0].Prompt != "1 分前の Explore" {
		t.Fatalf("records of another type are not dropped by the 30-second rule: %+v", other.SubagentTasks)
	}
	s := &hookSession{SubagentTasks: []subagentTask{{Type: "Plan", Prompt: "期限切れ", At: now.Add(-11 * time.Minute)}}}
	for i := 0; i < 10; i++ {
		in := &hookInput{ToolName: "Agent", ToolInput: json.RawMessage(`{"prompt":"仕事 ` + string(rune('A'+i)) + `","subagent_type":"Plan"}`)}
		for _, m := range hookAgentTask(in) {
			m(s)
		}
	}
	if len(s.SubagentTasks) != subagentTaskMax || s.SubagentTasks[0].Prompt != "仕事 C" || s.SubagentTasks[len(s.SubagentTasks)-1].Prompt != "仕事 J" {
		t.Fatalf("drops expired ones when adding and keeps only the newest 8: %+v", s.SubagentTasks)
	}
}

// Audit 0.9.1 (test gaps GM7, 8, 10, 11): the "いつも" rules are only rules whose scene is "いつも" (English always, case-insensitive),
// and replaced pages are not returned. A child without a kept task is not given the original text of unorganized conversations either
func TestAlwaysRulesAreOnlyLiveAlwaysRules(t *testing.T) {
	st, scope, _, draft, _ := memoryEffectFixture(t)
	book := notebook.Book{Root: st.Dir, Scope: scope}
	mk := func(title, kind, scene string, replaces ...string) *notebook.Page {
		d := draft
		d.ID, d.Title, d.Lesson, d.Kind, d.Scene, d.Replaces = "", title, title+"の教訓。", kind, scene, replaces
		r, err := book.Ingest(d)
		if err != nil {
			t.Fatal(err)
		}
		return r.Page
	}
	old := mk("古い決まり", "rule", "いつも")
	mk("新しい決まり", "rule", "いつも", old.ID)
	mk("英語の決まり", "rule", "ALWAYS")
	mk("いつものやり方", "howto", "いつも")
	// a page with broken sources (the source was removed) is not returned (re-audit GI)
	obs, err := book.Capture(notebook.Observation{Kind: "human", Title: "消える原資料", Content: "いつもの決まりの元", Reference: "r", Version: "v1", ObservedAt: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	broken := draft
	broken.ID, broken.Sources, broken.Title, broken.Lesson, broken.Kind, broken.Scene = "", []string{obs.ID}, "出典の壊れた決まり", "出典の壊れた決まりの教訓。", "rule", "いつも"
	if _, err := book.Ingest(broken); err != nil {
		t.Fatal(err)
	}
	files, _ := filepath.Glob(filepath.Join(st.Dir, "records", "wiki-sources", "*", obs.ID+"*"))
	if len(files) == 0 {
		files, _ = filepath.Glob(filepath.Join(st.Dir, "records", "wiki-sources", obs.ID+"*"))
	}
	if len(files) == 0 {
		t.Fatal("precondition of this check: the source file is found")
	}
	for _, f := range files {
		if err := os.Remove(f); err != nil {
			t.Fatal(err)
		}
	}
	got, err := book.AlwaysRules()
	titles := []string{}
	for _, p := range got {
		titles = append(titles, p.Title)
	}
	if err != nil || len(got) != 2 || !strings.Contains(strings.Join(titles, ","), "新しい決まり") || !strings.Contains(strings.Join(titles, ","), "英語の決まり") {
		t.Fatalf("returns only the \"いつも\" rules, without replaced pages: %v %v", err, titles)
	}
	// without "いつも" rules, the original text of unorganized conversations is not returned either, and nothing is delivered
	st2, scope2, _ := reviewFixture(t) // a base without lessons
	reviewSeed(t, st2, scope2, 2)      // conversations not organized yet, close to the request
	if ctx, err := memoryContextFor(st2.Dir, scope2, "README の起動手順を日本語にする 0000000000000002", &hookSession{TurnID: "dddddddddddddddd"}, "", ""); err != nil || !strings.Contains(ctx, "0000000000000002") {
		t.Fatalf("precondition of this check: picking by closeness to the request returns the original text of unorganized conversations: %v %q", err, ctx)
	}
	if ctx, err := alwaysContextFor(st2.Dir, scope2, "README の起動手順を日本語にする 0000000000000002", &hookSession{TurnID: "cccccccccccccccc"}, "p", "a"); err != nil || ctx != "" {
		t.Fatalf("a child without a kept task is not given the original text of unorganized conversations: %v %q", err, ctx)
	}
}
