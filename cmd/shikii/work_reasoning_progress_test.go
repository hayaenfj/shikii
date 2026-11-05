package main

import (
	"errors"
	"strings"
	"testing"
)

func TestReasoningPartialRecoveryMakesObservableProgress(t *testing.T) {
	for _, kind := range []string{"notes", "captures"} {
		t.Run(kind, func(t *testing.T) {
			repo, root, b, p := reasoningFixture(t)
			saveReasoning(t, b, p["receipt"], reasoningInput("start", "current"), nil)
			note := func(title string, sources []string) map[string]any {
				return map[string]any{"title": title, "lesson": "Keep the checked condition.", "applies_to": title, "kind": "fact", "scene": "synthetic progress", "sources": sources}
			}
			a := map[string]any{"kind": "test", "title": "Progress source A", "content": "Condition A."}
			c := map[string]any{"kind": "test", "title": "Progress source B", "content": "Condition B."}
			if kind == "notes" {
				v, e := workCall(t, b, "record", map[string]any{"receipt": p["receipt"], "notes": []any{note("Progress note A", []string{"missing"}), note("Progress note B", []string{"missing"})}})
				if e != nil || v["complete"] != false {
					t.Fatal("\u8907\u6570note\u5931\u6557\u3092\u4f5c\u308c\u306a\u3044", e)
				}
			} else {
				old := workAfterWrite
				workAfterWrite = func(kind string) error {
					if kind == "observation" {
						return errors.New("\u5408\u6210\u4e2d\u65ad")
					}
					return nil
				}
				v, e := workCall(t, b, "record", map[string]any{"receipt": p["receipt"], "observations": []any{a, c}})
				workAfterWrite = old
				if e != nil || v["complete"] != false {
					t.Fatal("\u8907\u6570capture\u4e2d\u65ad\u3092\u4f5c\u308c\u306a\u3044", e)
				}
			}
			initial := loadSession(root, "reasoning")
			n := len(initial.Reasoning.RequiredNotes) + len(initial.Reasoning.PendingCaptures)
			if n != 2 {
				t.Fatal("\u7fa9\u52d9\u304c2\u4ef6\u3067\u306f\u306a\u3044", n)
			}
			first := runUnreviewedHook(t, "stop", map[string]any{"session_id": "reasoning", "cwd": repo, "last_assistant_message": "\u5185\u90e8\u4fdd\u5b58\u306e\u5fa9\u65e7\u304c\u6b8b\u3063\u3066\u3044\u307e\u3059\u3002"})
			if !strings.Contains(first, "reasoning-before-end") {
				t.Fatal("\u6700\u521d\u306e\u505c\u6b62\u3092\u4f5c\u308c\u306a\u3044")
			}
			before := loadSession(root, "reasoning").RitualRecovery.Left
			input := map[string]any{"receipt": p["receipt"]}
			if kind == "notes" {
				input["notes"] = []any{note("Progress note A", []string{"current"})}
			} else {
				input["observations"] = []any{a}
			}
			repaired, e := workCall(t, b, "record", input)
			if e != nil || repaired["complete"] != true {
				t.Fatal("1\u4ef6\u306e\u5b9f\u4fdd\u5b58\u304c\u3067\u304d\u306a\u3044", e)
			}
			state := loadSession(root, "reasoning")
			left := reasoningLeft(state.Reasoning)
			actual := len(state.Reasoning.RequiredNotes) + len(state.Reasoning.PendingCaptures)
			if actual != 1 {
				t.Fatal("\u5b9f\u969b\u306e\u7fa9\u52d9\u304c1\u4ef6\u3078\u6e1b\u3089\u306a\u3044", actual)
			}
			second := runUnreviewedHook(t, "stop", map[string]any{"session_id": "reasoning", "cwd": repo, "last_assistant_message": "1\u4ef6\u3092\u4fdd\u5b58\u3057\u3001\u6b8b\u308b\u5185\u90e8\u4fdd\u5b58\u3092\u5fa9\u65e7\u3057\u307e\u3059\u3002"})
			halt := strings.Contains(second, "\"continue\":false")
			t.Logf("kind=%s \u5b9f\u969b\u306e\u7fa9\u52d92\u4ef6\u304b\u3089=%d \u521d\u56de\u6b8b\u4ef6=%d \u90e8\u5206\u4fdd\u5b58\u5f8c\u6b8b\u4ef6=%d \u81ea\u52d5\u7d99\u7d9a\u6253\u5207\u308a=%v", kind, actual, before, left, halt)
			if left >= before || halt || !strings.Contains(second, "\"decision\":\"block\"") {
				t.Error("RR-002: \u5b9f\u969b\u306e\u90e8\u5206\u4fdd\u5b58\u306e\u9032\u6357\u3092\u6b8b\u4ef6\u3078\u53cd\u6620\u3057\u306a\u3044")
			}
			remainingInput := map[string]any{"receipt": p["receipt"]}
			if kind == "notes" {
				remainingInput["notes"] = []any{note("Progress note B", []string{"current"})}
			} else {
				remainingInput["observations"] = []any{c}
			}
			finalSaved, e := workCall(t, b, "record", remainingInput)
			if e == nil && finalSaved["complete"] == false && kind == "notes" {
				pending, ok := finalSaved["pending"].([]any)
				if ok && len(pending) == 1 {
					finalSaved, e = workCall(t, b, "record", map[string]any{"receipt": p["receipt"], "notes": []any{map[string]any{"ref": loopObject(t, pending[0])["ref"], "target": "new"}}})
				}
			}
			if e != nil || finalSaved["complete"] != true {
				t.Fatal("\u6b8b\u308b1\u4ef6\u3092\u4fdd\u5b58\u3067\u304d\u306a\u3044", e)
			}
			finish := reasoningInput("finish", "current")
			finish["result"] = "\u6b8b\u308b1\u4ef6\u3082\u5b9f\u969b\u306b\u4fdd\u5b58\u3057\u3066\u78ba\u8a8d\u3057\u305f\u3002"
			complete := saveReasoning(t, b, p["receipt"], finish, nil)
			if loopObject(t, complete["reasoning"])["complete"] != true {
				t.Fatal("\u5168\u4fdd\u5b58\u5f8c\u306efinish\u304c\u5b8c\u4e86\u3057\u306a\u3044")
			}
			finalStop := runUnreviewedHook(t, "stop", map[string]any{"session_id": "reasoning", "cwd": repo, "last_assistant_message": "\u5fc5\u8981\u306a2\u4ef6\u3092\u4fdd\u5b58\u3057\u3066\u78ba\u8a8d\u3057\u307e\u3057\u305f\u3002"})
			if strings.Contains(finalStop, "\"continue\":false") || strings.Contains(finalStop, "\"decision\":\"block\"") || !loadSession(root, "reasoning").Judged {
				t.Fatal("\u5168\u4fdd\u5b58\u5f8c\u306b\u7d42\u4e86\u3067\u304d\u306a\u3044")
			}
			t.Log("\u6b8b\u308a\u306e\u5b9f\u4fdd\u5b58\u3068finish\u5f8c\u306fStop\u304c\u6b63\u5e38\u7d42\u4e86\u3057\u305f")

		})
	}
}
