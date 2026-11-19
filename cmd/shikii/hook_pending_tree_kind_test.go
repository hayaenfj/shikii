package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hayaenfj/shikii/internal/store"
)

// A request keeps the hash of the working tree for the outcome truth ("did files change since"), in a session that can outlive an
// update. Before treeHashPrefix the hash was that of the tree id `git write-tree` gave (16 hex digits, no prefix); the two kinds say
// nothing about each other, so a request kept with the old kind is dropped without a truth instead of being settled as "edit".
func TestAPendingHashOfTheOldKindIsNotSettledAsChanged(t *testing.T) {
	model := fakeAgentModel(t, func(string) float64 { return 0.1 })
	repo := gitRepo(t)
	agent := hookEnv(t, model, repo)
	st := store.Store{Dir: agent}
	const request = "translate the README into German in the background and tell me when it is done"
	for _, c := range []struct {
		session   string
		oldKind   bool
		wantLabel bool
	}{
		{"hash-old", true, false}, // kept by the version before the update: not comparable, no truth
		{"hash-new", false, true}, // the same flow with this version's hash: the changed file is an edit (the control)
	} {
		runHook(t, "user-prompt-submit", map[string]any{"session_id": c.session, "cwd": repo, "prompt": request})
		sess := loadSession(agent, c.session)
		if len(sess.Pending) != 1 || !strings.HasPrefix(sess.Pending[0].Tree, treeHashPrefix) {
			t.Fatalf("premise: the request keeps this version's hash: %+v", sess.Pending)
		}
		if c.oldKind {
			if err := updateSession(agent, c.session, func(s *hookSession) { s.Pending[0].Tree = "0123456789abcdef" }); err != nil {
				t.Fatal(err)
			}
		}
		trace := loadSession(agent, c.session).Pending[0].Trace
		if err := os.WriteFile(filepath.Join(repo, "README_"+c.session+".md"), []byte("changed\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		runHook(t, "user-prompt-submit", map[string]any{"session_id": c.session, "cwd": repo, "prompt": "thanks, now explain the Makefile"})
		labeled, _ := st.LabeledTraces("prompt-kind")
		l, got := labeled[trace]
		if got != c.wantLabel {
			t.Errorf("%s: outcome truth for the request %q: got %v (%+v), want %v", c.session, trace, got, l, c.wantLabel)
		}
		if got && (l.Truth != "edit" || l.Source != "outcome") {
			t.Errorf("%s: a changed file settles as an edit by outcome: %+v", c.session, l)
		}
	}
}
