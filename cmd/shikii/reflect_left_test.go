package main

import (
	"os"
	"strings"
	"testing"
)

// Behaviour check 0.9.2 (Claude Code): a submit that sorted the wiki but sent no provisional scores came back complete:true,
// so the agent stopped while the same two conversations stayed pending and the Stop hook blocked every turn.
// The receipt now says what is still left for the conversations it named (complete still only says every item was saved).
func TestReflectSubmitSaysWhatIsStillLeft(t *testing.T) {
	st, scope, h := reviewFixture(t)
	id, trace := reviewSeed(t, st, scope, 1)
	out := reflectCall(t, h, map[string]any{"operation": "submit", "wiki": []map[string]any{{"sources": []string{id}}}})
	left, _ := out["left"].([]any)
	if out["complete"] != true || len(left) != 1 || !strings.Contains(left[0].(string), id) || !strings.Contains(left[0].(string), "provisional scores 1") {
		t.Fatalf("a submit that leaves provisional scores says so in left: %v", out)
	}
	out = reflectCall(t, h, map[string]any{"operation": "submit", "reviews": []map[string]string{{"source_id": id, "trace_id": trace, "truth": "yes", "reason": "checked"}}})
	if out["complete"] != true || out["left"] != nil {
		t.Fatalf("once nothing is left, left is gone: %v", out)
	}
}

// A submit that names a conversation only through its reviews still says what is left (here, the wiki).
func TestReflectSubmitLeftNamesReviewedConversations(t *testing.T) {
	st, scope, h := reviewFixture(t)
	id, trace := reviewSeed(t, st, scope, 1)
	out := reflectCall(t, h, map[string]any{"operation": "submit", "reviews": []map[string]string{{"source_id": id, "trace_id": trace, "truth": "yes", "reason": "checked"}}})
	left, _ := out["left"].([]any)
	if out["complete"] != true || len(left) != 1 || !strings.Contains(left[0].(string), id) || !strings.Contains(left[0].(string), "wiki unsorted") {
		t.Fatalf("a reviews-only submit says the wiki is still left: %v", out)
	}
}

// complete:true only says every item was saved. Every text an agent reads about submit must say to do what left lists,
// or the agent stops on complete:true as before (behaviour check 0.9.2).
func TestReflectTextsSayToDoWhatIsLeft(t *testing.T) {
	texts := map[string]string{"hook notice": reflectAsk("shikii")}
	for _, tool := range mcpTools {
		if tool["name"] == "shikii_reflect" {
			texts["MCP description"], _ = tool["description"].(string)
		}
	}
	for _, path := range []string{"../../skills/shikii/SKILL.md", "../../skills/shikii/references/review.md"} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		texts[path] = string(data)
	}
	for name, text := range texts {
		if !strings.Contains(text, "left") || !strings.Contains(text, "complete") {
			t.Errorf("%s does not say that complete:true is not the end while left lists a conversation", name)
		}
	}
}
