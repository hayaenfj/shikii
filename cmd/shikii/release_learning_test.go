package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPinnedLearningReadKeepsOwnPremiseChange(t *testing.T) {
	for _, historical := range []bool{false, true} {
		t.Run(map[bool]string{false: "current revision", true: "historical revision"}[historical], func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			b, source := loopBook(t)
			file := filepath.Join(b.Scope, "policy.txt")
			if err := os.WriteFile(file, []byte("original policy"), 0600); err != nil {
				t.Fatal(err)
			}
			p := loopPage(t, b, source, map[string]any{"title": "Checked project practice", "body": "Keep the checked condition.", "watch": []string{"policy.txt"}, "learning": map[string]any{"probe": map[string]any{"question": "Does the condition hold?", "method": "Read the policy.", "reconsider_when": "The policy changes."}}})
			first, err := workCall(t, b, "prepare", map[string]any{"query": "lesson:" + p.ID, "request": "Inspect the project practice"})
			if err != nil {
				t.Fatal(err)
			}
			if historical {
				loopPage(t, b, source, map[string]any{"id": p.ID, "title": p.Title, "body": "A revised condition."})
			}
			if err := os.WriteFile(file, []byte("changed policy"), 0600); err != nil {
				t.Fatal(err)
			}
			full, err := workCall(t, b, "prepare", map[string]any{"receipt": first["receipt"], "query": "lesson:" + p.ID + "@" + p.Revision})
			if err != nil {
				t.Fatal(err)
			}
			entry := full["knowledge"].([]any)[0].(map[string]any)
			article := entry["article"].(map[string]any)
			orientation := entry["orientation"].(map[string]any)
			changed := entry["recheck"] != nil || article["recheck"] != nil
			if changed != !historical || orientation["premise_changed"] != !historical {
				t.Fatalf("only the exact current revision carries its live premise change: historical=%v, recheck=%v, orientation=%v", historical, changed, orientation["premise_changed"])
			}
			if entry["learning_context"].(map[string]any)["learning"] == nil {
				t.Fatal("the pinned read must retain the complete learning context")
			}
		})
	}
}
