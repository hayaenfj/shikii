package main

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/notebook"
)

// R1 (re-launching within 30 seconds of a refusal shifts by one) is not fixed, because SubagentStart has no call id and can only be matched by time and order;
// it is written as a limitation in integrations/claude-code.md

// R2: refuse an Agent call with no type (it starts as general-purpose), and a minute later start one with subagent_type: general-purpose.
// The 30-second discard compares types raw (it sees "" and "general-purpose" as different types), so the refused note stays and is used by the later child
func TestReaudit091BurstTreatsEmptyTypeAsGeneralPurpose(t *testing.T) {
	st, scope, _, _, _ := memoryEffectFixture(t)
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "r2", "cwd": scope, "prompt": "画面の色を直して"})
	runHook(t, "agent-task", map[string]any{"session_id": "r2", "cwd": scope, "tool_name": "Agent", "tool_input": map[string]any{"prompt": "断られた仕事 AAA の調査"}})
	if err := updateSession(st.Dir, "r2", func(s *hookSession) {
		for i := range s.SubagentTasks {
			s.SubagentTasks[i].At = s.SubagentTasks[i].At.Add(-time.Minute)
		}
	}); err != nil {
		t.Fatal(err)
	}
	ctx := subagentContext(t, startSubagent(t, "r2", scope, "child-r2", "general-purpose", "README の導入手順を更新してください"))
	if !strings.Contains(ctx, "README の導入手順") {
		t.Fatalf("a typeless note refused a minute ago is discarded when adding a new general-purpose note. heading: %q remaining: %+v", strings.SplitN(ctx, "\n", 2)[0], loadSession(st.Dir, "r2").SubagentTasks)
	}
}

// R3: history was removed before listing, so it also disappeared from the base used to count "rare words" (words in at most half the pages).
// With 2 history pages, README was a rare word in 2 of 4 pages, but after removing them it is in 2 of 2, a common word, and the rule delivered in 0.9.0 is no longer delivered
func TestReaudit091HistoryExclusionKeepsRarity(t *testing.T) {
	st, scope, _, draft, _ := memoryEffectFixture(t)
	book := notebook.Book{Root: st.Dir, Scope: scope}
	r := draft
	r.ID, r.Title, r.Lesson = "", "README の書き方", "README は英語と日本語の 2 つだけにする。"
	if _, err := book.Ingest(r); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		h := draft
		h.ID, h.Kind, h.Scene = "", "history", "画面を直す時"
		h.Title = fmt.Sprintf("画面の色を変えた経緯 %d", i)
		h.AppliesTo = "画面の色を変える時"
		h.Lesson = fmt.Sprintf("画面の色の経緯その %d。", i)
		if _, err := book.Ingest(h); err != nil {
			t.Fatal(err)
		}
	}
	pages, err := book.Recall("README を直す")
	titles := []string{}
	for _, p := range pages {
		titles = append(titles, p.Title)
	}
	if err != nil || len(pages) == 0 {
		t.Fatalf("even with history excluded, do not change the base for rare words (as in 0.9.0, the README rule is among the candidates): %v %v", err, titles)
	}
}
