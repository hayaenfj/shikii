package store

import (
	"os"
	"path/filepath"
	"testing"
)

// 0.9.7: the conversations kept for reflection are under a fixed name inside .shikii, not under a hash of the project's path, so moving the
// folder does not hide them. A file written by v0.9.1 under its path's hash is read at that path and after a move.
func TestFeedbackFollowsTheProjectFolder(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	f := Feedback{ID: "0000000000000abc", Request: "fix the readme", Summary: "fixed", Reply: "thanks"}
	if err := s.AppendFeedback("/work/old", f); err != nil {
		t.Fatal(err)
	}
	if got, err := s.RecentFeedback("/work/new"); err != nil || len(got) != 1 || got[0].ID != f.ID {
		t.Fatalf("the conversation is there after the move: %+v %v", got, err)
	}

	legacy := Store{Dir: t.TempDir()}
	src := filepath.Join("..", "notebook", "testdata", "v091", "records", "feedback")
	entries, err := os.ReadDir(src)
	if err != nil || len(entries) != 1 {
		t.Fatalf("precondition: the v0.9.1 feedback file: %v", err)
	}
	data, _ := os.ReadFile(filepath.Join(src, entries[0].Name()))
	if err := os.MkdirAll(filepath.Join(legacy.Dir, "records", "feedback"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacy.Dir, "records", "feedback", entries[0].Name()), data, 0o600); err != nil {
		t.Fatal(err)
	}
	before, err := legacy.RecentFeedback("/work/project")
	if err != nil || len(before) == 0 {
		t.Fatalf("v0.9.1's conversations are read at their own path: %+v %v", before, err)
	}
	if after, err := legacy.RecentFeedback("/moved/project"); err != nil || len(after) != len(before) {
		t.Fatalf("and after a move: %d of %d, %v", len(after), len(before), err)
	}
}
