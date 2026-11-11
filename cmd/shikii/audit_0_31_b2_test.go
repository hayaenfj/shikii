package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hayaenfj/shikii/internal/notebook"
)

// More probes of audit B (0.31.0). Both only printed what the candidate did; each now fails when what it shows is broken.

// The Stop block for a turn that changed a file and prepared nothing: what skills/shikii/references/planning.md says Stop asks for.
func TestAuditB_StopBlockAfterAChangedFile(t *testing.T) {
	repo, _, _ := planningFixture(t)
	t.Chdir(repo)
	runUnreviewedHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": planningTestRequest})
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("# changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	reason, blocked := stopReason(t, runUnreviewedHook(t, "stop", map[string]any{"session_id": "planning", "cwd": repo, "last_assistant_message": "Done."}))
	t.Logf("blocked=%v\n%s", blocked, reason)
	if !blocked {
		t.Fatalf("finishing with a changed file and nothing prepared is stopped: %s", reason)
	}
	// INVARIANT 7: one flow, work prepare then work record, with the way to call it through the MCP tool
	for _, w := range []string{" work prepare", "the same way to work record", "the MCP tool shikii_wiki, operation prepare", "MCP: operation record"} {
		if !strings.Contains(reason, w) {
			t.Errorf("the block lacks %q:\n%s", w, reason)
		}
	}
	// The legacy flow is named only to say that it is not run. Checked as what is allowed, not as a list of the old sentences:
	// with that one phrase taken out, the block does not name hook plan at all (planning.md said Stop requests hook plan submit).
	const allowed = "do not also run hook plan query, which replaces its receipt"
	if rest := strings.ReplaceAll(reason, allowed, ""); !strings.Contains(reason, allowed) || strings.Contains(rest, "hook plan") {
		t.Errorf("the block names hook plan other than to say %q:\n%s", allowed, reason)
	}
}

func TestAuditB_OutputNamesKnowledgeQuery2(t *testing.T) {
	st, scope, _, draft, _ := memoryEffectFixture(t)
	b := notebook.Book{Root: st.Dir, Scope: scope}
	for i := 0; i < 4; i++ {
		body := strings.Repeat("Check the setup steps of the README on a machine that has no project yet. ", 120)
		if _, err := b.Ingest(notebook.Draft{Sources: draft.Sources, Title: "README setup steps " + string(rune('A'+i)), Lesson: "Check the README setup steps on a fresh machine.", AppliesTo: "README \u306e\u5c0e\u5165\u624b\u9806\u3092\u66f4\u65b0\u3057\u3066\u304f\u3060\u3055\u3044", Kind: "rule", Scene: "\u30c6\u30b9\u30c8\u306e\u6642", Body: &body}); err != nil {
			t.Fatal(err)
		}
	}
	for _, session := range []string{"s1", "s2", "s3"} {
		memoryEffectPrompt(t, scope, session)
		memoryEffectComplete(t, st, scope, session)
	}
	runUnreviewedHook(t, "user-prompt-submit", map[string]any{"session_id": "s4", "cwd": scope, "prompt": "README \u306e\u5c0e\u5165\u624b\u9806\u3092\u3082\u3046\u4e00\u5ea6\u898b\u76f4\u3057\u3066\u304f\u3060\u3055\u3044"})
	shortened := false
	for _, bound := range []int{8192, 10000, 12000, 16000, 20000} {
		prepared, err := workCall(t, b, "prepare", map[string]any{"session_id": "s4", "turn_id": loadSession(st.Dir, "s4").TurnID, "query": "README \u5c0e\u5165\u624b\u9806", "max_bytes": bound})
		if err != nil {
			t.Logf("max_bytes %d: %v", bound, err)
			continue
		}
		output, _ := prepared["output"].(map[string]any)
		if output == nil {
			t.Logf("max_bytes %d: the reply fits whole", bound)
			continue
		}
		value, has := prepared["knowledge_query"]
		t.Logf("max_bytes %d: omitted=%v\n  next_action=%v\n  the reply has the key knowledge_query=%v, its value=%v; knowledge_context=%v", bound, output["omitted"], output["next_action"], has, value, prepared["knowledge_context"])
		// B-U5: the shortened reply of a preparation with hook ids has no knowledge_query, and next_action said that
		// knowledge_query continues the search. What next_action says of the field holds for this reply: it has the field, or
		// the sentence is about a reply that has one.
		next, _ := output["next_action"].(string)
		if next == "" {
			t.Errorf("max_bytes %d: a shortened reply says what to do next", bound)
		}
		if strings.Contains(next, "knowledge_query") && value == nil && !strings.Contains(next, "knowledge_query, when the reply has one,") {
			t.Errorf("max_bytes %d: next_action points at knowledge_query, a field this reply does not have: %s", bound, next)
		}
		shortened = true
		break
	}
	if !shortened {
		t.Fatal("precondition: one of the bounds shortens the reply of a preparation with hook ids")
	}
}
