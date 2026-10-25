package main

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/notebook"
)

// R1(断ってから 30 秒の内に起こし直すと 1 つずれる)は、SubagentStart に呼び出しの id が無く時刻と順番でしか結べないので直さず、
// integrations/claude-code.md に制限として書いた

// R2: 種類を書かない Agent 呼び出し(general-purpose として始まる)を断り、1 分後に subagent_type: general-purpose で起こす。
// 30 秒の捨て方は種類を生のまま比べる("" と "general-purpose" を別の種類と見る)ので、断られた控えが残り、後の子に使われる
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
		t.Fatalf("1 分前に断られた種類なしの控えは、general-purpose の新しい控えを足す時に捨てる。見出し: %q 残り: %+v", strings.SplitN(ctx, "\n", 2)[0], loadSession(st.Dir, "r2").SubagentTasks)
	}
}

// R3: 経緯を並べる前に外したので、「珍しい語」(記事の半分以下にしか無い語)を数える母数からも経緯が消えた。
// 経緯が 2 本あると README は 4 本中 2 本で珍しい語だったが、外すと 2 本中 2 本で遍在する語になり、0.9.0 で届いていた決まりが届かない
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
		t.Fatalf("経緯を除いても、珍しい語の母数は変えない(0.9.0 と同じく README の決まりが候補に入る): %v %v", err, titles)
	}
}
