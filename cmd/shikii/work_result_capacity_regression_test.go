package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/hayaenfj/shikii/internal/notebook"
)

func TestWorkResultCapacityBoundary(t *testing.T) {
	for _, headroom := range []int{512, 768, 1024, 1280, 1536, 2048, 4096} {
		t.Run(fmt.Sprint(headroom), func(t *testing.T) {
			repo, root, _ := planningFixture(t)
			session := "audit-r3-capacity"
			b := notebook.Book{Root: root, Scope: feedbackScope(repo)}
			for i := 0; i < 8; i++ {
				reasoningPage(t, b, fmt.Sprintf("\u5408\u6210 \u72b6\u614b \u78ba\u8a8d %d", i), "howto", strings.Repeat("\u5408\u6210\u72b6\u614b\u306e\u6761\u4ef6\u3092\u78ba\u8a8d\u3057\u3066\u9032\u3080\u3002\n\n", 10))
			}
			runUnreviewedHook(t, "user-prompt-submit", map[string]any{"session_id": session, "cwd": repo, "prompt": "\u5408\u6210\u306e\u4e00\u3064\u76ee\u306e\u72b6\u614b\u3092\u78ba\u8a8d\u3059\u308b\u3002"})
			p, err := workCall(t, b, "prepare", map[string]any{"session_id": session, "turn_id": loadSession(root, session).TurnID, "query": "\u5408\u6210 \u72b6\u614b"})
			if err != nil {
				t.Fatal("\u6e96\u5099\u5931\u6557", err)
			}
			s := loadSession(root, session)
			t.Logf("\u624b\u9806\u72b6\u614b\u3042\u308a=%v", s.Reasoning.Procedure != nil)
			s.Planning.Sources["audit-capacity-padding"] = ""
			raw, err := encodeHookSession(s)
			if err != nil {
				t.Fatal("\u5883\u754c\u6e96\u5099\u5931\u6557", err)
			}
			s.Planning.Sources["audit-capacity-padding"] = strings.Repeat("p", hookSessionRecordLimit-len(raw)-headroom)
			if err = saveSession(root, session, s); err != nil {
				t.Fatal("\u5883\u754c\u4fdd\u5b58\u5931\u6557", err)
			}
			failed, err := workCall(t, b, "record", map[string]any{"receipt": p["receipt"], "result": map[string]any{"changed": "\u5408\u6210\u306e\u4e00\u3064\u76ee\u306e\u7d50\u679c\u3092\u4fdd\u5b58\u3059\u308b\u3002", "sufficient": true, "evidence": []string{"missing-audit-proof"}}})
			if err != nil {
				t.Fatal("\u5165\u529b\u53d7\u4ed8\u5931\u6557", err)
			}
			after := loadSession(root, session)
			count := auditR2Count(t, b, "Work result and remaining conditions")
			retained := pendingResultCount(after.Reasoning) > 0 || len(after.Reasoning.PendingCaptures) > 0
			t.Logf("\u4f59\u88d5=%d complete=%v \u7d50\u679c\u539f\u8cc7\u6599\u6570=%d \u672a\u51e6\u7406\u7d50\u679c\u6570=%d \u672a\u51e6\u7406\u53d6\u5f97\u6570=%d \u4fdd\u6301=%v", headroom, failed["complete"], count, pendingResultCount(after.Reasoning), len(after.Reasoning.PendingCaptures), retained)
			if count != 1 || retained {
				return
			}

			runUnreviewedHook(t, "user-prompt-submit", map[string]any{"session_id": session, "cwd": repo, "prompt": "\u5408\u6210\u306e\u5225\u306e\u72b6\u614b\u3092\u78ba\u8a8d\u3059\u308b\u3002"})
			p2, err := workCall(t, b, "prepare", map[string]any{"session_id": session, "turn_id": loadSession(root, session).TurnID, "query": "\u5408\u6210 \u72b6\u614b"})
			if err != nil {
				t.Fatal("\u6b21\u306e\u6e96\u5099\u5931\u6557", err)
			}
			ended, err := workCall(t, b, "record", map[string]any{"receipt": p2["receipt"], "result": map[string]any{"changed": "\u5408\u6210\u306e\u5225\u306e\u7d50\u679c\u3060\u3051\u3092\u4fdd\u5b58\u3059\u308b\u3002"}})
			if err != nil {
				t.Fatal("\u6b21\u306e\u4fdd\u5b58\u5931\u6557", err)
			}
			last := loadSession(root, session)
			stop := verifyStopReasoning(root, last, last)
			t.Logf("\u6b21\u306e\u4fdd\u5b58complete=%v \u7d42\u4e86\u53d7\u4ed8=%v \u5143\u306e\u672a\u51e6\u7406\u7d50\u679c=%d", ended["complete"], stop == nil, pendingResultCount(last.Reasoning))
			if ended["complete"] == true && stop == nil {
				t.Error("\u539f\u8a18\u9332\u5f8c\u306e\u5bb9\u91cf\u5931\u6557\u3067\u672a\u53d6\u5f97\u306e\u8a3c\u62e0\u306b\u5bfe\u3059\u308b\u4fdd\u5b58\u7fa9\u52d9\u304c\u6d88\u3048\u305f")
			}
		})
	}
}

func TestWorkResultPartialAndBoundaryControls(t *testing.T) {
	repo, root, _ := planningFixture(t)
	session := "audit-r3-controls"
	runUnreviewedHook(t, "user-prompt-submit", map[string]any{"session_id": session, "cwd": repo, "prompt": "\u5408\u6210\u306e\u4e00\u3064\u76ee\u306e\u4f9d\u983c\u3002"})
	b := notebook.Book{Root: root, Scope: feedbackScope(repo)}
	p, err := workCall(t, b, "prepare", map[string]any{"session_id": session, "turn_id": loadSession(root, session).TurnID, "query": "\u5408\u6210"})
	if err != nil {
		t.Fatal(err)
	}
	failed, err := workCall(t, b, "record", map[string]any{"receipt": p["receipt"], "result": map[string]any{"changed": "\u5408\u6210\u306e\u7d50\u679c\u3002", "sufficient": true, "evidence": []string{"missing-proof"}}})
	if err != nil || failed["complete"] != false {
		t.Fatal("\u5bfe\u7167\u306e\u672a\u53d6\u5f97\u62d2\u5426\u304c\u52d5\u304b\u306a\u3044", err)
	}
	original := loopObject(t, failed["storage"])["result_source"].(string)
	runUnreviewedHook(t, "user-prompt-submit", map[string]any{"session_id": session, "cwd": repo, "prompt": "\u5408\u6210\u306e\u4e8c\u3064\u76ee\u306e\u4f9d\u983c\u3002"})
	p2, err := workCall(t, b, "prepare", map[string]any{"session_id": session, "turn_id": loadSession(root, session).TurnID, "query": "\u5408\u6210"})
	if err != nil {
		t.Fatal(err)
	}
	current, err := workCall(t, b, "record", map[string]any{"receipt": p2["receipt"], "result": map[string]any{"changed": "\u4e8c\u3064\u76ee\u306e\u7d50\u679c\u3002"}})
	if err != nil || current["complete"] != false {
		t.Fatal("\u5143\u306e\u672a\u51e6\u7406\u7d50\u679c\u3092\u6b8b\u3055\u305a\u7d42\u4e86\u3057\u305f", err)
	}
	repair, err := workCall(t, b, "record", map[string]any{"receipt": p2["receipt"], "observations": []any{map[string]any{"kind": "test", "title": "\u5408\u6210\u306e\u78ba\u8a8d", "content": "\u4e00\u3064\u76ee\u306e\u8a3c\u62e0\u304c\u5f97\u3089\u308c\u305f\u3002"}}, "result": map[string]any{"ref": original, "evidence": []string{"new1"}}})
	if err != nil || repair["complete"] != true {
		t.Fatal("ref\u56de\u5fa9\u5931\u6557", err)
	}
	raw, _ := json.Marshal(repair["reasoning"])
	t.Logf("\u901a\u5e38\u306e\u672a\u51e6\u7406\u4fdd\u6301\u3068ref\u56de\u5fa9=\u6210\u529f \u72b6\u614b=%s", string(raw))
}

func TestWorkResultCapacityThroughPublicReads(t *testing.T) {
	for _, desired := range []int{1400, 2400} {
		t.Run(fmt.Sprint(desired), func(t *testing.T) {
			repo, root, _ := planningFixture(t)
			session := "audit-r3-public-capacity"
			b := notebook.Book{Root: root, Scope: feedbackScope(repo)}
			reasoningPage(t, b, "\u76e3\u67fb\u7528\u306e\u521d\u671f\u8cc7\u6599", "howto", "\u5408\u6210\u72b6\u614b\u3092\u78ba\u8a8d\u3059\u308b\u3002")
			runUnreviewedHook(t, "user-prompt-submit", map[string]any{"session_id": session, "cwd": repo, "prompt": "\u5408\u6210\u72b6\u614b\u3092\u78ba\u8a8d\u3059\u308b\u3002"})
			p, err := workCall(t, b, "prepare", map[string]any{"session_id": session, "turn_id": loadSession(root, session).TurnID, "query": "\u76e3\u67fb\u7528\u306e\u521d\u671f\u8cc7\u6599"})
			if err != nil {
				t.Fatal("\u521d\u671f\u6e96\u5099\u5931\u6557", err)
			}
			previousSize := func() int {
				raw, e := encodeHookSession(loadSession(root, session))
				if e != nil {
					t.Fatal(e)
				}
				return len(raw)
			}
			lastOverhead := 2000
			for i := 0; i < 63; i++ {
				before := previousSize()
				remaining := hookSessionRecordLimit - before
				length := 16000
				last := false
				if remaining < 16000+lastOverhead+desired {
					length = remaining - lastOverhead - desired
					last = true
				}
				if length < 1 {
					t.Fatalf("\u5883\u754c\u6e96\u5099\u306e\u6b8b\u91cf\u4e0d\u8db3 %d", remaining)
				}
				page := reasoningPage(t, b, fmt.Sprintf("\u76e3\u67fb\u5bb9\u91cf\u8cc7\u6599 %02d", i), "howto", strings.Repeat("p", length))
				_, err = workCall(t, b, "prepare", map[string]any{"receipt": p["receipt"], "query": "lesson:" + page.ID + "@" + page.Revision, "max_bytes": 393216})
				if err != nil {
					t.Fatal("\u660e\u793a\u8aad\u53d6\u6e96\u5099\u5931\u6557", i, err)
				}
				after := previousSize()
				lastOverhead = after - before - length
				t.Logf("\u8cc7\u6599=%d \u672c\u6587=%d \u4fdd\u5b58\u91cf=%d \u4f59\u88d5=%d \u5897\u5206\u4f59\u767d=%d", i, length, after, hookSessionRecordLimit-after, lastOverhead)
				if last {
					break
				}
			}
			s := loadSession(root, session)
			if s.Reasoning.Procedure == nil {
				t.Fatal("\u624b\u9806\u306e\u6761\u4ef6\u304c\u6210\u7acb\u3057\u306a\u3044")
			}
			failed, err := workCall(t, b, "record", map[string]any{"receipt": p["receipt"], "result": map[string]any{"changed": "\u5408\u6210\u306e\u4e00\u3064\u76ee\u306e\u7d50\u679c\u3092\u4fdd\u5b58\u3059\u308b\u3002", "sufficient": true, "evidence": []string{"missing-audit-proof"}}})
			if err != nil {
				t.Fatal("\u7d50\u679c\u53d7\u4ed8\u5931\u6557", err)
			}
			after := loadSession(root, session)
			count := auditR2Count(t, b, "Work result and remaining conditions")
			t.Logf("\u5bb9\u91cf\u5931\u6557complete=%v \u539f\u8a18\u9332=%d \u672a\u51e6\u7406\u7d50\u679c=%d \u672a\u51e6\u7406\u53d6\u5f97=%d", failed["complete"], count, pendingResultCount(after.Reasoning), len(after.Reasoning.PendingCaptures))
			if count != 1 {
				t.Fatal("\u7d50\u679c\u306e\u539f\u8a18\u9332\u4fdd\u5b58\u306b\u5c4a\u304b\u306a\u304b\u3063\u305f")
			}
			retained := pendingResultCount(after.Reasoning) > 0 || len(after.Reasoning.PendingCaptures) > 0
			if desired == 2400 && !retained {
				t.Fatal("\u4f59\u88d5\u306e\u3042\u308b\u5bfe\u7167\u3067\u3082\u672a\u51e6\u7406\u7d50\u679c\u3092\u5931\u3063\u305f")
			}
			original := loopObject(t, failed["storage"])["result_source"].(string)
			runUnreviewedHook(t, "user-prompt-submit", map[string]any{"session_id": session, "cwd": repo, "prompt": "\u5408\u6210\u306e\u5225\u306e\u72b6\u614b\u3092\u78ba\u8a8d\u3059\u308b\u3002"})
			p2, err := workCall(t, b, "prepare", map[string]any{"session_id": session, "turn_id": loadSession(root, session).TurnID, "query": "\u5225\u306e\u72b6\u614b"})
			if err != nil {
				t.Fatal("\u6b21\u306e\u6e96\u5099\u5931\u6557", err)
			}
			ended, err := workCall(t, b, "record", map[string]any{"receipt": p2["receipt"], "result": map[string]any{"changed": "\u5408\u6210\u306e\u5225\u306e\u7d50\u679c\u3060\u3051\u3092\u4fdd\u5b58\u3059\u308b\u3002"}})
			if err != nil {
				t.Fatal("\u6b21\u306e\u4fdd\u5b58\u5931\u6557", err)
			}
			last := loadSession(root, session)
			stop := verifyStopReasoning(root, last, last)
			t.Logf("\u6b21\u306e\u4fdd\u5b58complete=%v \u7d42\u4e86\u53d7\u4ed8=%v", ended["complete"], stop == nil)
			repair, err := workCall(t, b, "record", map[string]any{"receipt": p2["receipt"], "observations": []any{map[string]any{"kind": "test", "title": "\u56de\u5fa9\u7528\u306e\u5408\u6210\u8a3c\u62e0", "content": "\u4e00\u3064\u76ee\u306e\u7d50\u679c\u306e\u78ba\u8a8d\u304c\u5f97\u3089\u308c\u305f\u3002"}}, "result": map[string]any{"ref": original, "evidence": []string{"new1"}}})
			if err != nil {
				t.Fatal("\u56de\u5fa9\u547c\u51fa\u3057\u5931\u6557", err)
			}
			t.Logf("\u5143\u306eref\u306e\u56de\u5fa9complete=%v \u30a8\u30e9\u30fc=%v", repair["complete"], repair["errors"])
			if !retained && ended["complete"] == true && stop == nil && repair["complete"] == false {
				t.Error("\u516c\u958b\u8aad\u53d6\u306e\u84c4\u7a4d\u3067\u539f\u8a18\u9332\u5f8c\u306e\u672a\u53d6\u5f97\u8a3c\u62e0\u306e\u7fa9\u52d9\u304c\u6d88\u3048\u305f")
			}
			if retained && (ended["complete"] != false || stop == nil || repair["complete"] != true) {
				t.Error("\u901a\u5e38\u306e\u672a\u51e6\u7406\u7d50\u679c\u4fdd\u6301\u3068ref\u56de\u5fa9\u306e\u5bfe\u7167\u304c\u6210\u7acb\u3057\u306a\u3044")
			}
		})
	}
}
