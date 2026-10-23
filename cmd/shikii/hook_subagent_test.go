package main

import (
	"encoding/json"
	"strings"
	"testing"

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
