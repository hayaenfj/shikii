package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/hayaenfj/shikii/internal/notebook"
)

// audit D (0.31.0, second round), M35 (work_record_reflection.go, the field named pending[].memories[] again): the refusal of a
// memory reference the receipt did not prepare names the field of the reply that holds the references. The field is read from
// the reply of prepare here, not written into the test.
func TestAuditDGap_TheRefusalNamesTheFieldTheReplyHas(t *testing.T) {
	st, scope, _, _, _ := memoryEffectFixture(t)
	memoryEffectPrompt(t, scope, "s1")
	memoryEffectComplete(t, st, scope, "s1")
	b := notebook.Book{Root: st.Dir, Scope: scope}
	prepared, err := workCall(t, b, "prepare", map[string]any{"session_id": "s1", "turn_id": loadSession(st.Dir, "s1").TurnID, "query": "README"})
	if err != nil {
		t.Fatal(err)
	}
	field, refs := "", []string{}
	pending, _ := prepared["pending"].([]any)
	for _, raw := range pending {
		entry, _ := raw.(map[string]any)
		for _, name := range []string{"memory", "memories"} {
			list, _ := entry[name].([]any)
			for _, item := range list {
				if ref, _ := item.(map[string]any)["ref"].(string); ref != "" {
					field = "pending[]." + name + "[]"
					refs = append(refs, ref)
				}
			}
		}
	}
	if field == "" {
		t.Fatalf("precondition: a pending conversation carries memory references: %v", prepared["pending"])
	}
	saved, err := workCall(t, b, "record", map[string]any{"receipt": prepared["receipt"], "memory": []any{map[string]any{"ref": "knowledge1", "result": "helpful", "decision": "d", "evidence": "e"}}})
	raw, _ := json.Marshal(saved["errors"])
	if err != nil || !strings.Contains(string(raw), "the refs of "+field+" in the reply of prepare: "+strings.Join(refs, ", ")) {
		t.Errorf("M35: the references %v are in %s of the reply, and the refusal says: %v %s", refs, field, err, raw)
	}
}
