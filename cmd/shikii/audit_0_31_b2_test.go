package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hayaenfj/shikii/internal/notebook"
)

// More probes of audit B (0.31.0). Both only printed what the candidate did; each now fails when what it shows is broken.

// The Stop block for a hook-prepared turn that changed a file without saving its ending result.
func TestAuditB_StopBlockAfterAChangedFile(t *testing.T) {
	repo, root, _ := planningFixture(t)
	t.Chdir(repo)
	preparedHookReply(t, runUnreviewedHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": planningTestRequest}))
	turnID := loadSession(root, "planning").TurnID
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("# changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	reason, blocked := stopReason(t, runUnreviewedHook(t, "stop", map[string]any{"session_id": "planning", "cwd": repo, "last_assistant_message": "Done."}))
	t.Logf("blocked=%v\n%s", blocked, reason)
	if !blocked {
		t.Fatalf("finishing with a changed file and no saved ending result is stopped: %s", reason)
	}
	// INVARIANT 7: the real hook prepares once; Stop directs its IDs and result to work record, including the MCP form.
	for _, w := range []string{"has its hook-prepared start and needs the saved ending result", planJSON("planning", turnID), "result:{changed,checked?,evidence?:[refs],remaining?}", " work record", "the MCP tool shikii_wiki, operation record"} {
		if !strings.Contains(reason, w) {
			t.Errorf("the block lacks %q:\n%s", w, reason)
		}
	}
	for _, obsolete := range []string{"hook plan", "receipt of your work prepare"} {
		if strings.Contains(reason, obsolete) {
			t.Errorf("the hook-prepared block requires an obsolete preparation step %q:\n%s", obsolete, reason)
		}
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
