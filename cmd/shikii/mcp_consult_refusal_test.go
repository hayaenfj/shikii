package main

import (
	"encoding/json"
	"strings"
	"testing"
)

// Audit of 0.33.0 (U-5): over MCP the refusal of consult and reuse said "prepare the task, then call prepare again with its
// receipt". An agent holding the hook's preparation that did so replaced it, and its record by the hook's ids was refused after
// that. The refusal now says to add the proposal or the conditions to the receipt the agent holds, the one in the hook's prepared
// work included, and names the CLI's shikii wiki consult and reuse for a task too long for those modes. Followed as written, the
// hook's preparation stays: the record by its ids goes through. The skill's references said the same as the old refusal (recording.md:
// "use the individual wiki operations", which MCP refuses; experience.md: "prepare the task, then call prepare again").
func TestMCPRefusalOfConsultAndReuseKeepsTheHookPreparation(t *testing.T) {
	recording := lineWith(t, "../../skills/shikii/references/recording.md", "Consult and reuse accept at most")
	experience := lineWith(t, "../../skills/shikii/references/experience.md", "MCP has no `consult` operation")
	repo, root, _ := planningFixture(t)
	h := newServer(serveOpts{storeDir: root, projectScope: feedbackScope(repo)})
	out := runUnreviewedHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": planningTestRequest})
	_, encoded, found := strings.Cut(feedbackContext(t, out), "shikii prepared work:\n")
	var prepared map[string]any
	if !found || json.Unmarshal([]byte(encoded), &prepared) != nil || prepared["receipt"] == nil {
		t.Fatalf("precondition: the hook prepared the work with a receipt: %.400s", out)
	}
	turn := loadSession(root, "planning").TurnID
	refusal := func(arguments string) string {
		t.Helper()
		params, _ := json.Marshal(map[string]any{"name": "shikii_wiki", "arguments": json.RawMessage(arguments)})
		reply := handleRPC(rpcRequest{JSONRPC: "2.0", ID: json.RawMessage("1"), Method: "tools/call", Params: params}, h)
		result, _ := reply["result"].(map[string]any)
		var body struct {
			Error string `json:"error"`
		}
		if result == nil || result["isError"] != true || json.Unmarshal([]byte(result["content"].([]map[string]any)[0]["text"].(string)), &body) != nil {
			t.Fatalf("%s must be refused: %v", arguments, reply)
		}
		return body.Error
	}

	for _, c := range []struct{ op, field, value string }{{"consult", "proposal", "Keep the filters in the export"}, {"reuse", "conditions", "Linux, an ordinary user"}} {
		text := refusal(`{"operation":"` + c.op + `"}`)
		for _, w := range []string{`work:{"receipt":"<receipt>","` + c.field + `":`, "the receipt in the hook's prepared work", "replaces that preparation",
			"shikii wiki " + c.op, `"request":"<scoped task>"`} {
			if !strings.Contains(text, w) {
				t.Errorf("the refusal of %s lacks %q: %s", c.op, w, text)
			}
		}
		if strings.Contains(text, "prepare the task, then") {
			t.Errorf("the refusal of %s tells an agent with the hook's preparation to prepare the task again: %s", c.op, text)
		}
		// Follow it with the hook's receipt.
		args, _ := json.Marshal(map[string]any{"operation": "prepare", "work": map[string]any{"receipt": prepared["receipt"], c.field: c.value}})
		mcpToolCall(t, h, "shikii_wiki", string(args))
	}
	args, _ := json.Marshal(map[string]any{"operation": "record", "work": map[string]any{"session_id": "planning", "turn_id": turn, "result": map[string]any{"changed": "Explained the CSV export."}}})
	got, _ := mcpToolCall(t, h, "shikii_wiki", string(args))
	if saved, _ := got["structuredContent"].(map[string]any); saved["complete"] != true {
		t.Fatalf("after the refusal was followed, the hook's ids still record its preparation: %v", got)
	}

	for _, w := range []string{"shikii wiki consult", "shikii wiki reuse"} {
		if !strings.Contains(recording, w) {
			t.Errorf("recording.md's way for a task too long for consult and reuse does not name %s: %s", w, recording)
		}
	}
	if strings.Contains(recording, "individual wiki operations") {
		t.Errorf("recording.md sends an agent to the individual wiki operations, which MCP refuses for consult and reuse: %s", recording)
	}
	if strings.Contains(experience, "prepare the task, then") || !strings.Contains(experience, "hook's prepared work") {
		t.Errorf("experience.md tells an agent with the hook's preparation to prepare the task again: %s", experience)
	}
}
