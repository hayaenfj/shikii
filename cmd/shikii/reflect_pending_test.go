package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/notebook"
)

// pendingReflection is a project where several conversations wait for the reflection: each has a decision to score, the lessons
// near the two oldest are searched by the full query, a captured observation is unsorted, and the lesson has never been reviewed
// (a maintenance task). It returns the store, the scope and the id of the oldest pending conversation.
func pendingReflection(t *testing.T) (root, scope, oldest string) {
	t.Helper()
	st, scope, _, _, _ := memoryEffectFixture(t) // conversation 1 with its decision, and a lesson made from it
	for n := 2; n <= 5; n++ {
		reviewSeed(t, st, scope, n)
	}
	if _, err := (notebook.Book{Root: st.Dir, Scope: scope}).Capture(notebook.Observation{Kind: "test", Title: "A check that waits for sorting", Content: "The synthetic check passed.", Reference: "test", Version: "v1", ObservedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}); err != nil {
		t.Fatal(err)
	}
	full, err := reflectQueryWith(st.Dir, scope, 0)
	if err != nil || len(full.Sources) < 4 || len(full.Lessons) == 0 || len(full.Maintenance) == 0 || len(full.Observations) == 0 {
		t.Fatalf("the fixture has pending conversations, lessons near them, a maintenance task and an observation: sources %d lessons %d maintenance %d observations %d %v",
			len(full.Sources), len(full.Lessons), len(full.Maintenance), len(full.Observations), err)
	}
	return st.Dir, scope, full.Sources[0].Source.ID
}

// breakMaintenance makes the wiki's review log unreadable, so the full reflection query fails in its maintenance step.
func breakMaintenance(t *testing.T, root, scope string) {
	t.Helper()
	logs, err := filepath.Glob(filepath.Join(root, "wiki", "lessons", "*"))
	if err != nil || len(logs) == 0 {
		t.Fatalf("the wiki folder: %v %v", logs, err)
	}
	for _, dir := range logs {
		if err := os.WriteFile(filepath.Join(dir, ".maintenance.json"), []byte("{not json"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := reflectQueryWith(root, scope, 0); err == nil {
		t.Fatal("the full query fails once the review log is unreadable")
	}
}

// What the hooks read from the reflection query is what the full query lists: the same conversations in the same order with the
// same decisions to score and deliveries to assess, the same observations, the same count beyond the heads. Only the lessons near
// the conversations and the maintenance tasks are left out, and rest runs those steps without an error.
func TestReflectPendingListsWhatTheFullQueryLists(t *testing.T) {
	root, scope, _ := pendingReflection(t)
	for _, headsMax := range []int{reflectHeadsMax, 0, 1} {
		full, err := reflectQueryWith(root, scope, headsMax)
		if err != nil {
			t.Fatal(err)
		}
		pending, rest, err := reflectPending(root, scope, headsMax)
		if err != nil {
			t.Fatal(err)
		}
		if len(pending.Lessons) != 0 || len(pending.Maintenance) != 0 {
			t.Fatalf("heads %d: the pending view leaves the lessons and the maintenance tasks out: %d %d", headsMax, len(pending.Lessons), len(pending.Maintenance))
		}
		full.Lessons, full.Maintenance = pending.Lessons, pending.Maintenance
		want, _ := json.Marshal(full)
		got, _ := json.Marshal(pending)
		if string(got) != string(want) {
			t.Fatalf("heads %d: apart from the lessons and the maintenance tasks the two views are the same:\n got %s\nwant %s", headsMax, got, want)
		}
		if err := rest(); err != nil {
			t.Fatalf("heads %d: the left-out steps run without an error on a healthy wiki: %v", headsMax, err)
		}
	}
}

// At Stop, a reflection that is finished stays finished when the wiki's maintenance cannot be read: nothing is left, so the steps
// the hook does not need are not run. When something is left, the Stop is about to send the agent to the full query, so that
// query's failure is still the answer (the turn is not stopped on a reflection the agent could not do).
func TestReflectLeftFailsOnlyWhenItWouldStopTheTurn(t *testing.T) {
	root, scope, oldest := pendingReflection(t)
	breakMaintenance(t, root, scope)
	left, err := reflectLeft(root, scope, []string{"ffffffffffffffff"}) // asked about a conversation that is no longer pending
	if err != nil || len(left) != 0 {
		t.Fatalf("nothing is left and nothing fails: %v %v", left, err)
	}
	if left, err := reflectLeft(root, scope, []string{oldest}); err == nil {
		t.Fatalf("a conversation is left, so the full query's failure is returned: %v", left)
	}
}

// Before the request, asking for the reflection still fails when the query the agent is sent to would fail: nothing is asked and
// the turn records no asked conversations, as before.
func TestReflectContextStillFailsWhenTheQueryItAsksForWould(t *testing.T) {
	root, scope, oldest := pendingReflection(t)
	context, ids, err := reflectContext(root, scope)
	if err != nil || context == "" || len(ids) == 0 || ids[0] != oldest {
		t.Fatalf("a healthy wiki with pending conversations asks for the reflection, oldest first: %q %v %v", context, ids, err)
	}
	breakMaintenance(t, root, scope)
	if context, ids, err := reflectContext(root, scope); err == nil || context != "" || ids != nil {
		t.Fatalf("with an unreadable review log nothing is asked and the failure is returned: %q %v %v", context, ids, err)
	}
}
