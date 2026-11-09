package main

import (
	"errors"
	"fmt"
	"testing"

	"github.com/hayaenfj/shikii/internal/notebook"
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

func TestWorkUnboundResultReferenceDoesNotCoverLaterEvidence(t *testing.T) {
	for _, later := range []bool{false, true} {
		for _, mode := range []string{"same_receipt", "same_turn_prepare", "new_turn"} {
			t.Run(fmt.Sprintf("later_%v_%s", later, mode), func(t *testing.T) {
				repo, root, _ := planningFixture(t)
				session := "audit-r5-unbound-generation"
				b := notebook.Book{Root: root, Scope: feedbackScope(repo)}
				runUnreviewedHook(t, "user-prompt-submit", map[string]any{"session_id": session, "cwd": repo, "prompt": "\u5408\u6210\u306e\u5143\u306e\u6761\u4ef6\u3092\u78ba\u8a8d\u3059\u308b\u3002"})
				p, err := workCall(t, b, "prepare", map[string]any{"session_id": session, "turn_id": loadSession(root, session).TurnID, "query": "\u5408\u6210"})
				if err != nil {
					t.Fatal("\u6e96\u5099\u5931\u6557", err)
				}
				first, err := workCall(t, b, "record", map[string]any{"receipt": p["receipt"], "observations": []any{map[string]any{"kind": "test", "title": "\u5143\u306e\u5408\u6210\u78ba\u8a8d", "content": "\u5143\u306e\u72ed\u3044\u5408\u6210\u6761\u4ef6\u306f\u6210\u529f\u3057\u305f\u3002"}}, "result": map[string]any{"changed": "\u5143\u306e\u5408\u6210\u6761\u4ef6\u306e\u78ba\u8a8d\u306f\u6210\u529f\u3057\u305f\u3002", "sufficient": true, "evidence": []string{"missing-proof"}}})
				if err != nil || first["complete"] != false {
					t.Fatal("\u672a\u53d6\u5f97\u7d50\u679c\u306e\u6e96\u5099\u5931\u6557", err)
				}
				original := loopObject(t, first["storage"])["result_source"]
				proofID, proofRef := "", ""
				for _, raw := range first["saved"].([]any) {
					item := raw.(map[string]any)
					if item["kind"] == "observation" {
						proofID = item["id"].(string)
						proofRef = item["ref"].(string)
					}
				}
				if proofID == "" {
					t.Fatal("\u5143\u306e\u5408\u6210\u8a3c\u62e0\u304c\u4fdd\u5b58\u3055\u308c\u3066\u3044\u306a\u3044")
				}
				if later {
					third, err := workCall(t, b, "record", map[string]any{"receipt": p["receipt"], "observations": []any{map[string]any{"kind": "test", "title": "\u5f8c\u306e\u65b0\u3057\u3044\u6761\u4ef6", "content": "\u5143\u306e\u78ba\u8a8d\u304c\u6271\u308f\u306a\u3044\u5408\u6210\u306e\u6240\u6709\u6761\u4ef6\u3067\u53cd\u4f8b\u304c\u5f97\u3089\u308c\u305f\u3002\u4ee5\u524d\u306e\u6210\u529f\u5831\u544a\u306f\u65b0\u3057\u3044\u6761\u4ef6\u3092\u6271\u308f\u306a\u3044\u3002"}}})
					if err != nil || third["complete"] != false {
						t.Fatal("\u5f8c\u306e\u89b3\u6e2c\u306e\u6e96\u5099\u5931\u6557", err)
					}
				}
				if mode == "new_turn" {
					runUnreviewedHook(t, "user-prompt-submit", map[string]any{"session_id": session, "cwd": repo, "prompt": "\u5225\u306e\u5408\u6210\u306e\u4f5c\u696d\u3092\u78ba\u8a8d\u3059\u308b\u3002"})
				}
				if mode != "same_receipt" {
					p, err = workCall(t, b, "prepare", map[string]any{"session_id": session, "turn_id": loadSession(root, session).TurnID, "query": "\u5408\u6210"})
					if err != nil {
						t.Fatal("\u518d\u6e96\u5099\u5931\u6557", err)
					}
					proofRef = ""
					for ref, id := range loadSession(root, session).Planning.Work.Sources {
						if id == proofID {
							proofRef = ref
							break
						}
					}
					if proofRef == "" {
						t.Fatal("\u518d\u6e96\u5099\u304c\u5143\u306e\u672a\u6574\u7406\u306e\u5408\u6210\u8a3c\u62e0\u3092\u8fd4\u3057\u3066\u3044\u306a\u3044")
					}
				}
				before := loadSession(root, session)
				repaired, err := workCall(t, b, "record", map[string]any{"receipt": p["receipt"], "result": map[string]any{"ref": original, "evidence": []string{proofRef}}})
				if err != nil {
					t.Fatal("\u5143\u306e\u8a3c\u62e0\u56de\u5fa9\u5931\u6557", err)
				}
				after := loadSession(root, session)
				complete := loopObject(t, repaired["reasoning"])["complete"]
				stop := verifyStopReasoning(root, after, after)
				t.Logf("\u5f8c\u306e\u8a3c\u62e0=%v \u7d4c\u8def=%s \u4fdd\u5b58complete=%v reasoning.complete=%v \u7d42\u4e86\u53d7\u4ed8=%v \u4e16\u4ee3\u524d=%d \u4e16\u4ee3\u5f8c=%d errors=%v", later, mode, repaired["complete"], complete, stop == nil, before.Reasoning.Generation, after.Reasoning.Generation, repaired["errors"])
				if !later && mode != "new_turn" && (repaired["complete"] != true || complete != true || stop != nil) {
					t.Error("\u5143\u306e\u8a3c\u62e0\u3060\u3051\u306e\u56de\u5fa9\u3092\u5b8c\u4e86\u3067\u304d\u306a\u3044")
				}
				if (later || mode == "new_turn") && complete == true && stop == nil {
					t.Error("\u672a\u53d6\u5f97\u306e\u5143\u7d50\u679c\u306e\u56de\u5fa9\u304c\u5f8c\u306e\u5225\u306e\u4f5c\u696d\u3092\u5b8c\u4e86\u3055\u305b\u305f")
				}
			})
		}
	}
}

func TestWorkEndingCaptureResumesWithoutRepeatingResult(t *testing.T) {
	for _, point := range []string{"none", "ending-source", "ending-history"} {
		for _, renew := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s_renew_%v", point, renew), func(t *testing.T) {
				repo, root, _ := planningFixture(t)
				session := "audit-r5-ending-capture"
				b := notebook.Book{Root: root, Scope: feedbackScope(repo)}
				runUnreviewedHook(t, "user-prompt-submit", map[string]any{"session_id": session, "cwd": repo, "prompt": "\u7d42\u4e86\u8a18\u9332\u306e\u5408\u6210\u56de\u5fa9\u3092\u78ba\u8a8d\u3059\u308b\u3002"})
				p, err := workCall(t, b, "prepare", map[string]any{"session_id": session, "turn_id": loadSession(root, session).TurnID, "query": "\u5408\u6210"})
				if err != nil {
					t.Fatal("\u6e96\u5099\u5931\u6557", err)
				}
				old := workAfterWrite
				defer func() { workAfterWrite = old }()
				n, injected := 0, false
				workAfterWrite = func(kind string) error {
					if kind == "observation" {
						n++
					}
					if !injected && (point == "ending-source" && kind == "observation" && n == 3 || point == "ending-history" && kind == "reasoning-history") {
						injected = true
						return errors.New("\u5408\u6210\u306e\u7d42\u4e86\u4fdd\u5b58\u5fdc\u7b54\u3092\u4e2d\u65ad")
					}
					return nil
				}
				first, err := workCall(t, b, "record", map[string]any{"receipt": p["receipt"], "observations": []any{map[string]any{"kind": "test", "title": "\u5408\u6210\u306e\u7d42\u4e86\u78ba\u8a8d", "content": "\u5408\u6210\u306e\u7d42\u4e86\u6761\u4ef6\u3092\u78ba\u8a8d\u3057\u305f\u3002"}}, "result": map[string]any{"changed": "\u5408\u6210\u4f5c\u696d\u306e\u7d50\u679c\u3092\u4e00\u5ea6\u3060\u3051\u4fdd\u5b58\u3059\u308b\u3002", "evidence": []string{"new1"}}})
				workAfterWrite = old
				if err != nil {
					t.Fatal("\u6700\u521d\u306e\u4fdd\u5b58\u53d7\u4ed8\u5931\u6557", err)
				}
				if point != "none" && !injected {
					t.Fatal("\u6307\u5b9a\u3057\u305f\u7d42\u4e86\u4fdd\u5b58\u4f4d\u7f6e\u306b\u5230\u9054\u3057\u3066\u3044\u306a\u3044")
				}
				original := loopObject(t, first["storage"])["result_source"]
				before := loadSession(root, session)
				t.Logf("\u4e2d\u65ad\u4f4d\u7f6e=%s \u518d\u6e96\u5099=%v \u6700\u521dcomplete=%v \u5143\u7d50\u679c\u539f\u8cc7\u6599=%d \u7d42\u4e86\u539f\u8cc7\u6599=%d \u672a\u51e6\u7406\u53d6\u5f97=%d \u4e16\u4ee3=%d \u7d50\u679c\u4e16\u4ee3=%d", point, renew, first["complete"], auditR2Count(t, b, "Work result and remaining conditions"), auditR2Count(t, b, "Evidence-linked work judgment and ending review"), len(before.Reasoning.PendingCaptures), before.Reasoning.Generation, before.Reasoning.Result.ReviewGeneration)
				if renew {
					p, err = workCall(t, b, "prepare", map[string]any{"session_id": session, "turn_id": loadSession(root, session).TurnID, "query": "\u5408\u6210"})
					if err != nil {
						t.Fatal("\u518d\u6e96\u5099\u5931\u6557", err)
					}
				}
				replay, err := workCall(t, b, "record", map[string]any{"receipt": p["receipt"], "result": map[string]any{"ref": original}})
				if err != nil {
					t.Fatal("ref\u56de\u5fa9\u306e\u53d7\u4ed8\u5931\u6557", err)
				}
				after := loadSession(root, session)
				stop := verifyStopReasoning(root, after, after)
				complete := loopObject(t, replay["reasoning"])["complete"]
				t.Logf("ref\u56de\u5fa9complete=%v reasoning.complete=%v \u7d42\u4e86\u53d7\u4ed8=%v \u5143\u7d50\u679c\u539f\u8cc7\u6599=%d \u7d42\u4e86\u539f\u8cc7\u6599=%d \u672a\u51e6\u7406\u53d6\u5f97=%d errors=%v", replay["complete"], complete, stop == nil, auditR2Count(t, b, "Work result and remaining conditions"), auditR2Count(t, b, "Evidence-linked work judgment and ending review"), len(after.Reasoning.PendingCaptures), replay["errors"])
				if replay["complete"] != true || complete != true || stop != nil {
					t.Error("\u5185\u90e8\u306e\u7d42\u4e86\u4fdd\u5b58\u306e\u4e2d\u65ad\u3092\u539f\u8a18\u9332\u306e\u518d\u9001\u306a\u3057\u3067\u56de\u5fa9\u3067\u304d\u306a\u3044")
				}
				if auditR2Count(t, b, "Evidence-linked work judgment and ending review") != 1 {
					t.Error("a retried ending must retain one immutable source")
				}
				if auditR2Count(t, b, "Work result and remaining conditions") != 1 {
					t.Error("\u5143\u306e\u7d50\u679c\u539f\u8a18\u9332\u304c\u5897\u6b96\u3057\u305f")
				}
			})
		}
	}
}

func TestWorkUnknownQualityResultRefRetainsOptionalEvidence(t *testing.T) {
	for _, proof := range []bool{false, true} {
		for _, renew := range []bool{false, true} {
			t.Run(fmt.Sprintf("proof_%v_renew_%v", proof, renew), func(t *testing.T) {
				repo, root, _ := planningFixture(t)
				session := "audit-r5-unknown-quality"
				b := notebook.Book{Root: root, Scope: feedbackScope(repo)}
				runUnreviewedHook(t, "user-prompt-submit", map[string]any{"session_id": session, "cwd": repo, "prompt": "\u54c1\u8cea\u8a55\u4fa1\u306e\u7121\u3044\u5408\u6210\u5831\u544a\u3092\u4fdd\u5b58\u3059\u308b\u3002"})
				p, err := workCall(t, b, "prepare", map[string]any{"session_id": session, "turn_id": loadSession(root, session).TurnID, "query": "\u5408\u6210"})
				if err != nil {
					t.Fatal("\u6e96\u5099\u5931\u6557", err)
				}
				result := map[string]any{"changed": "\u5408\u6210\u5831\u544a\u3092\u4fdd\u5b58\u3059\u308b\u3002\u54c1\u8cea\u306f\u672a\u8a55\u4fa1\u3002"}
				fields := map[string]any{"receipt": p["receipt"], "result": result}
				if proof {
					fields["observations"] = []any{map[string]any{"kind": "test", "title": "\u5408\u6210\u306e\u8a18\u9332\u78ba\u8a8d", "content": "\u5408\u6210\u7d50\u679c\u306e\u8a18\u9332\u72b6\u614b\u3092\u78ba\u8a8d\u3057\u305f\u3002"}}
					result["evidence"] = []string{"new1"}
				}
				first, err := workCall(t, b, "record", fields)
				if err != nil || first["complete"] != true {
					t.Fatal("\u6700\u521d\u306e\u4fdd\u5b58\u5931\u6557", err)
				}
				original := loopObject(t, first["storage"])["result_source"]
				if renew {
					p, err = workCall(t, b, "prepare", map[string]any{"session_id": session, "turn_id": loadSession(root, session).TurnID, "query": "\u5408\u6210"})
					if err != nil {
						t.Fatal("\u518d\u6e96\u5099\u5931\u6557", err)
					}
				}
				replay, err := workCall(t, b, "record", map[string]any{"receipt": p["receipt"], "result": map[string]any{"ref": original}})
				if err != nil {
					t.Fatal("ref\u518d\u9001\u53d7\u4ed8\u5931\u6557", err)
				}
				t.Logf("\u8a3c\u62e0\u3042\u308a=%v \u518d\u6e96\u5099=%v \u6700\u521dcomplete=%v ref\u518d\u9001complete=%v reasoning.complete=%v \u539f\u8cc7\u6599=%d errors=%v", proof, renew, first["complete"], replay["complete"], loopObject(t, replay["reasoning"])["complete"], auditR2Count(t, b, "Work result and remaining conditions"), replay["errors"])
				if replay["complete"] != true {
					t.Error("\u672a\u8a55\u4fa1\u3067\u8a3c\u62e0\u3092\u8981\u6c42\u3057\u306a\u3044\u6b63\u5e38\u5831\u544a\u306eref\u518d\u9001\u3092\u62d2\u5426\u3057\u305f")
				}
			})
		}
	}
}
