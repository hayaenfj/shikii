package main

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// 0.9.3: restates reaches any earlier request of this session and project (the person decided: no limit on how far back; the agent judges
// which request it restates). hook plan query still shows only the recent turns; shikii looks the named request up in what it recorded
// for this session itself.

// restateFarShort are seven new requests, each under 40 characters (so request 1 gets no outcome from them) and not acknowledgements.
var restateFarShort = []string{"add a test too", "log the errors", "add a dry-run flag", "rename the output file", "sort the output by date", "add a header row", "write it in the README too"}

// restateFarTurns runs request 1 and the seven short requests (each organized as a request of its own, with its Stop) in repo, and returns
// request 1's turn id and done-as-asked trace.
func restateFarTurns(t *testing.T, repo, root, session string) (string, string) {
	t.Helper()
	turn, done := "", ""
	for i, prompt := range append([]string{restateFirst}, restateFarShort...) {
		runHook(t, "user-prompt-submit", map[string]any{"session_id": session, "cwd": repo, "prompt": prompt})
		if i > 0 {
			restateSubmit(t, root, session, "")
		}
		runHook(t, "stop", map[string]any{"session_id": session, "cwd": repo, "last_assistant_message": "Done."})
		if i == 0 {
			s := loadSession(root, session)
			turn = s.TurnID
			for _, r := range s.Reviews {
				if r.Spec == "done-as-asked" {
					done = r.Trace
				}
			}
		}
	}
	if turn == "" || done == "" {
		t.Fatal("request 1 must have a turn and a done-as-asked decision")
	}
	if l, ok := doneLabel(t, root, done); ok {
		t.Fatalf("request 1 must have no outcome before its restatement: %+v", l)
	}
	return turn, done
}

func TestRestatesReachesRequestEightTurnsBack(t *testing.T) {
	t.Run("similar", func(t *testing.T) {
		repo, root, _ := planningFixture(t)
		turn, done := restateFarTurns(t, repo, root, "planning")
		runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": restateAgain})
		out, err := planningCall(t, "query", map[string]any{"session_id": "planning", "turn_id": loadSession(root, "planning").TurnID})
		if err != nil {
			t.Fatalf("plan query: %v", err)
		}
		// what the agent reads stays the bounded recent turns: request 1 is not listed, and nothing else is added
		n := 0
		for key := range out["source_material"].(map[string]any) {
			if strings.HasPrefix(key, "history:") && strings.HasSuffix(key, ":user") {
				n++
			}
		}
		raw, _ := json.Marshal(out)
		if n != 4 || strings.Contains(string(raw), turn) || strings.Contains(string(raw), "\"earlier\"") {
			t.Fatalf("plan query must list the same 4 recent turns and nothing about request 1 (8 turns back): %d turns, %.800s", n, raw)
		}
		restateSubmit(t, root, "planning", turn)
		if l, ok := doneLabel(t, root, done); !ok || l.Truth != "no" || l.Weight != 1 || l.Source != "outcome" {
			t.Fatalf("a restatement of request 1 eight turns back, named by restates, must give it \"no\": %+v %v", l, ok)
		}
	})
	t.Run("not similar", func(t *testing.T) {
		repo, root, _ := planningFixture(t)
		turn, done := restateFarTurns(t, repo, root, "planning")
		runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": restateOther})
		restateSubmit(t, root, "planning", turn)
		runHook(t, "stop", map[string]any{"session_id": "planning", "cwd": repo, "last_assistant_message": "Done."})
		if l, ok := doneLabel(t, root, done); ok {
			t.Fatalf("a message not similar to the named request gives it nothing: %+v", l)
		}
	})
}

func TestRestatesFarIgnoresForeignTurns(t *testing.T) {
	t.Run("another session", func(t *testing.T) {
		repo, root, _ := planningFixture(t)
		turn, done := restateFarTurns(t, repo, root, "planning")
		restateFarTurns(t, repo, root, "other")
		runHook(t, "user-prompt-submit", map[string]any{"session_id": "other", "cwd": repo, "prompt": restateAgain})
		restateSubmit(t, root, "other", turn)
		if l, ok := doneLabel(t, root, done); ok {
			t.Fatalf("restates naming another session's request is ignored: %+v", l)
		}
	})
	t.Run("another project", func(t *testing.T) {
		repo, root, _ := planningFixture(t)
		turn, done := restateFarTurns(t, repo, root, "planning")
		repo2 := gitRepo(t) // the same session id moves to another project, with its own store
		root2, _ := initTestProject(t, repo2)
		usePlanningSpec(t, root2)
		t.Chdir(repo2)
		runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo2, "prompt": restateAgain})
		restateSubmit(t, root2, "planning", turn)
		if l, ok := doneLabel(t, root, done); ok {
			t.Fatalf("restates naming a request of another project is ignored: %+v", l)
		}
		if l, ok := doneLabel(t, root2, done); ok {
			t.Fatalf("restates naming a request of another project is ignored: %+v", l)
		}
	})
	// one store can hold several projects (scopes): a request recorded under another scope is not looked up
	t.Run("another scope in the same record", func(t *testing.T) {
		repo, root, _ := planningFixture(t)
		turn, done := restateFarTurns(t, repo, root, "planning")
		previous := loadSession(root, "planning")
		f := &planningFrame{TurnID: "next", At: time.Now(), Sources: map[string]string{"current_user": restateAgain}}
		addPlanningContext(previous.Scope+"-elsewhere", previous, f)
		s := &hookSession{Scope: previous.Scope + "-elsewhere", Planning: f}
		if restateDone(root, s, turn) {
			t.Fatal("restates must not find a request recorded under another project")
		}
		if l, ok := doneLabel(t, root, done); ok {
			t.Fatalf("restates naming a request of another project is ignored: %+v", l)
		}
		// the same record and the same project finds it (so the case above fails for the right reason)
		f = &planningFrame{TurnID: "next", At: time.Now(), Sources: map[string]string{"current_user": restateAgain}}
		addPlanningContext(previous.Scope, previous, f)
		if !restateDone(root, &hookSession{Scope: previous.Scope, Planning: f}, turn) {
			t.Fatal("restates must find request 1 in the same session and project")
		}
	})
	t.Run("made-up id", func(t *testing.T) {
		repo, root, _ := planningFixture(t)
		_, done := restateFarTurns(t, repo, root, "planning")
		runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": restateAgain})
		restateSubmit(t, root, "planning", "0123456789abcdef")
		if l, ok := doneLabel(t, root, done); ok {
			t.Fatalf("restates naming an unknown request is ignored: %+v", l)
		}
	})
}
