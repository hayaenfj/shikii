package main

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"github.com/hayaenfj/shikii/internal/notebook"
)

// On 2026-11-15 OpenCode 2.3.0 (glm-5.3) copied its receipt with one character changed: the request id inside it read 7d8f...
// where prepare had returned 7e8f..., while the work nonce was the current one. shikii refused the record with "a newer request has
// started in this session", which was not true, and the agent prepared the task again from the start. A receipt whose work is the
// current one but whose request id is not was changed while being copied: the refusal says so and asks for the receipt exactly as
// prepare returned it. A receipt of an earlier request, whose work is not the current one, keeps the old refusal
// (TestWorkRefusalsNameTheCauseAndTheNextCall).
func TestACopiedReceiptWithOneCharacterChangedIsNotCalledAnEarlierRequest(t *testing.T) {
	repo, root, _ := planningFixture(t)
	b := notebook.Book{Root: root, Scope: feedbackScope(repo)}
	prepared, err := workCall(t, b, "prepare", map[string]any{"query": "CSV export", "request": "Check the CSV export."})
	if err != nil {
		t.Fatal(err)
	}
	receipt := prepared["receipt"].(string)
	token, err := workDecode(receipt)
	if err != nil {
		t.Fatal(err)
	}
	changed := []byte(token.Turn)
	changed[1] ^= 1 // one character of the request id, as the agent's copy had it
	token.Turn = string(changed)
	raw, _ := json.Marshal(token)
	miscopied := base64.RawURLEncoding.EncodeToString(raw)

	result := map[string]any{"changed": "Nothing.", "checked": "Read the export."}
	_, err = workCall(t, b, "record", map[string]any{"receipt": miscopied, "result": result})
	if err == nil {
		t.Fatal("a receipt with a changed request id must be refused")
	}
	if strings.Contains(err.Error(), "a newer request has started") || !strings.Contains(err.Error(), "exactly as prepare returned it") {
		t.Fatalf("the refusal must say the receipt was changed while copying, not that a newer request started: %v", err)
	}
	if _, err := workCall(t, b, "record", map[string]any{"receipt": receipt, "result": result}); err != nil {
		t.Fatalf("the receipt prepare returned still records after the refusal: %v", err)
	}
}
