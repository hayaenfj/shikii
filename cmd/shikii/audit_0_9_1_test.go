package main

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/notebook"
)

// G1: the parent called the Agent tool but the child never started (the person refused, etc.). When a child of the same type is started for different work within 10 minutes,
// that child is chosen with the text of the previous (refused) work
func TestAudit091DeniedAgentTaskShiftsLaterChild(t *testing.T) {
	st, scope, _, _, _ := memoryEffectFixture(t)
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "g1", "cwd": scope, "prompt": "画面の色を直して"})
	// 1st: the refused Agent tool (only PreToolUse arrives)
	runHook(t, "agent-task", map[string]any{"session_id": "g1", "cwd": scope, "tool_name": "Agent", "tool_input": map[string]any{"prompt": "断られた仕事 AAA の調査", "subagent_type": "Explore"}})
	// The time until the person refuses and the parent reconsiders (1 minute). Within the 10-minute limit
	if err := updateSession(st.Dir, "g1", func(s *hookSession) {
		for i := range s.SubagentTasks {
			s.SubagentTasks[i].At = s.SubagentTasks[i].At.Add(-time.Minute)
		}
	}); err != nil {
		t.Fatal(err)
	}
	// 2nd: the child that actually starts
	out := startSubagent(t, "g1", scope, "child-2", "Explore", "README の導入手順を更新してください")
	ctx := subagentContext(t, out)
	if !strings.Contains(ctx, "README の導入手順") {
		t.Fatalf("a started child should say it was chosen by the text of the work given to that child. actual heading: %q\nremaining notes: %+v", strings.SplitN(ctx, "\n", 2)[0], loadSession(st.Dir, "g1").SubagentTasks)
	}
}

// G2: start 2 children of the same type side by side. SubagentStart chooses from the same copy read outside the lock, so both pick the same note and one note remains
func TestAudit091ParallelSubagentsPickDistinctTasks(t *testing.T) {
	st, scope, _, _, _ := memoryEffectFixture(t)
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "g2", "cwd": scope, "prompt": "画面の色を直して"})
	for _, p := range []string{"README の導入手順を更新してください ONE", "README の導入手順を更新してください TWO"} {
		runHook(t, "agent-task", map[string]any{"session_id": "g2", "cwd": scope, "tool_name": "Agent", "tool_input": map[string]any{"prompt": p, "subagent_type": "Explore"}})
	}
	agent := st.Dir
	snapshot := loadSession(agent, "g2") // the copy the 2 hooks read at the same time
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
	if h1 == h2 || len(left) != 0 || !strings.Contains(h1, "ONE") || !strings.Contains(h2, "TWO") { // c1 is ONE, c2 is TWO (so it does not pass even if one has no note; recheck GC)
		t.Fatalf("2 children started side by side use separate notes, and no note remains:\n c1=%q\n c2=%q\n remaining=%+v", h1, h2, left)
	}
}

// G3: excluding history applies after candidates are cut to 6. With 6 history pages close to the request, the rule decided in this project (rule) loses its seat
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
		t.Fatalf("choose 2 after removing history pages, and deliver the rule decided in this project. candidate kinds=%v\ndelivered text=%q", kinds, ctx)
	}
}
