package main

import (
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/notebook"

	"github.com/hayaenfj/shikii/internal/store"
)

func TestAssignmentCapacityFailureRetiresNewPreparation(t *testing.T) {
	b, source := loopBook(t)
	loopPage(t, b, source, map[string]any{"title": "Synthetic routing policy", "body": "Synthetic routing keeps the owner unresolved."})
	dir, e := assignmentDir(b.Root, b.Scope)
	if e != nil {
		t.Fatal(e)
	}
	defer dir.Close()
	for i := range 511 {
		a := assignmentContext{ID: fmt.Sprintf("audit-prior-%03d", i), Group: "audit", Role: "worker", Stage: "work", Purpose: "Synthetic capacity"}
		if e = saveAssignment(dir, assignmentRecord{Context: a, At: time.Unix(int64(i+1), 0), Bindings: map[string]assignmentBinding{}, Used: map[string]notebook.CollectiveBaseline{}, Events: map[string]assignmentSavedEvent{}}); e != nil {
			t.Fatal(e)
		}
	}
	old := assignmentPrepareWork
	t.Cleanup(func() { assignmentPrepareWork = old })
	var produced string
	assignmentPrepareWork = func(root, scope string, req workRequest) (any, error) {
		value, err := old(root, scope, req)
		if err != nil {
			return nil, err
		}
		produced = value.(map[string]any)["receipt"].(string)
		filler := assignmentContext{ID: "audit-last-slot", Group: "audit", Role: "worker", Stage: "work", Purpose: "Synthetic capacity"}
		err = saveAssignment(dir, assignmentRecord{Context: filler, At: time.Unix(1000, 0), Bindings: map[string]assignmentBinding{}, Used: map[string]notebook.CollectiveBaseline{}, Events: map[string]assignmentSavedEvent{}})
		return value, err
	}
	_, e = workCall(t, b, "prepare", map[string]any{"assignment": assignmentInput("audit-rejected", "audit", "worker", "work"), "query": "routing"})
	if e == nil {
		t.Fatal("expected final capacity rejection")
	}
	token, err := workDecode(produced)
	if err != nil {
		t.Fatal(err)
	}
	sessions, err := openWorkSessions(b.Root)
	if err != nil {
		t.Fatal(err)
	}
	defer sessions.Close()
	w, name, err := loadWorkState(sessions, b.Scope, token)
	if err != nil {
		t.Log("rejected session is unavailable")
		return
	}
	active, delivered := w.Nonce != "", uint64(0)
	if w.Delivery != nil {
		delivered = w.Delivery.Sequence
	}
	registered := len(w.Articles)
	state, e2 := readPlanningSession(sessions, name)
	if e2 != nil {
		t.Fatal(e2)
	}
	reads := 0
	if state.Planning.Knowledge != nil {
		reads = len(state.Planning.Knowledge.Read)
	}
	t.Logf("final_error=%v active_receipt=%v delivery=%d registered_articles=%d full_reads=%d", e, active, delivered, registered, reads)
	if active && delivered > 0 && registered > 0 {
		t.Fatal("rejected assignment retained active knowledge delivery")
	}
}

func TestAssignmentPrepareFailureRetiresOnlyNewSession(t *testing.T) {
	for _, kind := range []string{"inventory-lock", "sync"} {
		for _, extend := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/extend%v", kind, extend), func(t *testing.T) {
				b, _ := loopBook(t)
				peer, err := workCall(t, b, "prepare", map[string]any{"assignment": assignmentInput("peer", "case", "worker", "work"), "query": "unmatched"})
				if err != nil {
					t.Fatal(err)
				}
				a := assignmentInput("current", "case", "worker", "work")
				req := map[string]any{"assignment": a, "query": "unmatched"}
				if extend {
					p, err := workCall(t, b, "prepare", req)
					if err != nil {
						t.Fatal(err)
					}
					req["receipt"] = p["receipt"]
				}
				dir, err := assignmentDir(b.Root, b.Scope)
				if err != nil {
					t.Fatal(err)
				}
				defer dir.Close()
				old, oldSync := assignmentPrepareWork, store.SyncTemp
				t.Cleanup(func() { assignmentPrepareWork = old; store.SyncTemp = oldSync })
				var produced string
				var release func()
				assignmentPrepareWork = func(root, scope string, req workRequest) (any, error) {
					v, e := old(root, scope, req)
					if e != nil {
						return v, e
					}
					produced = v.(map[string]any)["receipt"].(string)
					if kind == "inventory-lock" {
						release, e = workLock(dir, "assignments")
					} else {
						store.SyncTemp = func(*os.File) error { return errors.New("synthetic final sync failure") }
					}
					return v, e
				}
				_, err = workCall(t, b, "prepare", req)
				if release != nil {
					release()
				}
				assignmentPrepareWork, store.SyncTemp = old, oldSync
				if err == nil {
					t.Fatal("expected final publication failure")
				}
				sessions, err := openWorkSessions(b.Root)
				if err != nil {
					t.Fatal(err)
				}
				defer sessions.Close()
				token, e := workDecode(produced)
				if e != nil {
					t.Fatal(e)
				}
				_, _, e = loadWorkState(sessions, b.Scope, token)
				if extend && e != nil {
					t.Fatal("an existing receipt must remain usable", e)
				}
				if !extend && e == nil {
					t.Fatal("a newly prepared but unreturned receipt must be retired")
				}
				peerToken, _ := workDecode(peer["receipt"].(string))
				if _, _, e = loadWorkState(sessions, b.Scope, peerToken); e != nil {
					t.Fatal("another assignment must survive", e)
				}
				if _, err = workCall(t, b, "prepare", req); err != nil {
					t.Fatal("the same request must retry after publication recovers", err)
				}
			})
		}
	}
}

func TestAssignmentRetirementChecksCurrentNonce(t *testing.T) {
	b, _ := loopBook(t)
	p, err := workCall(t, b, "prepare", map[string]any{"assignment": assignmentInput("current", "case", "worker", "work"), "query": "unmatched"})
	if err != nil {
		t.Fatal(err)
	}
	token, _ := workDecode(p["receipt"].(string))
	other := token
	other.Nonce = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	if err = retireAssignmentPreparation(b.Root, b.Scope, workEncode(other)); err == nil {
		t.Fatal("a different nonce cannot retire the current execution")
	}
	dir, err := openWorkSessions(b.Root)
	if err != nil {
		t.Fatal(err)
	}
	defer dir.Close()
	if _, _, err = loadWorkState(dir, b.Scope, token); err != nil {
		t.Fatal("the original receipt must remain usable", err)
	}
}
