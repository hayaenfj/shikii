package main

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hayaenfj/shikii/internal/store"
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

// leftDelivery saves a delivery of one lesson (a past conversation quoted as feedback) to the delivery id, with parent when it went to a subagent.
func leftDelivery(t *testing.T, st store.Store, scope, id, parent string) string {
	t.Helper()
	key := "feedback:00000000000000aa"
	d := store.MemoryDelivery{ID: id, Scope: scope, Request: "fix the README", Parent: parent, Quotes: []store.MemoryQuote{{Key: key, ID: "00000000000000aa", Request: "earlier request", Summary: "earlier summary", Reply: "earlier reply"}}}
	if parent != "" {
		d.Agent = "subagent-1"
	}
	if err := st.SaveMemoryDelivery(d); err != nil {
		t.Fatal(err)
	}
	return key
}

// A submit that only assesses delivered lessons names that conversation, so left says what else is left for it.
func TestReflectSubmitLeftNamesAssessedConversations(t *testing.T) {
	st, scope, h := reviewFixture(t)
	id, _ := reviewSeed(t, st, scope, 1)
	key := leftDelivery(t, st, scope, id, "")
	out := reflectCall(t, h, map[string]any{"operation": "submit", "memory": []map[string]string{{"source_id": id, "key": key, "result": "unknown", "evidence": "not checked"}}})
	left, _ := out["left"].([]any)
	if out["complete"] != true || len(left) != 1 || !strings.Contains(left[0].(string), id) || !strings.Contains(left[0].(string), "provisional scores 1") {
		t.Fatalf("a memory-only submit says what is left for its conversation: %v", out)
	}
}

// When left cannot be worked out, the receipt says so in left_error instead of dropping left as if nothing were left.
func TestReflectSubmitSaysWhenLeftFails(t *testing.T) {
	st, scope, h := reviewFixture(t)
	id, _ := reviewSeed(t, st, scope, 1)
	key := leftDelivery(t, st, scope, id, "")
	// a broken provisional memory assessment makes the reflect view (and so left) fail; the wiki-only submit itself does not read it
	sum := func(s string) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }
	dir := filepath.Join(st.Dir, "records", "memory-reviews", sum(scope))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, sum(id+":"+key)+".json"), []byte("{broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	out := reflectCall(t, h, map[string]any{"operation": "submit", "wiki": []map[string]any{{"sources": []string{id}}}})
	msg, _ := out["left_error"].(string)
	if out["complete"] != true || out["left"] != nil || !strings.Contains(msg, "could not work out what is left") || !strings.Contains(msg, "query") {
		t.Fatalf("a failure to work out left is said, not swallowed: %v", out)
	}
}

// A lesson delivered to a subagent is assessed with the delivery's own source_id, which is not the parent conversation's id.
// left still names the parent conversation (the reflect view gathers the delivery under its parent).
func TestReflectSubmitLeftMapsSubagentDeliveryToParent(t *testing.T) {
	st, scope, h := reviewFixture(t)
	parent, _ := reviewSeed(t, st, scope, 1)
	delivery := "00000000000000bb"
	key := leftDelivery(t, st, scope, delivery, parent)
	out := reflectCall(t, h, map[string]any{"operation": "submit", "memory": []map[string]string{{"source_id": delivery, "key": key, "result": "unknown", "evidence": "not checked"}}})
	left, _ := out["left"].([]any)
	if out["complete"] != true || len(left) != 1 || !strings.Contains(left[0].(string), parent) || !strings.Contains(left[0].(string), "provisional scores 1") {
		t.Fatalf("assessing a subagent's delivery says what is left for the parent conversation: %v", out)
	}
}
