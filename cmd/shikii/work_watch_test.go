package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hayaenfj/shikii/internal/notebook"
	"github.com/hayaenfj/shikii/internal/store"
)

func TestWorkWatchSnapshotRecovery(t *testing.T) {
	t.Run("changed", func(t *testing.T) { round4CodeWatchSnapshotRecovery(t, true) })
	t.Run("stable", func(t *testing.T) { round4CodeWatchSnapshotRecovery(t, false) })
}

func round4CodeWatchSnapshotRecovery(t *testing.T, changeDuring bool) {
	repo, root, _ := planningFixture(t)
	b := notebook.Book{Root: root, Scope: feedbackScope(repo)}
	path := filepath.Join(repo, "synthetic-context.txt")
	if e := os.WriteFile(path, []byte("SYNTHETIC_CONTEXT_A"), 0600); e != nil {
		t.Fatal(e)
	}
	prep, e := workCall(t, b, "prepare", map[string]any{"query": "synthetic context"})
	if e != nil {
		t.Fatal(e)
	}
	payload := map[string]any{"receipt": prep["receipt"], "observations": []any{map[string]any{"title": "Synthetic context input", "content": "SYNTHETIC_CONTEXT_EVIDENCE"}}, "notes": []any{map[string]any{"title": "Synthetic context procedure", "lesson": "Recheck the context before recording.", "applies_to": "synthetic context", "kind": "howto", "scene": "checking context", "sources": []string{"new1"}, "watch": []string{"synthetic-context.txt"}}}}
	oldSync, oldAfter := store.SyncTemp, workAfterWrite
	defer func() { store.SyncTemp = oldSync; workAfterWrite = oldAfter }()
	changed := false
	store.SyncTemp = func(f *os.File) error {
		if !changed && strings.Contains(f.Name(), ".work-") {
			raw, e := os.ReadFile(f.Name())
			var s hookSession
			if e == nil && json.Unmarshal(raw, &s) == nil && s.Planning != nil && s.Planning.Work != nil && len(s.Planning.Work.Expected) > 0 {
				changed = true
				if changeDuring {
					if e := os.WriteFile(path, []byte("SYNTHETIC_CONTEXT_B"), 0600); e != nil {
						return e
					}
				}
			}
		}
		return oldSync(f)
	}
	failed := false
	workAfterWrite = func(kind string) error {
		if kind == "note" && !failed {
			failed = true
			return errors.New("SYNTHETIC_POST_PUBLICATION_FAILURE")
		}
		return nil
	}
	first, e := workCall(t, b, "record", payload)
	store.SyncTemp = oldSync
	workAfterWrite = oldAfter
	if e != nil || !changed || !failed || first["complete"] != false {
		t.Fatal(changed, failed, first, e)
	}
	before, e := b.Live()
	if e != nil || len(before) != 1 {
		t.Fatal(before, e)
	}
	expectedHash := sha256.Sum256([]byte("SYNTHETIC_CONTEXT_A"))
	if len(before[0].Premises) != 1 || before[0].Premises[0].Hash != hex.EncodeToString(expectedHash[:]) {
		t.Fatal("publication must keep the validated premise snapshot", before)
	}
	if changeDuring {
		view, err := b.Query("lesson:" + before[0].ID)
		if err != nil || len(view.Lessons) != 1 || len(view.Lessons[0].Recheck) != 1 {
			t.Fatal("a change after validation remains an explicit recheck", view, err)
		}
	}

	token, e := workDecode(prep["receipt"].(string))
	if e != nil {
		t.Fatal(e)
	}
	dir, e := openWorkSessions(root)
	if e != nil {
		t.Fatal(e)
	}
	state, _, e := loadWorkState(dir, b.Scope, token)
	dir.Close()
	if e != nil {
		t.Fatal(e)
	}
	expectedMatches := false
	for _, expected := range state.Expected {
		if expected.Revision == before[0].Revision {
			expectedMatches = true
		}
	}
	retry, e := workCall(t, b, "record", payload)
	pending, _ := retry["pending"].([]any)
	t.Logf("watch_changed=%v publication_failure=%v expected_matches_published=%v first_complete=%v retry_complete=%v retry_pending=%d", changeDuring, failed, expectedMatches, first["complete"], retry["complete"], len(pending))
	after, readError := b.Live()
	if readError != nil || len(after) != 1 || after[0].Revision != before[0].Revision {
		t.Fatal(after, readError)
	}
	if e != nil || retry["complete"] != true {
		t.Fatalf("\u540c\u4e00\u5165\u529b\u306e\u518d\u9001\u304c\u516c\u958b\u6e08\u307f\u306e\u7248\u3078\u623b\u3089\u306a\u3044: complete=%v pending=%d errors=%v error=%v", retry["complete"], len(pending), retry["errors"], e)
	}

}
