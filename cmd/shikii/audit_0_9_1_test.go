package main

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/notebook"
)

// G1: 親が Agent ツールを呼んだが子が始まらなかった(人が断った等)。10 分以内に同じ種類の子を別の仕事で起こすと、
// その子は前の(断られた)仕事の文で選ばれる
func TestAudit091DeniedAgentTaskShiftsLaterChild(t *testing.T) {
	st, scope, _, _, _ := memoryEffectFixture(t)
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "g1", "cwd": scope, "prompt": "画面の色を直して"})
	// 1 回目: 断られた Agent ツール(PreToolUse だけが来る)
	runHook(t, "agent-task", map[string]any{"session_id": "g1", "cwd": scope, "tool_name": "Agent", "tool_input": map[string]any{"prompt": "断られた仕事 AAA の調査", "subagent_type": "Explore"}})
	// 人が断り、親が考え直すまでの間(1 分)。10 分の期限の内
	if err := updateSession(st.Dir, "g1", func(s *hookSession) {
		for i := range s.SubagentTasks {
			s.SubagentTasks[i].At = s.SubagentTasks[i].At.Add(-time.Minute)
		}
	}); err != nil {
		t.Fatal(err)
	}
	// 2 回目: 実際に始まる子
	out := startSubagent(t, "g1", scope, "child-2", "Explore", "README の導入手順を更新してください")
	ctx := subagentContext(t, out)
	if !strings.Contains(ctx, "README の導入手順") {
		t.Fatalf("始まった子には、その子に渡した仕事の文で選んだと書くべき。実際の見出し: %q\n残った控え: %+v", strings.SplitN(ctx, "\n", 2)[0], loadSession(st.Dir, "g1").SubagentTasks)
	}
}

// G2: 同じ種類の子を 2 つ並べて起こす。SubagentStart は錠の外で読んだ同じ写しから選ぶので、両方が同じ控えを選び、片方の控えが残る
func TestAudit091ParallelSubagentsPickDistinctTasks(t *testing.T) {
	st, scope, _, _, _ := memoryEffectFixture(t)
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "g2", "cwd": scope, "prompt": "画面の色を直して"})
	for _, p := range []string{"README の導入手順を更新してください ONE", "README の導入手順を更新してください TWO"} {
		runHook(t, "agent-task", map[string]any{"session_id": "g2", "cwd": scope, "tool_name": "Agent", "tool_input": map[string]any{"prompt": p, "subagent_type": "Explore"}})
	}
	agent := st.Dir
	snapshot := loadSession(agent, "g2") // 2 つの hook が同時に読んだ写し
	o1, m1, err1 := hookSubagentStart(agent, &hookInput{SessionID: "g2", CWD: scope, AgentID: "c1", AgentType: "Explore"}, snapshot)
	o2, m2, err2 := hookSubagentStart(agent, &hookInput{SessionID: "g2", CWD: scope, AgentID: "c2", AgentType: "Explore"}, snapshot)
	if err1 != nil || err2 != nil {
		t.Fatal(err1, err2)
	}
	for _, ms := range [][]func(*hookSession){m1, m2} {
		if err := updateSession(agent, "g2", func(s *hookSession) {
			for _, m := range ms {
				m(s)
			}
		}); err != nil {
			t.Fatal(err)
		}
	}
	h1, h2 := strings.SplitN(o1.Context, "\n", 2)[0], strings.SplitN(o2.Context, "\n", 2)[0]
	left := loadSession(agent, "g2").SubagentTasks
	if h1 == h2 || len(left) != 0 || !strings.Contains(h1, "ONE") || !strings.Contains(h2, "TWO") { // c1 は ONE、c2 は TWO(片方が控え無しでも通らないように。再監査 GC)
		t.Fatalf("並べて起こした 2 つの子は別々の控えを使い、控えは残らない:\n c1=%q\n c2=%q\n 残り=%+v", h1, h2, left)
	}
}

// G3: 経緯(history)の除外が、候補を 6 件に切った後にかかる。依頼に近い経緯の記事が 6 件あると、このプロジェクトで決められたルール(rule)が席を失う
func TestAudit091HistoryFilterBeforeCut(t *testing.T) {
	st, scope, _, draft, _ := memoryEffectFixture(t)
	book := notebook.Book{Root: st.Dir, Scope: scope}
	for i := 0; i < notebook.RecallCandidates; i++ {
		h := draft
		h.ID, h.Kind, h.Scene = "", "history", "README の導入手順を更新する時"
		h.Title = fmt.Sprintf("README の導入手順を更新した経緯 %d", i)
		h.AppliesTo = "README の導入手順を更新する時 README 導入手順 更新"
		h.Lesson = fmt.Sprintf("README の導入手順の更新の経緯その %d。", i)
		if _, err := book.Ingest(h); err != nil {
			t.Fatal(err)
		}
	}
	recalled, _ := book.Recall("README の導入手順を更新してください")
	kinds := []string{}
	for _, p := range recalled {
		kinds = append(kinds, p.Kind)
	}
	ctx, err := memoryContext(st.Dir, scope, "README の導入手順を更新してください", &hookSession{TurnID: "cccccccccccccccc"})
	if err != nil || !strings.Contains(ctx, draft.Lesson) {
		t.Fatalf("経緯の記事を除いた後で 2 件を選び、このプロジェクトで決められたルールを届ける。候補の種類=%v\n届いた文=%q", kinds, ctx)
	}
}
