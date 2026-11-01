package main

import (
	"github.com/hayaenfj/shikii/internal/notebook"
	"github.com/hayaenfj/shikii/internal/store"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestIndependent019OldReceiptContinuesAfterTurnChange(t *testing.T) {
	repo, root, _ := planningFixture(t)
	st := store.Store{Dir: root}
	scope := feedbackScope(repo)
	_, trace := reviewSeed(t, st, scope, 1)
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": planningTestRequest})
	b := notebook.Book{Root: root, Scope: scope}
	v, err := workCall(t, b, "prepare", map[string]any{"session_id": "planning", "turn_id": loadSession(root, "planning").TurnID, "query": "README language"})
	if err != nil {
		t.Fatal(err)
	}
	p := v["pending"].([]any)[0].(map[string]any)
	ref := p["reviews"].([]any)[0].(map[string]any)["ref"]
	oldTurn := loadSession(root, "planning").TurnID
	old := workAfterWrite
	defer func() { workAfterWrite = old }()
	switched := false
	workAfterWrite = func(kind string) error {
		if kind == "observation" && !switched {
			switched = true
			runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": "New independent task"})
			if loadSession(root, "planning").TurnID == oldTurn {
				t.Fatal("turn did not change")
			}
		}
		return nil
	}
	result, err := workCall(t, b, "record", map[string]any{"receipt": v["receipt"], "observations": []any{map[string]any{"kind": "test", "title": "Synthetic race checkpoint", "content": "Only synthetic content."}}, "reviews": []any{map[string]any{"ref": ref, "answer": "unknown", "reason": "Synthetic outcome is unknown."}}, "reviewed": []any{p["ref"]}})
	if err != nil {
		t.Fatal(err)
	}
	if !switched {
		t.Fatal("checkpoint not reached")
	}
	t.Logf("complete=%v observation_errors=%v reflection=%v trace=%s", result["complete"], result["errors"], result["reflection"], trace)
	remaining, err := reflectQuery(root, scope)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range remaining.Sources {
		for _, target := range s.Targets {
			if target.TraceID == trace {
				return
			}
		}
	}
	t.Fatal("old receipt saved its review after the current turn was replaced")
}

func TestSessionUpdateRefusesUnavailableLock(t *testing.T) {
	repo := t.TempDir()
	root := hookEnv(t, nil, repo)
	id := "unavailable-lock"
	lock := sessionPath(root, id) + ".lock"
	if err := os.MkdirAll(filepath.Dir(lock), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(lock, 0700); err != nil {
		t.Fatal(err)
	}
	ran := false
	err := updateSession(root, id, func(s *hookSession) { ran = true; s.TurnID = "1111111111111111" })
	if err == nil || ran {
		t.Fatal("session mutation must not proceed without owning its lock", ran, err)
	}
	if _, err := os.Stat(sessionPath(root, id)); !os.IsNotExist(err) {
		t.Fatal("an unlocked update wrote a session", err)
	}
}

func TestSessionPruningKeepsLockIdentity(t *testing.T) {
	repo := t.TempDir()
	root := hookEnv(t, nil, repo)
	id := "active-lock"
	unlock, ok := lockSessionWithin(root, id, 0)
	if !ok {
		t.Fatal("lock unavailable")
	}
	defer unlock()
	path := sessionPath(root, id) + ".lock"
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-hookSessionTTL - time.Hour)
	if err = os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	pruneSessions(root)
	after, err := os.Stat(path)
	if err != nil || !os.SameFile(before, after) {
		t.Fatal("pruning must retain the shared lock inode", err)
	}
}

func TestWorkPlacementModelCannotPublishAfterTurnChange(t *testing.T) {
	repo := gitRepo(t)
	var root string
	var switched, lockHeld atomic.Bool
	model := fakeAgentModel(t, func(state string) float64 {
		if root != "" && strings.HasPrefix(state, "existing page title:") && switched.CompareAndSwap(false, true) {
			unlock, ok := lockSessionWithin(root, "planning", 0)
			if ok {
				unlock()
			} else {
				lockHeld.Store(true)
			}
			runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": "Start a different independent task"})
		}
		return .9
	})
	root = hookEnv(t, model, repo)
	usePlanningSpec(t, root)
	b := notebook.Book{Root: root, Scope: feedbackScope(repo)}
	o, err := b.Capture(notebook.Observation{Kind: "test", Title: "Existing rule source", Content: "Synthetic existing rule.", Reference: "synthetic", Version: "1", ObservedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	loopPage(t, b, o.ID, map[string]any{"title": "Existing CSV procedure", "lesson": "Keep tenant filters.", "applies_to": "CSV export"})
	if _, err = b.Capture(notebook.Observation{Kind: "test", Title: "New checked result", Content: "Date filters were checked.", Reference: "synthetic", Version: "1", ObservedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": planningTestRequest})
	v, err := workCall(t, b, "prepare", map[string]any{"session_id": "planning", "turn_id": loadSession(root, "planning").TurnID, "query": "CSV filters"})
	if err != nil {
		t.Fatal(err)
	}
	result, err := workCall(t, b, "record", map[string]any{"receipt": v["receipt"], "notes": []any{map[string]any{"title": "CSV date filters", "lesson": "Keep tenant and date filters.", "applies_to": "CSV export", "kind": "howto", "scene": "exporting CSV", "sources": []string{"evidence1"}}}})
	if err != nil || result["complete"] != false || !switched.Load() {
		t.Fatal(result, err, switched.Load())
	}
	if lockHeld.Load() {
		t.Fatal("model inference must not hold the session commit lock")
	}
	st := store.Store{Dir: root}
	for _, specID := range []string{"wiki-place", "wiki-conflict"} {
		records, err := st.Records(specID, 0)
		if err != nil || len(records) != 0 {
			t.Fatal("a model result from the old turn was published", specID, len(records), err)
		}
	}
	pages, err := b.Live()
	if err != nil || len(pages) != 1 {
		t.Fatal("the old turn published a new note", pages, err)
	}
}

func TestWorkCommitSharesTheHookSessionLock(t *testing.T) {
	repo, root, _ := planningFixture(t)
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": planningTestRequest})
	b := notebook.Book{Root: root, Scope: feedbackScope(repo)}
	v, err := workCall(t, b, "prepare", map[string]any{"session_id": "planning", "turn_id": loadSession(root, "planning").TurnID, "query": "CSV filters"})
	if err != nil {
		t.Fatal(err)
	}
	token, err := workDecode(v["receipt"].(string))
	if err != nil {
		t.Fatal(err)
	}
	dir, err := openWorkSessions(root)
	if err != nil {
		t.Fatal(err)
	}
	defer dir.Close()
	name := filepath.Base(sessionPath(root, "planning"))
	ran := false
	err = commitWorkTurn(dir, name, b.Scope, token, func() error {
		ran = true
		unlock, ok := lockSessionWithin(root, "planning", 0)
		if ok {
			unlock()
			t.Error("hook mutation acquired the lock inside a work commit")
		}
		return nil
	})
	if err != nil || !ran {
		t.Fatal(ran, err)
	}
}

func TestWorkReflectionCommitStopsBetweenItems(t *testing.T) {
	repo, root, _ := planningFixture(t)
	st := store.Store{Dir: root}
	scope := feedbackScope(repo)
	id1, trace1 := reviewSeed(t, st, scope, 1)
	id2, trace2 := reviewSeed(t, st, scope, 2)
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": planningTestRequest})
	b := notebook.Book{Root: root, Scope: scope}
	v, err := workCall(t, b, "prepare", map[string]any{"session_id": "planning", "turn_id": loadSession(root, "planning").TurnID, "query": "README language"})
	if err != nil {
		t.Fatal(err)
	}
	token, err := workDecode(v["receipt"].(string))
	if err != nil {
		t.Fatal(err)
	}
	dir, err := openWorkSessions(root)
	if err != nil {
		t.Fatal(err)
	}
	defer dir.Close()
	name := filepath.Base(sessionPath(root, "planning"))
	published := 0
	gate := workCommitter(func(action func() error) error {
		err := commitWorkTurn(dir, name, scope, token, action)
		if err == nil {
			published++
			if published == 1 {
				runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": "A new independent request"})
			}
		}
		return err
	})
	req := reflectRequest{Operation: "submit", Reviews: []reviewRequest{{SourceID: id1, TraceID: trace1, Truth: "unknown", Reason: "Unknown synthetic outcome."}, {SourceID: id2, TraceID: trace2, Truth: "unknown", Reason: "Unknown synthetic outcome."}}, Wiki: []notebook.Draft{{Sources: []string{id1, id2}}}}
	raw, err := runReflectWithCommit(root, scope, req, gate)
	if err != nil {
		t.Fatal(err)
	}
	result := raw.(reflectResult)
	if result.Complete || published != 1 {
		t.Fatal("reflection must guard each item separately", result, published)
	}
	left, err := reflectQuery(root, scope)
	if err != nil {
		t.Fatal(err)
	}
	pendingSecond, firstUnsorted := false, false
	for _, s := range left.Sources {
		if s.Source.ID == id1 {
			firstUnsorted = s.WikiPending
		}
		for _, target := range s.Targets {
			if target.TraceID == trace2 {
				pendingSecond = true
			}
			if target.TraceID == trace1 {
				t.Fatal("the first pre-transition review should be saved")
			}
		}
	}
	if !pendingSecond || !firstUnsorted {
		t.Fatal("post-transition review or source sorting was saved", left)
	}
}
