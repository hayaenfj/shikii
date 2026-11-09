package main

import (
	"errors"
	"fmt"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/notebook"
	"github.com/hayaenfj/shikii/internal/store"
)

func pauseAssignmentPreparation(t *testing.T) (<-chan struct{}, chan struct{}) {
	t.Helper()
	entered := make(chan struct{})
	resume := make(chan struct{})
	var once atomic.Bool
	old := assignmentPrepareWork
	assignmentPrepareWork = func(root, scope string, req workRequest) (any, error) {
		if once.CompareAndSwap(false, true) {
			close(entered)
			<-resume
		}
		return old(root, scope, req)
	}
	t.Cleanup(func() { assignmentPrepareWork = old })
	return entered, resume
}

func TestAssignmentPrepareAllowsOtherAssignments(t *testing.T) {
	b, _ := loopBook(t)
	entered, resume := pauseAssignmentPreparation(t)
	first := assignmentInput("first", "case", "worker", "work")
	finished := make(chan error, 1)
	go func() {
		_, err := workCall(t, b, "prepare", map[string]any{"assignment": first, "query": "unmatched"})
		finished <- err
	}()
	<-entered
	defer func() {
		close(resume)
		if err := <-finished; err != nil {
			t.Error(err)
		}
	}()
	if _, err := workCall(t, b, "prepare", map[string]any{"assignment": first, "query": "unmatched"}); err == nil {
		t.Fatal("the same assignment must remain serialized")
	}
	if _, err := workCall(t, b, "prepare", map[string]any{"assignment": assignmentInput("second", "case", "worker", "work"), "query": "unmatched"}); err != nil {
		t.Fatal("another assignment must prepare outside the first one's inventory boundary", err)
	}
}

func TestAssignmentPrepareRechecksFinalCapacity(t *testing.T) {
	b, _ := loopBook(t)
	dir, err := assignmentDir(b.Root, b.Scope)
	if err != nil {
		t.Fatal(err)
	}
	defer dir.Close()
	for i := range 511 {
		a := assignmentContext{ID: fmt.Sprintf("prior-%03d", i), Group: "case", Role: "worker", Stage: "work", Purpose: "Synthetic capacity"}
		if err = saveAssignment(dir, assignmentRecord{Context: a, At: time.Unix(int64(i+1), 0), Bindings: map[string]assignmentBinding{}, Used: map[string]notebook.CollectiveBaseline{}, Events: map[string]assignmentSavedEvent{}}); err != nil {
			t.Fatal(err)
		}
	}
	entered, resume := pauseAssignmentPreparation(t)
	finished := make(chan error, 1)
	first := assignmentInput("first", "case", "worker", "work")
	go func() {
		_, e := workCall(t, b, "prepare", map[string]any{"assignment": first, "query": "unmatched"})
		finished <- e
	}()
	<-entered
	_, secondErr := workCall(t, b, "prepare", map[string]any{"assignment": assignmentInput("second", "case", "worker", "work"), "query": "unmatched"})
	close(resume)
	firstErr := <-finished
	if secondErr != nil {
		t.Fatal("the competing assignment must fill the final slot", secondErr)
	}
	if firstErr == nil {
		t.Fatal("capacity must be checked again after the unlocked work")
	}
	if _, err := readAssignment(dir, assignmentName("first")); !os.IsNotExist(err) {
		t.Fatal("a rejected checkpoint must not enter the inventory", err)
	}
	if err = dir.Remove(assignmentName("prior-000")); err != nil {
		t.Fatal(err)
	}
	if _, err := workCall(t, b, "prepare", map[string]any{"assignment": first, "query": "unmatched"}); err != nil {
		t.Fatal("the original request must retry after capacity becomes available", err)
	}
}

func TestAssignmentPrepareFailureReleasesLocks(t *testing.T) {
	b, _ := loopBook(t)
	old := assignmentPrepareWork
	assignmentPrepareWork = func(string, string, workRequest) (any, error) {
		return nil, errors.New("synthetic preparation failure")
	}
	t.Cleanup(func() { assignmentPrepareWork = old })
	a := assignmentInput("retry", "case", "worker", "work")
	if _, err := workCall(t, b, "prepare", map[string]any{"assignment": a, "query": "unmatched"}); err == nil {
		t.Fatal("expected preparation failure")
	}
	assignmentPrepareWork = old
	oldSync := store.SyncTemp
	t.Cleanup(func() { store.SyncTemp = oldSync })
	assignmentPrepareWork = func(root, scope string, req workRequest) (any, error) {
		v, e := old(root, scope, req)
		store.SyncTemp = func(*os.File) error { return errors.New("synthetic checkpoint sync failure") }
		return v, e
	}
	if _, err := workCall(t, b, "prepare", map[string]any{"assignment": a, "query": "unmatched"}); err == nil {
		t.Fatal("expected final checkpoint failure")
	}
	store.SyncTemp = oldSync
	assignmentPrepareWork = old
	if _, err := workCall(t, b, "prepare", map[string]any{"assignment": a, "query": "unmatched"}); err != nil {
		t.Fatal("the unchanged request must retry", err)
	}
}

func TestAssignmentPrepareRefreshesPeerState(t *testing.T) {
	b, _ := loopBook(t)
	peer := assignmentInput("peer", "case", "worker", "work")
	p, err := workCall(t, b, "prepare", map[string]any{"assignment": peer, "query": "unmatched"})
	if err != nil {
		t.Fatal(err)
	}
	entered, resume := pauseAssignmentPreparation(t)
	type result struct {
		value map[string]any
		err   error
	}
	done := make(chan result, 1)
	go func() {
		v, e := workCall(t, b, "prepare", map[string]any{"assignment": assignmentInput("current", "case", "worker", "work"), "query": "unmatched"})
		done <- result{v, e}
	}()
	<-entered
	_, recordErr := workCall(t, b, "record", map[string]any{"assignment": peer, "receipt": p["receipt"], "event": map[string]any{"id": "finished", "state": "completed", "summary": "Synthetic peer completion."}})
	close(resume)
	r := <-done
	if recordErr != nil || r.err != nil {
		t.Fatal(recordErr, r.err)
	}
	for _, raw := range r.value["teamwork"].(map[string]any)["coverage"].([]any) {
		item := raw.(map[string]any)
		if item["id"] == "peer" && item["state"] == "completed" {
			return
		}
	}
	t.Fatal("peer state must be read again after the unlocked preparation")
}

func TestAssignmentExtensionRetainsInventoryLock(t *testing.T) {
	b, _ := loopBook(t)
	a := assignmentInput("current", "case", "worker", "work")
	p, err := workCall(t, b, "prepare", map[string]any{"assignment": a, "query": "unmatched"})
	if err != nil {
		t.Fatal(err)
	}
	entered, resume := pauseAssignmentPreparation(t)
	finished := make(chan error, 1)
	go func() {
		_, e := workCall(t, b, "prepare", map[string]any{"assignment": a, "receipt": p["receipt"], "query": "unmatched"})
		finished <- e
	}()
	<-entered
	defer func() {
		close(resume)
		if e := <-finished; e != nil {
			t.Error(e)
		}
	}()
	if _, err = workCall(t, b, "prepare", map[string]any{"assignment": assignmentInput("other", "case", "worker", "work"), "query": "unmatched"}); err == nil {
		t.Fatal("extensions must retain the previous inventory publication boundary")
	}
}
