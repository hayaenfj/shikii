package main

import (
	"strings"
	"testing"

	"github.com/hayaenfj/shikii/internal/notebook"
)

// Second confirmation audit of 0.34.0 (U-1): a record quoting the person's words after the first 4000 bytes of the message was
// refused with "Copy it from the original text query returned", but query is not a step of the record flow, and the words could not
// be quoted from any reply. The refusal of a record names the quote by its field and ref, says that the ref holds the first 4000
// bytes of the message, and what to send instead; sending that records. How much of the message can be quoted does not change.
func TestARefusedQuoteOfARecordSaysWhereItsWordsComeFrom(t *testing.T) {
	repo, root, _ := planningFixture(t)
	const head, tail = "Compare the export formats.", "TAIL_CONDITION never publish this report."
	prompt := head + " " + strings.Repeat("x", 4100) + " " + tail
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": prompt})
	s := loadSession(root, "planning")
	b := notebook.Book{Root: root, Scope: feedbackScope(repo)}
	got, err := workCall(t, b, "prepare", map[string]any{"session_id": "planning", "turn_id": s.TurnID, "query": "formats"})
	if err != nil {
		t.Fatal(err)
	}
	current, _ := got["conversation"].(map[string]any)["current"].(string)
	if !strings.HasPrefix(current, head) || strings.Contains(current, tail) {
		t.Fatalf("precondition: conversation.current is the head of the message without its tail (%d bytes)", len(current))
	}
	quote := func(text string) map[string]any {
		return map[string]any{"summary": "Compare the formats.", "quotes": []any{map[string]any{"ref": "current", "text": text}}}
	}
	// record returns the refusal of the plan, which comes back in errors of the reply, or "" when the plan is saved.
	record := func(agreed, open map[string]any) string {
		r, err := workCall(t, b, "record", map[string]any{"receipt": got["receipt"], "agreed": agreed, "open": open})
		if err != nil {
			return err.Error()
		}
		errs, _ := r["errors"].([]any)
		for _, e := range errs {
			if e, _ := e.(map[string]any); e["kind"] == "plan" {
				return e["error"].(string)
			}
		}
		if saved, _ := r["saved"].([]any); len(saved) == 0 {
			t.Fatalf("the record neither saved the plan nor refused it: %v", r)
		}
		return ""
	}
	for field, refusal := range map[string]string{
		"agreed": record(quote(tail), map[string]any{"unknown": true}),
		"open":   record(quote(head), quote(tail)),
	} {
		if refusal == "" {
			t.Fatalf("a quote of %s from after the first 4000 bytes is recorded", field)
		}
		for _, want := range []string{field + ".quotes[0] (ref current)", "the first 4000 bytes of the message", "conversation.current"} {
			if !strings.Contains(refusal, want) {
				t.Errorf("the refusal of %s lacks %q: %s", field, want, refusal)
			}
		}
		if strings.Contains(refusal, "query") {
			t.Errorf("the refusal of %s names query, which is not a step of the record flow: %s", field, refusal)
		}
	}
	// What the refusal says to send: words copied from conversation.current.
	if refusal := record(quote(head), map[string]any{"unknown": true}); refusal != "" {
		t.Fatalf("a quote copied from conversation.current is refused: %s", refusal)
	}
	// The bound the refusal names is the one the prepare reply gives the ref.
	if limit := got["conversation_context"].(map[string]any)["current"].(map[string]any)["limit_bytes"]; limit != float64(planningQuoteBytes) {
		t.Errorf("conversation_context.current.limit_bytes is %v, the refusal says %d", limit, planningQuoteBytes)
	}
	// A history ref holds less of its exchange, and the refusal says so; hook plan submit, whose query returns the sources, keeps its
	// refusal.
	history := "history:0123456789abcdef:user"
	_, err = renderPlanningFact(&planningFact{Summary: "s", Evidence: []planningQuote{{Source: history, Quote: "missing", ref: "history1"}}}, map[string]string{history: "Earlier request."}, false)
	if err == nil || !strings.Contains(err.Error(), "open.quotes[0] (ref history1)") || !strings.Contains(err.Error(), "the first 1200 bytes of the message") || !strings.Contains(err.Error(), "conversation.history1") {
		t.Errorf("the refusal of a history quote: %v", err)
	}
	_, err = renderPlanningFact(&planningFact{Summary: "s", Evidence: []planningQuote{{Source: "current_user", Quote: "missing"}}}, map[string]string{"current_user": "Now."}, true)
	if err == nil || !strings.Contains(err.Error(), "evidence[0]") || !strings.Contains(err.Error(), "query returned") {
		t.Errorf("the refusal of hook plan submit changed: %v", err)
	}
}
