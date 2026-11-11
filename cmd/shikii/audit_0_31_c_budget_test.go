package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// audit C, finding A-6 (INVARIANTS 4): the person's request is a long pasted log. The notice asks for work prepare on the CLI with
// no max_bytes; the CLI widens its default to 64 KiB when the request does not fit 28 KiB.
func TestAuditCCLIPrepareAfterAVeryLongRequest(t *testing.T) {
	for _, size := range []int{56 << 10, 60 << 10, 64 << 10, 120 << 10} {
		t.Run(fmt.Sprintf("a request of %d bytes", size), func(t *testing.T) {
			repo, _, _ := planningFixture(t)
			t.Chdir(repo)
			prompt := "Use the agreed CSV columns and keep the existing filters. The log follows.\n" + strings.Repeat("2026-11-12 12:00:00 export row written with filters kept\n", size/56)
			notice := feedbackContext(t, runUnreviewedHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": prompt}))
			ids := auditIDs.FindString(notice)
			if ids == "" {
				t.Fatalf("the notice carries the ids: %.300s", notice)
			}
			var out string
			var err error
			withStdin(t, strings.TrimSuffix(ids, "}")+`,"query":"CSV export filters"}`, func() { out, err = captureErr(func() error { return cmdWork([]string{"prepare"}) }) })
			if err != nil {
				t.Errorf("INVARIANT 4: the call the notice asks for is refused, and the agent holds no receipt (the request is %d bytes): %v", len(prompt), err)
				return
			}
			var reply map[string]any
			if e := json.Unmarshal([]byte(out), &reply); e != nil {
				t.Fatalf("%v: %.200s", e, out)
			}
			t.Logf("request %d bytes: reply %d bytes, receipt=%v, task=%d bytes", len(prompt), len(out), reply["receipt"] != nil, len(fmt.Sprint(reply["task"])))
			task := fmt.Sprint(reply["task"])
			if whole := min(len(strings.TrimSpace(prompt)), 64<<10); reply["receipt"] == nil || !strings.HasPrefix(out, `{"receipt":`) || !strings.HasPrefix(prompt, task) || len(task) < whole-4 {
				t.Errorf("the reply starts with the receipt and carries the request whole (up to the 64 KiB the hook keeps): receipt=%v, task %d of %d bytes, reply %.40s", reply["receipt"] != nil, len(task), whole, out)
			}
			if len(out) > workSourceBudget*3 {
				t.Errorf("the reply is %d bytes, over the largest bound a caller can name (%d)", len(out), workSourceBudget*3)
			}
		})
	}
}

// audit C, finding A-5 (INVARIANTS 5): one pending conversation had a long request (a pasted log). Its scoring targets carry the
// request whole (original_inputs), so the conversation does not fit the bound even as a head.
func TestAuditCReflectQueryWithALongRequestAmongThePending(t *testing.T) {
	st, scope, h, _, _ := memoryEffectFixture(t)
	long := "README \u306e\u5c0e\u5165\u624b\u9806\u3092\u66f4\u65b0\u3057\u3066\u304f\u3060\u3055\u3044\u3002\n" + strings.Repeat("2026-11-12 12:00:00 install step finished without a record\n", (30<<10)/60)
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "s-long", "cwd": scope, "prompt": long})
	memoryEffectComplete(t, st, scope, "s-long")
	for i := 0; i < 6; i++ {
		session := fmt.Sprintf("s%d", i)
		memoryEffectPrompt(t, scope, session)
		memoryEffectComplete(t, st, scope, session)
	}
	view, err := reflectQuery(st.Dir, scope)
	if err != nil {
		t.Fatal(err)
	}
	for i, s := range view.Sources {
		t.Logf("pending conversation %d: %6d bytes whole, %6d bytes as a head (request %d bytes, %d targets, targets %d bytes)", i, workContextSize(s), workContextSize(reflectHead(s)), len(s.Source.Request), len(s.Targets), workContextSize(s.Targets))
	}
	// (1) as the hooks leave it: the long conversation is the second oldest
	out := capture(t, func() error { return cmdReflect([]string{"query"}) })
	var cli map[string]any
	if err := json.Unmarshal([]byte(out), &cli); err != nil {
		t.Fatalf("%v: %.200s", err, out)
	}
	sources, _ := cli["sources"].([]any)
	t.Logf("the long conversation second: CLI %d bytes of 28672, %d listed, more_pending=%v", len(out), len(sources), cli["more_pending"])
	if len(sources) < len(view.Sources)-1 {
		t.Errorf("one conversation with a long request (%d bytes) makes every conversation after the first wait: %d of %d listed, in a reply of %d bytes of 28672", len(long), len(sources), len(view.Sources), len(out))
	}
	result, size := mcpToolCall(t, h, "shikii_reflect", `{"operation":"query"}`)
	structured, _ := result["structuredContent"].(map[string]any)
	mcpSources, _ := structured["sources"].([]any)
	t.Logf("the long conversation second: MCP %d bytes of 65536, %d listed, more_pending=%v", size, len(mcpSources), structured["more_pending"])
	if len(mcpSources) < len(view.Sources)-1 {
		t.Errorf("through MCP too: %d of %d listed, in a reply of %d bytes of 65536", len(mcpSources), len(view.Sources), size)
	}
	// (2) once the conversations before it are settled, it is the first
	first := view
	first.Sources = append([]reflectSource{}, view.Sources[1:]...)
	raw, _ := json.Marshal(limitReflectQuery(first, cliPrepareBytes, func(v any) int { return workContextSize(v) + 1 }))
	var reply map[string]any
	_ = json.Unmarshal(raw, &reply)
	listed, _ := reply["sources"].([]any)
	output, _ := reply["output"].(map[string]any)
	t.Logf("the long conversation first: %d bytes of 28672, %d listed, more_pending=%v, omitted=%v", len(raw)+1, len(listed), reply["more_pending"], output["omitted"])
	if len(raw)+1 > cliPrepareBytes {
		t.Errorf("INVARIANT 5: with the long conversation first, the CLI reply of reflect query is %d bytes, over 28672 (its targets carry the request whole)", len(raw)+1)
	}
}
