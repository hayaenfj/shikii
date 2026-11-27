package main

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/hayaenfj/shikii/internal/store"
)

// restates must not name a request that was recorded for another project. The case "another project" of TestRestatesIgnoresForeignTurns
// failed under none of the mistakes tried (the check of the project removed, the session records shared by every project, both), for
// two reasons that hold together:
//   - the other project is made with its own store (initTestProject), which holds no record of the session and no record of the
//     request. Nothing of the first project reaches it, so the check that a turn is of this project (previous.Scope == scope in
//     addPlanningContext) is never what stops the request being named;
//   - if the turn did get there, naming it would still write nothing: restateDone labels through the store it is given
//     (FindRecord), which holds no record of that request, and the case reads the label of the other store and the one of the first
//     store, which restateDone is never given.
//
// Both were shown by running that case with the check removed and the session record copied into the other store: the turns were
// found (4 recent, 4 earlier), and no label appeared until the record of the request was copied as well.
//
// A project that holds the records of another is a real one: a folder copied together with its `.shikii` and made a project of its own
// with `shikii init` ("the wiki and records are kept", relinkMovedStore): its store keeps the session records and the request records
// of the project it was copied from, under the path of that project. A restatement there must not give a request of the original
// project its outcome.

// projectMadeOfACopy copies the project at repo (its `.shikii` with it) and makes the copy a project of its own, as a person does with
// `shikii init` in it, and returns the copy's folder and store. The store keeps what the original had: the records of the requests and
// the session records, whose scope is the original's path.
func projectMadeOfACopy(t *testing.T, repo string) (string, string) {
	t.Helper()
	repo2 := filepath.Join(t.TempDir(), "copy")
	copyFolder(t, repo, repo2)
	t.Chdir(repo2)
	if out := capture(t, func() error { return cmdInit(nil) }); !strings.Contains(out, "Made this folder a project of its own") {
		t.Fatalf("premise: init made the copy a project of its own: %.300s", out)
	}
	return repo2, filepath.Join(repo2, ".shikii")
}

func TestRestatesIgnoresARequestOfTheProjectThisOneWasCopiedFrom(t *testing.T) {
	// restateAs runs the restatement of request 1 in the copy and returns the label request 1 got in the copy's store
	restateAs := func(t *testing.T, ownScope bool) (store.Label, bool) {
		t.Helper()
		repo, root, _ := planningFixture(t)
		turn, done := restateTurns(t, repo, root, "planning")
		repo2, root2 := projectMadeOfACopy(t, repo)
		s := loadSession(root2, "planning")
		if _, err := (store.Store{Dir: root2}).FindRecord("done-as-asked", done); err != nil || s.Scope != feedbackScope(repo) || len(s.Planning.Recent) == 0 {
			t.Fatalf("premise: the copy's store holds the request's record (%v) and the session record of the original project (scope %q, %d recent turns)", err, s.Scope, len(s.Planning.Recent))
		}
		if ownScope {
			s.Scope = feedbackScope(repo2)
			if err := saveSession(root2, "planning", s); err != nil {
				t.Fatal(err)
			}
		}
		runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo2, "prompt": restateAgain})
		restateSubmit(t, root2, "planning", turn)
		if l, ok := doneLabel(t, root, done); ok {
			t.Fatalf("premise: the original project's own store is not written: %+v", l)
		}
		return doneLabel(t, root2, done)
	}
	t.Run("a request of the project it was copied from", func(t *testing.T) {
		if l, ok := restateAs(t, false); ok {
			t.Fatalf("restates naming a request recorded for another project is ignored: %+v", l)
		}
	})
	// the same flow with the session record under the copy's own path: the request is named and gets "no", so the case above fails for
	// the right reason (the label can be written in this store; the project the record is of is the only difference)
	t.Run("control: a request recorded for this project", func(t *testing.T) {
		if l, ok := restateAs(t, true); !ok || l.Truth != "no" || l.Weight != 1 || l.Source != "outcome" {
			t.Fatalf("a restatement of request 1 named by restates must give it \"no\": %+v %v", l, ok)
		}
	})
}
