package main

import (
	"fmt"
	"github.com/hayaenfj/shikii/internal/notebook"
	"testing"
)

func TestWorkResultReferenceDoesNotCoverLaterEvidence(t *testing.T) {
	for _, later := range []bool{false, true} {
		for _, renew := range []bool{false, true} {
			t.Run(fmt.Sprintf("later_%v_renew_%v", later, renew), func(t *testing.T) {
				repo, root, _ := planningFixture(t)
				session := "audit-r4-later-evidence"
				b := notebook.Book{Root: root, Scope: feedbackScope(repo)}
				runUnreviewedHook(t, "user-prompt-submit", map[string]any{"session_id": session, "cwd": repo, "prompt": "\u9014\u4e2d\u306e\u5408\u6210\u8a3c\u62e0\u3068\u7d50\u679c\u306e\u533a\u5225\u3092\u78ba\u8a8d\u3059\u308b\u3002"})
				p, err := workCall(t, b, "prepare", map[string]any{"session_id": session, "turn_id": loadSession(root, session).TurnID, "query": "\u5408\u6210"})
				if err != nil {
					t.Fatal("\u6e96\u5099\u5931\u6557", err)
				}
				first, err := workCall(t, b, "record", map[string]any{"receipt": p["receipt"], "result": map[string]any{"changed": "\u4e00\u3064\u76ee\u306e\u5408\u6210\u78ba\u8a8d\u3092\u7d42\u3048\u305f\u3002"}})
				if err != nil || first["complete"] != true {
					t.Fatal("\u521d\u671f\u4fdd\u5b58\u5931\u6557", err)
				}
				second, err := workCall(t, b, "record", map[string]any{"receipt": p["receipt"], "observations": []any{map[string]any{"kind": "test", "title": "\u4e8c\u3064\u76ee\u306e\u78ba\u8a8d", "content": "\u5408\u6210\u6761\u4ef6\u306f\u6210\u529f\u3057\u305f\u3002"}}, "result": map[string]any{"changed": "\u4e8c\u3064\u76ee\u306e\u5408\u6210\u78ba\u8a8d\u306f\u6210\u529f\u3057\u305f\u3002", "sufficient": true, "evidence": []string{"new1"}}, "memory": []any{map[string]any{"ref": "missing-memory", "result": "unknown"}}})
				if err != nil || second["complete"] != false {
					t.Fatal("\u90e8\u5206\u6210\u529f\u306e\u6e96\u5099\u5931\u6557", err)
				}
				ref := loopObject(t, second["storage"])["result_source"]
				if later {
					third, err := workCall(t, b, "record", map[string]any{"receipt": p["receipt"], "observations": []any{map[string]any{"kind": "test", "title": "\u5f8c\u306e\u65b0\u3057\u3044\u53cd\u4f8b", "content": "\u4e8c\u3064\u76ee\u306e\u78ba\u8a8d\u304c\u898b\u843d\u3068\u3057\u305f\u5408\u6210\u306e\u6240\u6709\u6761\u4ef6\u3092\u691c\u51fa\u3057\u305f\u3002\u524d\u306e\u6210\u529f\u5831\u544a\u306f\u73fe\u5728\u306e\u7d50\u679c\u3068\u7570\u306a\u308b\u3002"}}, "memory": []any{map[string]any{"ref": "missing-memory", "result": "unknown"}}})
					if err != nil || third["complete"] != false {
						t.Fatal("\u65b0\u3057\u3044\u53cd\u4f8b\u306e\u4fdd\u5b58\u5931\u6557", err)
					}
				}
				if renew {
					p, err = workCall(t, b, "prepare", map[string]any{"session_id": session, "turn_id": loadSession(root, session).TurnID, "query": "\u5408\u6210"})
					if err != nil {
						t.Fatal("\u518d\u6e96\u5099\u5931\u6557", err)
					}
				}
				before := loadSession(root, session)
				beforeStop := verifyStopReasoning(root, before, before)
				replay, err := workCall(t, b, "record", map[string]any{"receipt": p["receipt"], "result": map[string]any{"ref": ref}})
				if err != nil {
					t.Fatal("\u56de\u5fa9\u53d7\u4ed8\u5931\u6557", err)
				}
				complete := loopObject(t, replay["reasoning"])["complete"]
				after := loadSession(root, session)
				afterStop := verifyStopReasoning(root, after, after)
				t.Logf("\u5f8c\u306e\u8a3c\u62e0=%v \u518d\u6e96\u5099=%v ref\u56de\u5fa9complete=%v reasoning.complete=%v \u7d42\u4e86\u53d7\u4ed8\u524d=%v \u7d42\u4e86\u53d7\u4ed8\u5f8c=%v \u4e16\u4ee3\u524d=%d \u4e16\u4ee3\u5f8c=%d errors=%v", later, renew, replay["complete"], complete, beforeStop == nil, afterStop == nil, before.Reasoning.Generation, after.Reasoning.Generation, replay["errors"])
				if !later && (replay["complete"] != true || complete != true) {
					t.Error("\u76f4\u8fd1\u306e\u90e8\u5206\u6210\u529f\u306b\u5bfe\u3059\u308bref\u56de\u5fa9\u304c\u52d5\u304b\u306a\u3044")
				}
				if later && replay["complete"] == true && complete == true && afterStop == nil {
					t.Error("\u5f8c\u306e\u8a3c\u62e0\u3092\u8ffd\u52a0\u3057\u3066\u3082\u4ee5\u524d\u306e\u7d50\u679cref\u3067\u5b8c\u4e86\u3067\u304d\u305f")
				}
				if later {
					corrected, err := workCall(t, b, "record", map[string]any{"receipt": p["receipt"], "observations": []any{map[string]any{"kind": "test", "title": "\u5f8c\u306e\u6761\u4ef6\u3092\u518d\u78ba\u8a8d", "content": "\u6240\u6709\u6761\u4ef6\u306e\u5408\u6210\u53cd\u4f8b\u3092\u518d\u78ba\u8a8d\u3057\u305f\u3002"}}, "result": map[string]any{"changed": "\u5f8c\u306e\u8a3c\u62e0\u3092\u53cd\u6620\u3057\u3001\u4e8c\u3064\u76ee\u306e\u6210\u529f\u5831\u544a\u3092\u8a02\u6b63\u3057\u305f\u3002", "sufficient": false, "evidence": []string{"new1"}}})
					if err != nil {
						t.Fatal("\u65b0\u3057\u3044\u7d50\u679c\u306e\u4fdd\u5b58\u5931\u6557", err)
					}
					t.Logf("\u5f8c\u306e\u8a3c\u62e0\u3092\u53cd\u6620\u3057\u305f\u65b0\u7d50\u679ccomplete=%v reasoning.complete=%v", corrected["complete"], loopObject(t, corrected["reasoning"])["complete"])
					if corrected["complete"] != true || loopObject(t, corrected["reasoning"])["complete"] != true {
						t.Error("\u65b0\u3057\u3044\u7d50\u679c\u3092\u4f7f\u3063\u305f\u56de\u5fa9\u306e\u5bfe\u7167\u304c\u52d5\u304b\u306a\u3044")
					}
				}
			})
		}
	}
}
