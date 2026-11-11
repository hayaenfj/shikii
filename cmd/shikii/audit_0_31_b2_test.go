package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hayaenfj/shikii/internal/notebook"
)

// More probes of audit B (0.31.0).

// The Stop block for a turn that changed a file and prepared nothing: what skills/shikii/references/planning.md says Stop asks for.
func TestAuditB_StopBlockAfterAChangedFile(t *testing.T) {
	repo, _, _ := planningFixture(t)
	t.Chdir(repo)
	runUnreviewedHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": planningTestRequest})
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("# changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	reason, blocked := stopReason(t, runUnreviewedHook(t, "stop", map[string]any{"session_id": "planning", "cwd": repo, "last_assistant_message": "Done."}))
	t.Logf("blocked=%v asks hook plan submit=%v\n%s", blocked, strings.Contains(reason, "to hook plan submit") || strings.Contains(reason, "hook plan query, read"), reason)
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
		break
	}
}
