package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/notebook"
	"github.com/hayaenfj/shikii/internal/store"
)

const completionSession = "completion-atomic"

func completionArticleFixture(t *testing.T) (string, string, notebook.Book, notebook.Page) {
	t.Helper()
	repo, root, _ := planningFixture(t)
	b := notebook.Book{Root: root, Scope: feedbackScope(repo)}
	runUnreviewedHook(t, "user-prompt-submit", map[string]any{"session_id": completionSession, "cwd": repo, "prompt": planningTestRequest})
	o, err := b.Capture(notebook.Observation{Kind: "test", Title: "\u5408\u6210\u8a18\u4e8b\u306e\u8cc7\u6599", Content: "\u5408\u6210CSV\u306e\u6761\u4ef6\u3092\u78ba\u8a8d\u3057\u305f\u3002", Reference: "completion-atomic", Version: "1", ObservedAt: time.Now()})
	if err != nil {
		t.Fatal("\u5408\u6210\u8cc7\u6599\u306e\u6e96\u5099\u306b\u5931\u6557", err)
	}
	body := "\u6700\u521d\u306e\u6761\u4ef6\u3092\u78ba\u8a8d\u3059\u308b\u3002"
	created, err := b.Ingest(notebook.Draft{Title: "\u5408\u6210CSV\u306e\u624b\u9806", Lesson: "\u6761\u4ef6\u3092\u78ba\u8a8d\u3059\u308b\u3002", Body: &body, AppliesTo: "CSV", Kind: "howto", Scene: "\u5408\u6210\u78ba\u8a8d", Sources: []string{o.ID}})
	if err != nil {
		t.Fatal("\u5408\u6210\u8a18\u4e8b\u306e\u6e96\u5099\u306b\u5931\u6557", err)
	}
	prepared, err := workCall(t, b, "prepare", map[string]any{"session_id": completionSession, "turn_id": loadSession(root, completionSession).TurnID, "query": "lesson:" + created.Page.ID})
	if err != nil {
		t.Fatal("work\u306e\u6e96\u5099\u306b\u5931\u6557", err)
	}
	ref := ""
	for _, raw := range prepared["knowledge"].([]any) {
		p := loopObject(t, raw)
		if p["id"] == created.Page.ID && p["revision"] == created.Page.Revision {
			ref, _ = p["ref"].(string)
		}
	}
	if ref == "" {
		t.Fatal("\u540c\u3058\u8a18\u4e8b\u7248\u3092\u8aad\u3081\u306a\u304b\u3063\u305f")
	}
	for _, phase := range []string{"start", "finish"} {
		v, err := workCall(t, b, "record", map[string]any{"receipt": prepared["receipt"], "reasoning": reasoningInput(phase, ref)})
		if err != nil || v["complete"] != true {
			t.Fatal("\u63a8\u8ad6\u306e\u524d\u63d0\u3092\u4fdd\u5b58\u3067\u304d\u306a\u304b\u3063\u305f", err)
		}
	}
	return repo, root, b, *created.Page
}

func completionPatch(p notebook.Page) notebook.Patch {
	return notebook.Patch{ID: p.ID, Revision: p.Revision, Sources: []string{p.Sources[0].ID}, ChangeReason: "\u5408\u6210\u306e\u4e26\u884c\u66f4\u65b0", Edits: []notebook.TextEdit{{Field: "body", Before: "\u6700\u521d\u306e\u6761\u4ef6\u3092\u78ba\u8a8d\u3059\u308b\u3002", After: "\u5909\u66f4\u5f8c\u306e\u6761\u4ef6\u3092\u78ba\u8a8d\u3059\u308b\u3002"}}}
}

func completionPublicPatch(t *testing.T, p notebook.Page) {
	t.Helper()
	raw, err := json.Marshal(completionPatch(p))
	if err != nil {
		t.Fatal(err)
	}
	old := stdin
	stdin = strings.NewReader(string(raw))
	defer func() { stdin = old }()
	capture(t, func() error { return cmdNotebook([]string{"patch"}) })
}

func TestCompletionCommitSerializesArticleMutation(t *testing.T) {
	repo, root, book, page := completionArticleFixture(t)
	snapshot := loadSession(root, completionSession)
	wikiLock := filepath.Join(root, "wiki", "lessons", store.StoreName(root, book.Scope), ".lock")
	entered, resume := make(chan bool, 1), make(chan struct{})
	finished := make(chan error, 1)
	var actionRunning atomic.Bool
	commit := workCommitter(func(action func() error) error {
		return commitStopReasoning(root, completionSession, snapshot, func() error {
			actionRunning.Store(true)
			entered <- errors.Is(tryLockFromOutside(wikiLock), syscall.EWOULDBLOCK)
			<-resume
			err := action()
			actionRunning.Store(false)
			return err
		})
	})
	go func() {
		_, _, err := hookDecideWithCommit(root, "done-as-asked", map[string]string{"request": planningTestRequest, "files": "none", "summary": "\u5408\u6210\u78ba\u8a8d\u3092\u7d42\u3048\u307e\u3057\u305f\u3002"}, repo, commit)
		finished <- err
	}()
	var held bool
	select {
	case held = <-entered:
	case err := <-finished:
		t.Fatal("\u4fdd\u5b58\u5883\u754c\u306b\u5230\u9054\u3057\u306a\u304b\u3063\u305f", err)
	case <-time.After(5 * time.Second):
		t.Fatal("\u4fdd\u5b58\u5883\u754c\u306b\u5230\u9054\u3057\u306a\u304b\u3063\u305f")
	}
	editStarted, editDone := make(chan struct{}), make(chan error, 1)
	var overlapped atomic.Bool
	go func() {
		close(editStarted)
		_, err := runNotebook(root, book.Scope, notebookRequest{Operation: "patch", Patch: ptrCompletionPatch(page)})
		overlapped.Store(actionRunning.Load())
		editDone <- err
	}()
	<-editStarted
	// Finish the edit before resuming an unprotected commit to fix the race order.
	if !held {
		if err := <-editDone; err != nil {
			t.Error("\u4e26\u884c\u8a18\u4e8b\u7de8\u96c6\u306b\u5931\u6557", err)
		}
	}
	close(resume)
	if err := <-finished; err != nil {
		t.Fatal("\u5224\u5b9a\u306e\u4fdd\u5b58\u306b\u5931\u6557", err)
	}
	if held {
		if err := <-editDone; err != nil {
			t.Fatal("\u76f4\u5217\u5316\u5f8c\u306e\u8a18\u4e8b\u7de8\u96c6\u306b\u5931\u6557", err)
		}
	}
	records, err := (store.Store{Dir: root}).Records("done-as-asked", 0)
	if err != nil || len(records) != 1 {
		t.Fatal("\u5224\u5b9a\u306e\u4fdd\u5b58\u4ef6\u6570\u304c\u7570\u306a\u308b", len(records), err)
	}
	t.Logf("\u5224\u5b9a\u4fdd\u5b58\u4e2d\u306ewiki\u30ed\u30c3\u30af=%t \u8a18\u4e8b\u66f4\u65b0\u3068\u5224\u5b9a\u4fdd\u5b58\u306e\u91cd\u8907=%t \u5224\u5b9a\u4ef6\u6570=%d", held, overlapped.Load(), len(records))
	if !held || overlapped.Load() {
		t.Fatal("\u8a18\u4e8b\u306e\u7248\u78ba\u8a8d\u3068\u5224\u5b9a\u4fdd\u5b58\u306e\u9593\u306b\u6b63\u898f\u306e\u8a18\u4e8b\u66f4\u65b0\u304c\u5165\u3063\u305f")
	}
}

func ptrCompletionPatch(p notebook.Page) *notebook.Patch {
	patch := completionPatch(p)
	return &patch
}

func TestCompletionFinalSaveRejectsChangedPremise(t *testing.T) {
	for _, transition := range []string{"edit", "delete", "unreadable", "source-unavailable", "turn"} {
		t.Run(transition, func(t *testing.T) {
			repo, root, book, page := completionArticleFixture(t)
			snapshot := loadSession(root, completionSession)
			out, muts, err := hookStop(root, &hookInput{SessionID: completionSession, CWD: repo, LastAssistantMessage: "\u5408\u6210\u78ba\u8a8d\u3092\u7d42\u3048\u307e\u3057\u305f\u3002"}, snapshot)
			if err != nil || out.Block || out.Halt != "" {
				t.Fatal("\u6700\u7d42\u4fdd\u5b58\u306e\u524d\u63d0\u3092\u6e96\u5099\u3067\u304d\u306a\u304b\u3063\u305f", err)
			}
			switch transition {
			case "edit":
				completionPublicPatch(t, page)
			case "delete":
				_, err = book.Manage(notebook.Change{ID: page.ID, Revision: page.Revision, Action: "delete"})
			case "unreadable":
				err = os.WriteFile(filepath.Join(root, "wiki", "lessons", store.StoreName(root, book.Scope), page.ID+".md"), []byte("synthetic unreadable article"), 0600)
			case "source-unavailable":
				err = os.Remove(filepath.Join(root, "records", "wiki-sources", store.StoreName(root, book.Scope), page.Sources[0].ID+".json"))
			case "turn":
				err = updateSession(root, completionSession, func(s *hookSession) { s.TurnID = store.NewTraceID() })
			}
			if err != nil {
				t.Fatal("\u7af6\u5408\u306e\u6e96\u5099\u306b\u5931\u6557", err)
			}
			err = saveHookMutations(root, completionSession, "stop", snapshot, &out, muts, nil)
			saved := loadSession(root, completionSession)
			t.Logf("\u6700\u7d42\u4fdd\u5b58\u304c\u62d2\u5426=%t Judged=%t", err != nil || out.Block || out.Halt != "", saved.Judged)
			if err == nil && !out.Block && out.Halt == "" || saved.Judged {
				t.Fatal("\u7121\u52b9\u306b\u306a\u3063\u305f\u524d\u63d0\u3067\u6700\u7d42\u7d42\u4e86\u72b6\u614b\u3092\u4fdd\u5b58\u3057\u305f")
			}
		})
	}
}

func TestCompletionModelWaitAllowsWikiMutation(t *testing.T) {
	repo, root, book, page := completionArticleFixture(t)
	snapshot := loadSession(root, completionSession)
	entered, resume := make(chan struct{}), make(chan struct{})
	var waiting atomic.Bool
	model := fakeAgentModel(t, func(state string) float64 {
		if strings.Contains(state, "COMPLETION_MODEL_WAIT") && waiting.CompareAndSwap(false, true) {
			close(entered)
			<-resume
		}
		return .9
	})
	old := hookLocalURL
	hookLocalURL = model.URL
	t.Cleanup(func() { hookLocalURL = old })
	resumed := false
	defer func() {
		if !resumed {
			close(resume)
		}
	}()
	type stopResult struct {
		out hookOut
		err error
	}
	finished := make(chan stopResult, 1)
	go func() {
		out, _, err := hookStop(root, &hookInput{SessionID: completionSession, CWD: repo, LastAssistantMessage: "COMPLETION_MODEL_WAIT"}, snapshot)
		finished <- stopResult{out: out, err: err}
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("\u30e2\u30c7\u30eb\u5f85\u6a5f\u3078\u5230\u9054\u3057\u306a\u304b\u3063\u305f")
	}
	_, editErr := runNotebook(root, book.Scope, notebookRequest{Operation: "patch", Patch: ptrCompletionPatch(page)})
	close(resume)
	resumed = true
	result := <-finished
	if editErr != nil {
		t.Fatal("\u30e2\u30c7\u30eb\u5f85\u6a5f\u4e2d\u306e\u6b63\u898fwiki\u66f4\u65b0\u3092\u59a8\u3052\u305f", editErr)
	}
	records, err := (store.Store{Dir: root}).Records("done-as-asked", 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("\u30e2\u30c7\u30eb\u5f85\u6a5f\u4e2d\u306e\u8a18\u4e8b\u7de8\u96c6\u6210\u529f=true \u7af6\u5408\u3057\u305f\u5224\u5b9a\u306e\u4fdd\u5b58\u4ef6\u6570=%d \u7d42\u4e86\u62d2\u5426=%t", len(records), result.err != nil || result.out.Block || result.out.Halt != "")
	if len(records) != 0 || result.err == nil && !result.out.Block && result.out.Halt == "" {
		t.Fatal("\u30e2\u30c7\u30eb\u5f85\u6a5f\u4e2d\u306b\u5909\u308f\u3063\u305f\u8a18\u4e8b\u3067\u5b8c\u4e86\u3092\u4fdd\u5b58\u3057\u305f")
	}
}
