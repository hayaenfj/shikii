package main

import (
	"bytes"
	"github.com/hayaenfj/shikii/internal/notebook"
	"github.com/hayaenfj/shikii/internal/store"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestPlanningSnapshotBoundaries(t *testing.T) {
	repo, root, _ := planningFixture(t)
	book := notebook.Book{Root: root, Scope: feedbackScope(repo)}
	page := reasoningPage(t, book, "CSV export workflow", "howto", "Supplied body.")
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": planningTestRequest})
	turn := loadSession(root, "planning").TurnID
	_, err := planningCall(t, "query", map[string]any{"session_id": "planning", "turn_id": turn, "query": "CSV export"})
	if err != nil {
		t.Fatal(err)
	}
	dir, err := planningSessionRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer dir.Close()
	name := filepath.Base(sessionPath(root, "planning"))
	path := filepath.Join(root, "sessions", name)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	req := planningRequest{SessionID: "planning", TurnID: turn, Query: "replacement-query"}
	sources := map[string]string{"current_user": "UNEXPECTED_REPLACEMENT"}
	k := &store.KnowledgeContext{Query: req.Query}
	assertUnchanged := func(t *testing.T) {
		t.Helper()
		after, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(before, after) {
			t.Fatal("rejected query modified session", err)
		}
	}
	t.Run("foreign-project", func(t *testing.T) {
		if err := savePlanningQuery(dir, name, book.Scope+"/foreign", req, sources, k); err == nil {
			t.Error("foreign project accepted")
		}
		assertUnchanged(t)
	})
	t.Run("session-lock", func(t *testing.T) {
		f, err := dir.OpenFile(name+".lock", os.O_CREATE|os.O_RDWR, 0600)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
			t.Fatal(err)
		}
		defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		if err := savePlanningQuery(dir, name, book.Scope, req, sources, k); err == nil {
			t.Error("locked session accepted")
		}
		assertUnchanged(t)
	})
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": planningTestRequest + " Handle an empty list too."})
	before, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Run("new-turn", func(t *testing.T) {
		if err := savePlanningQuery(dir, name, book.Scope, req, sources, k); err == nil {
			t.Error("old turn accepted")
		}
		assertUnchanged(t)
	})
	s := loadSession(root, "planning")
	if s.Planning.Knowledge != nil {
		t.Fatal("old query crossed the turn boundary")
	}
	payload := planningPayload(root)
	payload["query"] = "CSV export"
	if _, err := planningCall(t, "submit", payload); err != nil {
		t.Fatal(err)
	}
	records, err := (store.Store{Dir: root}).Records("needs-plan", 0)
	if err != nil || len(records) != 1 {
		t.Fatal("missing direct-submit decision", err)
	}
	if len(records[0].KnowledgeContext.Read) != 0 {
		t.Fatal("previous turn's body counted as read", page.ID)
	}
}
