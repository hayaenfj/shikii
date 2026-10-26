package store

import (
	"os"
	"path/filepath"
	"testing"
)

func auditDAppend(t *testing.T, path, s string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString(s)
	f.Close()
}

// 0.9.7 re-audit D-1: when the older file was made again, the offset in .merged belonged to the old file: new lines were not merged (while shorter) or their start was skipped.
func TestReauditDMergeRecreatedFileKeepsStaleOffset(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	scope := "/work/p"
	if err := s.AppendFeedback(scope, Feedback{ID: "0000000000000001", Request: "a", Summary: "b", Reply: "c"}); err != nil {
		t.Fatal(err)
	}
	from := filepath.Join(s.Dir, "records", "feedback", scopeKey(scope)+".jsonl")
	auditDAppend(t, from, auditCLine("0000000000000002")+auditCLine("0000000000000003"))
	if got, _ := s.RecentFeedback(scope); len(got) != 3 {
		t.Fatalf("precondition: 3 lines merged: %d", len(got))
	}
	// the older file is removed and made again (e.g. after adopt moved it, the older shikii writes)
	os.Remove(from)
	auditDAppend(t, from, auditCLine("0000000000000004"))
	got, _ := s.RecentFeedback(scope)
	if len(got) != 4 {
		t.Errorf("the first line of the remade older file is merged: %d", len(got))
	}
	auditDAppend(t, from, auditCLine("0000000000000005")+auditCLine("0000000000000006"))
	got, _ = s.RecentFeedback(scope)
	ids := map[string]bool{}
	for _, f := range got {
		ids[f.ID] = true
	}
	for _, id := range []string{"0000000000000004", "0000000000000005", "0000000000000006"} {
		if !ids[id] {
			t.Errorf("%s is kept: %v", id, ids)
		}
	}
}

// 0.9.7 re-audit D-1: after adopt (the fixed name was emptied), the old .merged was left and skipped the older shikii's new lines.
func TestReauditDMergeStaleOffsetAfterAdopt(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	scope := "/work/p"
	if err := s.AppendFeedback(scope, Feedback{ID: "0000000000000001", Request: "a", Summary: "b", Reply: "c"}); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(s.Dir, "records", "feedback")
	from := filepath.Join(dir, scopeKey(scope)+".jsonl")
	auditDAppend(t, from, auditCLine("0000000000000002")+auditCLine("0000000000000003"))
	s.RecentFeedback(scope)
	os.Truncate(filepath.Join(dir, "project.jsonl"), 0)   // the fixed name is emptied
	s.RecentFeedback(scope)                               // adopt moves the older file to the fixed name
	auditDAppend(t, from, auditCLine("0000000000000009")) // the older shikii, still running, writes
	got, _ := s.RecentFeedback(scope)
	for _, f := range got {
		if f.ID == "0000000000000009" {
			return
		}
	}
	t.Errorf("a line the older shikii wrote after adopt is merged: %d", len(got))
}

// 0.9.7 re-audit D-2: a .merged broken to a negative number stopped merging for good.
func TestReauditDMergeNegativeOffsetStops(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	scope := "/work/p"
	if err := s.AppendFeedback(scope, Feedback{ID: "0000000000000001", Request: "a", Summary: "b", Reply: "c"}); err != nil {
		t.Fatal(err)
	}
	from := filepath.Join(s.Dir, "records", "feedback", scopeKey(scope)+".jsonl")
	auditDAppend(t, from, auditCLine("0000000000000002"))
	os.WriteFile(from+".merged", []byte("-1"), 0o600)
	for i := 0; i < 3; i++ {
		s.RecentFeedback(scope)
	}
	if got, _ := s.RecentFeedback(scope); len(got) != 2 {
		t.Errorf("with a broken .merged (-1) the older shikii's line is still merged: %d", len(got))
	}
}
