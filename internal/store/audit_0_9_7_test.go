package store

import (
	"os"
	"path/filepath"
	"testing"
)

// 0.9.7 audit B-3: after the move to the fixed name, an older shikii still running (an MCP server started before the update) appends to
// records/feedback/<sha256(path)>.jsonl. Those conversations were never read again. They are merged into the fixed name when it is used.
func TestAuditOldBinaryWritesAfterMigration(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	scope := "/work/p"
	if err := s.AppendFeedback(scope, Feedback{ID: "0000000000000001", Request: "a", Summary: "b", Reply: "c"}); err != nil {
		t.Fatal(err)
	}
	line := `{"id":"0000000000000002","request":"x","summary":"y","reply":"z","at":"2026-10-01T00:00:00Z"}` + "\n"
	if err := os.WriteFile(filepath.Join(s.Dir, "records", "feedback", scopeKey(scope)+".jsonl"), []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := s.RecentFeedback(scope)
	if err != nil || len(got) != 2 {
		t.Fatalf("the conversation an older shikii wrote is read: %d %v", len(got), err)
	}
	if again, err := s.RecentFeedback(scope); err != nil || len(again) != 2 {
		t.Fatalf("and merged once, not twice: %d %v", len(again), err)
	}
}
