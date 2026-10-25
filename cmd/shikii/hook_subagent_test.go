package main

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/notebook"
	"github.com/hayaenfj/shikii/internal/store"
)

// startSubagent は、親が Agent ツールで子に仕事を渡し(PreToolUse の agent-task)、その子が始まる(SubagentStart)までを流す(0.9.1)
func startSubagent(t *testing.T, session, scope, agentID, agentType, prompt string) string {
	t.Helper()
	if out := runHook(t, "agent-task", map[string]any{"session_id": session, "cwd": scope, "tool_name": "Agent", "tool_input": map[string]any{"description": "d", "prompt": prompt, "subagent_type": agentType}}); out != "" {
		t.Fatalf("子への仕事を控える hook は何も出さない(ツールを止めない): %q", out)
	}
	return runHook(t, "subagent-start", map[string]any{"session_id": session, "cwd": scope, "agent_id": agentID, "agent_type": agentType})
}

func subagentContext(t *testing.T, out string) string {
	t.Helper()
	var parsed map[string]any
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("SubagentStart は JSON を返す: %q", out)
	}
	hs, _ := parsed["hookSpecificOutput"].(map[string]any)
	if hs["hookEventName"] != "SubagentStart" {
		t.Fatalf("SubagentStart の文脈で返す: %s", out)
	}
	ctx, _ := hs["additionalContext"].(string)
	return ctx
}

// サブエージェント(Claude Code の Agent ツール)の依頼文は UserPromptSubmit にも SubagentStart にも来ない。0.7.3 から SubagentStart で教訓を届ける。
// 0.9.1: 親が Agent ツールに渡した仕事の文を PreToolUse で控え、その文で選ぶ(親の依頼で選ぶと、子の仕事に関係ない教訓が届いた)。
// 親のセッションの控え(依頼・届けた記憶の版)は変えず、配送は親とは別の ID で記録する(振り返りで helpful / unused を付けられる)。親の依頼が無ければ何も出さない。
func TestSubagentStartDeliversLessonsForItsOwnTask(t *testing.T) {
	st, scope, _, draft, _ := memoryEffectFixture(t)
	if out := runHook(t, "subagent-start", map[string]any{"session_id": "sub", "cwd": scope, "agent_id": "agent-1", "agent_type": "Explore"}); out != "" {
		t.Fatalf("親の依頼がまだ無ければ何も届けない: %s", out)
	}
	// 親の依頼は README と無関係。子には README の仕事を渡す
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "sub", "cwd": scope, "prompt": "画面の色を直して、監査も頼みます"})
	before := loadSession(st.Dir, "sub")
	ctx := subagentContext(t, startSubagent(t, "sub", scope, "agent-1", "Explore", "README の導入手順を更新してください"))
	if !strings.Contains(ctx, draft.Lesson) || !strings.Contains(ctx, "頼まれた仕事") {
		t.Fatalf("子に渡した仕事の文に近い教訓を届ける: %s", ctx)
	}
	after := loadSession(st.Dir, "sub")
	if after.Request != before.Request || after.TurnID != before.TurnID || len(after.Memories) != len(before.Memories) || len(after.SubagentTasks) != 0 {
		t.Fatalf("親の控えは、使った子への仕事を外すほかは変えない: before=%+v after=%+v", before, after)
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
		t.Fatalf("子への配送を、親とは別の ID で、子の仕事の文と引用の版を付けて記録する: %+v", deliveries)
	}
	// 同じ子がもう一度始まっても落ちない(仕事を渡し直せば、同じ ID・同じ引用は再保存しない)
	if ctx2 := subagentContext(t, startSubagent(t, "sub", scope, "agent-1", "Explore", "README の導入手順を更新してください")); !strings.Contains(ctx2, draft.Lesson) {
		t.Fatalf("二度目も届ける: %s", ctx2)
	}
}

// 0.9.1: 子への仕事の控えが無い(Codex のように PreToolUse が来ない、種類が違う、古すぎる)子には、親の依頼で選ばず、
// どの依頼にも効く決まり(場面が「いつも」の rule)だけを届ける。控えは同じ種類の古い物から 1 つずつ使う
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
	if !strings.Contains(ctx, always.Lesson) || strings.Contains(ctx, draft.Lesson) || !strings.Contains(ctx, "どの依頼にも効く決まり") {
		t.Fatalf("仕事の控えが無い子には、親の依頼に近い教訓ではなく「いつも」の決まりだけ: %s", ctx)
	}
	// 別の種類の子への控えは使わない。同じ種類の控えは古い物から使う
	runHook(t, "agent-task", map[string]any{"session_id": "s4", "cwd": scope, "tool_name": "Agent", "tool_input": map[string]any{"prompt": "README の導入手順を更新してください", "subagent_type": "Plan"}})
	// WebFetch も入力に prompt を持つ。Agent / Task 以外のツールは、prompt があっても控えない
	runHook(t, "agent-task", map[string]any{"session_id": "s4", "cwd": scope, "tool_name": "WebFetch", "tool_input": map[string]any{"url": "https://example.com", "prompt": "README の導入手順を更新してください"}})
	if ctx := subagentContext(t, runHook(t, "subagent-start", map[string]any{"session_id": "s4", "cwd": scope, "agent_id": "a2", "agent_type": "Explore"})); strings.Contains(ctx, draft.Lesson) {
		t.Fatalf("別の種類(Plan)の子への仕事を Explore の子に使わない: %s", ctx)
	}
	if got := loadSession(st.Dir, "s4").SubagentTasks; len(got) != 1 || got[0].Type != "Plan" {
		t.Fatalf("Agent ツールの仕事だけを控え(WebFetch は控えない)、使っていない控えは残す: %+v", got)
	}
	if ctx := subagentContext(t, runHook(t, "subagent-start", map[string]any{"session_id": "s4", "cwd": scope, "agent_id": "a3", "agent_type": "Plan"})); !strings.Contains(ctx, draft.Lesson) {
		t.Fatalf("同じ種類の子には控えた仕事の文で選ぶ: %s", ctx)
	}
	// 種類を書かずに起こした子は general-purpose として始まる
	runHook(t, "agent-task", map[string]any{"session_id": "s4", "cwd": scope, "tool_name": "Task", "tool_input": map[string]any{"prompt": "README の導入手順を更新してください"}})
	if ctx := subagentContext(t, runHook(t, "subagent-start", map[string]any{"session_id": "s4", "cwd": scope, "agent_id": "a4", "agent_type": "general-purpose"})); !strings.Contains(ctx, draft.Lesson) {
		t.Fatalf("種類の無い控えは general-purpose の子に使う: %s", ctx)
	}
	_ = page
}

// 0.9.1: 経緯(history)の記事は自動では届けない(なぜそうなったかの話。探した時に読む)。種類の無い前の版の記事は今までどおり届ける
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
		t.Fatalf("経緯の記事は自動で届けず、守る決まりは届ける: %v %q", err, ctx)
	}
	v, err := book.Query("README の導入手順")
	found := false
	for _, p := range v.Lessons {
		found = found || p.Lesson == hist.Lesson
	}
	if err != nil || !found {
		t.Fatalf("経緯の記事も、探せば読める: %v %+v", err, v.Lessons)
	}
}

// 監査(2026-10-23): サブエージェントへの配送は親の会話に添えて振り返りで評価でき、教訓の版が変わった後の再開始でも黙らない。
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
		t.Fatalf("サブエージェントへの配送に親のターンと agent を付ける: %+v", deliveries)
	}
	// 教訓の版が変わってから同じサブエージェントがもう一度始まっても届く(以前は同じ id への別の引用として拒まれ、黙っていた)
	draft.Lesson = draft.Lesson + " 変更後も確認する。"
	if _, err := (notebook.Book{Root: st.Dir, Scope: scope}).Ingest(draft); err != nil {
		t.Fatal(err)
	}
	out := startSubagent(t, "sub2", scope, "agent-x", "Explore", "README の導入手順を更新してください")
	if !strings.Contains(out, "変更後も確認する") {
		t.Fatalf("版が変わった後の再開始にも届く: %q", out)
	}
	// 振り返り: 親の会話(返答と次の発言が揃った後)に、サブエージェントへの配送も並ぶ
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
		t.Fatalf("サブエージェントへの配送が親の会話に添えて評価に出る: %v", q["sources"])
	}
}

// 監査(2026-10-23): 既に届けた版を除いてから 2 件に切る。除く前に 2 件に切ると、届いた版が席を塞いで 3 位の教訓が届かない。
func TestRecallCutsToTwoAfterRemovingAlreadyDeliveredRevisions(t *testing.T) {
	st, scope, _, draft, _ := memoryEffectFixture(t)
	book := notebook.Book{Root: st.Dir, Scope: scope}
	var keys []string
	for i := 0; i < 2; i++ {
		d := draft
		d.ID, d.Title = "", "README の導入手順を更新してください "+string(rune('A'+i)) // 依頼そのものを題にして、元の教訓より上位に来るようにする
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
		t.Fatalf("届けた 2 件を除いた後の 3 本目が届く: %v %q", err, ctx)
	}
}

// 監査 R2: サブエージェントへの配送は、振り返りの出力にある source_id(配送 id)で評価を保存でき、観察待ちから消える。
// 親の会話(返答と次の発言)が揃っていれば、配送 id 自体が会話の id でなくてもよい。submit の会話数の数え方も親に畳む。
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
		t.Fatal("サブエージェントへの配送がある")
	}
	key := "lesson:" + draft.ID + ":" + revision
	// 評価の前: 親の会話が揃っているので、サブエージェントへの配送も「次の依頼待ち」ではなく評価待ち(監査 R3)
	if rep, err := st.MemoryReport(scope); err != nil || rep.Stats.Pending != 2 || rep.Stats.AwaitingFeedback != 0 {
		t.Fatalf("親の会話が揃えばサブエージェントへの配送も次の依頼待ちではない: %+v %v", rep.Stats, err)
	}
	// 別のセッションの配送を 1 件足しても、親とサブは同じ会話に畳まれて「最大 2 会話」に収まる(監査 R3)
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
		t.Fatalf("親とサブエージェントの配送を同じ会話として一度に評価できる(別の会話 1 件を足して 2 会話): %v", out)
	}
	rep, err := st.MemoryReport(scope)
	if err != nil || rep.Stats.Pending != 0 || rep.Stats.AwaitingFeedback != 0 || rep.Stats.Helpful != 2 || rep.Stats.Unused != 1 {
		t.Fatalf("評価が付けば観察待ちから消える: %+v %v", rep.Stats, err)
	}
}
