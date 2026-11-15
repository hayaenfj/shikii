package main

import (
	"encoding/json"
	"strings"
	"testing"
)

// On 2026-11-15 Codex was refused twice in a way that did not say what to send. A capture with observed_at "2026-11-15" was answered
// in Go's words ('parsing time "2026-11-15" as "2006-01-02T15:04:05Z07:00": cannot parse "" as "T"'). A record whose
// work.maintenance[0] held the page_id it had read in the prepared maintenance task was told '"page_id" is a field of maintenance',
// the argument of another operation, and Codex reviewed the page through that operation instead of the task prepare had given it.
// The refusals now name the field and its form, and where a maintenance review's ref comes from.
func TestRefusalsSayWhatToSendForCodexsTwoMistakes(t *testing.T) {
	send, _ := mcpSession(t, "true", map[string]float64{"change_request": 0.9, "question": 0.1, "other": 0})
	result(t, send(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize"}))
	refusal := func(arguments map[string]any) string {
		t.Helper()
		got := result(t, send(map[string]any{"jsonrpc": "2.0", "id": 2, "method": "tools/call", "params": map[string]any{"name": "shikii_wiki", "arguments": arguments}}))
		text := got["content"].([]any)[0].(map[string]any)["text"].(string)
		var r struct {
			Error string `json:"error"`
		}
		if got["isError"] != true || json.Unmarshal([]byte(text), &r) != nil || r.Error == "" {
			t.Fatalf("the call must be refused: %s", text)
		}
		return r.Error
	}

	date := refusal(map[string]any{"operation": "capture", "observation": map[string]any{"kind": "test", "title": "Synthetic validation", "content": "Three archives were checked.", "reference": "evidence.txt", "version": "read 2026-11-15", "observed_at": "2026-11-15"}})
	for _, w := range []string{"observation.observed_at", "RFC 3339", "2026-11-15T", `"2026-11-15"`} {
		if !strings.Contains(date, w) {
			t.Errorf("the refusal of a date without a time does not say %s: %s", w, date)
		}
	}
	t.Log(date)
	if strings.Contains(date, "2006-01-02T15:04:05Z07:00") {
		t.Errorf("the refusal is in Go's words: %s", date)
	}

	page := refusal(map[string]any{"operation": "record", "work": map[string]any{"receipt": "eyJzIjoieCJ9", "maintenance": []any{map[string]any{"page_id": "325c0267cfe4ce6d", "reason": "Read the page; no change."}}}})
	for _, w := range []string{"work.maintenance[0]", "maintenance1", "prepare"} {
		if !strings.Contains(page, w) {
			t.Errorf("the refusal of page_id in a maintenance review does not say %s: %s", w, page)
		}
	}
	t.Log(page)
	if strings.Contains(page, `"page_id" is a field of`) {
		t.Errorf("the refusal sends the agent to another operation's argument: %s", page)
	}
}
