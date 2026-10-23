package main

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/notebook"
	"github.com/hayaenfj/shikii/internal/store"
)

// サブエージェント(Claude Code の Agent ツール)の依頼文は UserPromptSubmit に来ないので、0.7.2 までは教訓が一切届かなかった。
// 0.7.3: SubagentStart で、親の依頼に近い教訓を同じ形で届ける。親のセッションの控え(依頼・届けた記憶の版)は変えず、
// 配送は親とは別の ID で記録する(振り返りで helpful / unused を付けられる)。親の依頼が無ければ何も出さない。
func TestSubagentStartDeliversTheParentRequestsLessons(t *testing.T) {
	st, scope, _, draft, _ := memoryEffectFixture(t)
	if out := runHook(t, "subagent-start", map[string]any{"session_id": "sub", "cwd": scope, "agent_id": "agent-1", "agent_type": "Explore"}); out != "" {
		t.Fatalf("親の依頼がまだ無ければ何も届けない: %s", out)
	}
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "sub", "cwd": scope, "prompt": "README の導入手順を更新してください"})
	before := loadSession(st.Dir, "sub")
	out := runHook(t, "subagent-start", map[string]any{"session_id": "sub", "cwd": scope, "agent_id": "agent-1", "agent_type": "Explore"})
	var parsed map[string]any
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("SubagentStart は JSON を返す: %q", out)
	}
	hs, _ := parsed["hookSpecificOutput"].(map[string]any)
	ctx, _ := hs["additionalContext"].(string)
	if hs["hookEventName"] != "SubagentStart" || !strings.Contains(ctx, draft.Lesson) || !strings.Contains(ctx, "サブエージェント") {
		t.Fatalf("親の依頼に近い教訓を SubagentStart の文脈で届ける: %s", out)
	}
	after := loadSession(st.Dir, "sub")
	if after.Request != before.Request || after.TurnID != before.TurnID || len(after.Memories) != len(before.Memories) {
		t.Fatalf("親の控えは変えない: before=%+v after=%+v", before, after)
	}
	deliveries, err := st.MemoryDeliveries(scope)
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]bool{}
	for _, d := range deliveries {
		ids[d.ID] = true
	}
	if len(deliveries) != 2 || len(ids) != 2 || !ids[before.TurnID] {
		t.Fatalf("親への配送とサブエージェントへの配送を別の ID で記録する: %+v", deliveries)
	}
	// 同じサブエージェントがもう一度始まっても落ちない(同じ ID・同じ引用は再保存しない)
	if out2 := runHook(t, "subagent-start", map[string]any{"session_id": "sub", "cwd": scope, "agent_id": "agent-1", "agent_type": "Explore"}); !strings.Contains(out2, draft.Lesson) {
		t.Fatalf("二度目も届ける: %s", out2)
	}
	var sub store.MemoryDelivery
	for _, d := range deliveries {
		if d.ID != before.TurnID {
			sub = d
		}
	}
	if len(sub.Quotes) == 0 || sub.Quotes[0].ID != draft.ID {
		t.Fatalf("サブエージェントへの配送にも引用の版が残る: %+v", sub)
	}
}

// 監査(2026-10-23): サブエージェントへの配送は親の会話に添えて振り返りで評価でき、教訓の版が変わった後の再開始でも黙らない。
func TestSubagentDeliveriesAreAssessedWithTheParentAndSurviveRevisions(t *testing.T) {
	st, scope, h, draft, _ := memoryEffectFixture(t)
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "sub2", "cwd": scope, "prompt": "README の導入手順を更新してください"})
	parent := loadSession(st.Dir, "sub2").TurnID
	runHook(t, "subagent-start", map[string]any{"session_id": "sub2", "cwd": scope, "agent_id": "agent-x", "agent_type": "Explore"})
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
	out := runHook(t, "subagent-start", map[string]any{"session_id": "sub2", "cwd": scope, "agent_id": "agent-x", "agent_type": "Explore"})
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
	runHook(t, "subagent-start", map[string]any{"session_id": "sub3", "cwd": scope, "agent_id": "agent-y", "agent_type": "Explore"})
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
	out := reflectCall(t, h, map[string]any{"operation": "submit", "memory": []map[string]any{
		{"source_id": parent, "key": key, "result": "helpful", "decision": "手順に確認を足した", "evidence": "返答に確認の手順が入った"},
		{"source_id": subID, "key": key, "result": "unused", "decision": "サブエージェントは手順を読まなかった", "evidence": "返答に手順への言及が無い"},
	}})
	if out["complete"] != true {
		t.Fatalf("親とサブエージェントの配送を同じ会話として一度に評価できる: %v", out)
	}
	rep, err := st.MemoryReport(scope)
	if err != nil || rep.Stats.Pending != 0 || rep.Stats.AwaitingFeedback != 0 || rep.Stats.Helpful != 1 || rep.Stats.Unused != 1 {
		t.Fatalf("評価が付けば観察待ちから消える: %+v %v", rep.Stats, err)
	}
}
