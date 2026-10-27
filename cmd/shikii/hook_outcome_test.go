package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/decide"
	"github.com/hayaenfj/shikii/internal/store"
)

// The 2 end-of-turn questions also get outcome truths: if the changed files are reverted before the next request, outside the scope; a restatement of the same request, not finished.
func TestHookScopeAndDoneGetOutcomeTruths(t *testing.T) {
	model := fakeAgentModel(t, func(string) float64 { return 0.6 }) // unsure (ask without a line)
	repo := gitRepo(t)
	agent := hookEnv(t, model, repo)
	st := store.Store{Dir: agent}
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(repo, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	trace := func(session, specID string) string {
		for _, r := range loadSession(agent, session).Reviews {
			if r.Spec == specID {
				return r.Trace
			}
		}
		t.Fatalf("%s: no %s decision in %+v", session, specID, loadSession(agent, session))
		return ""
	}
	labels := func(specID string) map[string]store.Label {
		m, err := st.LabeledTraces(specID)
		if err != nil {
			t.Fatal(err)
		}
		return m
	}

	// A: the added file was removed before the next request → outside the scope (no, weight 1). The next message is clearly a different request → finished (yes, partial)
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "a", "cwd": repo, "prompt": "add a greeting file named hello.txt to the repository"})
	write("hello.txt", "hi")
	runHook(t, "stop", map[string]any{"session_id": "a", "cwd": repo, "last_assistant_message": "Added hello.txt."})
	scopeA, doneA := trace("a", "changes-in-scope"), trace("a", "done-as-asked")
	if w := loadSession(agent, "a").Watch; len(w) != 1 || w[0].Trace != scopeA || w[0].Before["hello.txt"] != "" {
		t.Fatalf("the changed file must be watched with its state before the turn: %+v", w)
	}
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "a", "cwd": repo, "prompt": "Why is the build matrix in CI so slow on macOS runners lately?"})
	if l := labels("done-as-asked")[doneA]; l.Truth != "yes" || l.Weight != outcomeKeptWeight || l.Source != "outcome" {
		t.Fatalf("a clearly different next request must give done-as-asked yes w=%v: %+v", outcomeKeptWeight, l)
	}
	if _, ok := labels("changes-in-scope")[scopeA]; ok {
		t.Fatal("scope must wait until the next turn has run")
	}
	if err := os.Remove(filepath.Join(repo, "hello.txt")); err != nil {
		t.Fatal(err)
	}
	runHook(t, "stop", map[string]any{"session_id": "a", "cwd": repo, "last_assistant_message": "Removed it."})
	if l := labels("changes-in-scope")[scopeA]; l.Truth != "no" || l.Weight != 1 || l.Source != "outcome" {
		t.Fatalf("a change undone in the next turn must give changes-in-scope no w=1: %+v", l)
	}
	for _, w := range loadSession(agent, "a").Watch {
		if w.Trace == scopeA {
			t.Fatalf("a settled watch must be dropped: %+v", w)
		}
	}

	// B: a restatement of the same request → not finished (no, weight 1). The changed file stayed until the request after next → inside the scope (yes, partial)
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "b", "cwd": repo, "prompt": "fix the off-by-one error in the pagination helper"})
	write("page.go", "package page\n")
	runHook(t, "stop", map[string]any{"session_id": "b", "cwd": repo, "last_assistant_message": "Fixed."})
	scopeB, doneB := trace("b", "changes-in-scope"), trace("b", "done-as-asked")
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "b", "cwd": repo, "prompt": "please fix the off-by-one error in the pagination helper"})
	if l := labels("done-as-asked")[doneB]; l.Truth != "no" || l.Weight != 1 {
		t.Fatalf("repeating the request must give done-as-asked no w=1: %+v", l)
	}
	runHook(t, "stop", map[string]any{"session_id": "b", "cwd": repo, "last_assistant_message": "Fixed again."})
	if _, ok := labels("changes-in-scope")[scopeB]; ok {
		t.Fatal("a kept change must not be settled before the second request")
	}
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "b", "cwd": repo, "prompt": "now write release notes describing the newest version for the users"})
	if l := labels("changes-in-scope")[scopeB]; l.Truth != "yes" || l.Weight != outcomeKeptWeight {
		t.Fatalf("a change kept through the next turn must give changes-in-scope yes w=%v: %+v", outcomeKeptWeight, l)
	}

	// D: when the same request continues via a background notification, what the agent reverted itself does not count as "reverted by the person"
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "d", "cwd": repo, "prompt": "draft a scratch file with ideas for the parser rewrite"})
	write("scratch.txt", "ideas")
	runHook(t, "stop", map[string]any{"session_id": "d", "cwd": repo, "last_assistant_message": "Drafted."})
	scopeD := trace("d", "changes-in-scope")
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "d", "cwd": repo, "prompt": "<task-notification>background job finished</task-notification>"})
	if err := os.Remove(filepath.Join(repo, "scratch.txt")); err != nil {
		t.Fatal(err)
	}
	runHook(t, "stop", map[string]any{"session_id": "d", "cwd": repo, "last_assistant_message": "Cleaned up."})
	if l, ok := labels("changes-in-scope")[scopeD]; ok {
		t.Fatalf("an undo within the same request is not the person's undo: %+v", l)
	}

	// C: a short reply does not decide whether it finished
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "c", "cwd": repo, "prompt": "rename the config loader to something clearer"})
	runHook(t, "stop", map[string]any{"session_id": "c", "cwd": repo, "last_assistant_message": "Renamed."})
	doneC := trace("c", "done-as-asked")
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "c", "cwd": repo, "prompt": "ok"})
	if l, ok := labels("done-as-asked")[doneC]; ok {
		t.Fatalf("a short reply must not settle done-as-asked: %+v", l)
	}
}

func TestTextSimilarity(t *testing.T) {
	for _, c := range []struct {
		a, b     string
		min, max float64
	}{
		{"fix the off-by-one error in the pagination helper", "please fix the off-by-one error in the pagination helper", outcomeRepeatAbove, 1},
		{"READMEの誤字を直して", "READMEの誤字を直してください", outcomeRepeatAbove, 1},
		{"add a greeting file named hello.txt", "Why is the CI matrix so slow on macOS?", 0, outcomeMovedBelow},
		{"", "", 0, 0},
	} {
		if got := textSimilarity(c.a, c.b); got < c.min || got > c.max {
			t.Errorf("textSimilarity(%q, %q) = %.2f, want %.2f..%.2f", c.a, c.b, got, c.min, c.max)
		}
	}
}

// Only the unsure decisions that get no truth from outcomes and spot checks of calibrated decisions wait for the person's answer.
func TestWaitsForPersonFor(t *testing.T) {
	audit := ""
	for i := 0; audit == ""; i++ {
		if id := store.NewTraceID(); store.AuditSample(id) {
			audit = id
		}
	}
	for _, c := range []struct {
		spec string
		d    decide.Decision
		want bool
	}{
		{"needs-plan", decide.Decision{Band: decide.Ask}, true},
		{"prompt-kind", decide.Decision{Band: decide.Ask}, false},
		{"changes-in-scope", decide.Decision{Band: decide.Ask}, false},
		{"done-as-asked", decide.Decision{Band: decide.Ask}, false},
		{"needs-plan", decide.Decision{Band: decide.Act, IsCalibrated: true, TraceID: audit}, true},
		{"prompt-kind", decide.Decision{Band: decide.Act, IsCalibrated: true, TraceID: audit}, true},
		{"needs-plan", decide.Decision{Band: decide.Discard}, false},
	} {
		if got := waitsForPersonFor(c.spec, &c.d); got != c.want {
			t.Errorf("%s %s calibrated=%v: got %v, want %v", c.spec, c.d.Band, c.d.IsCalibrated, got, c.want)
		}
	}
}

// A spec whose line could not be drawn does not repeat auto-calibration until truths grow by hookRecalibrateEvery (replaying every time only spends machine time).
func TestHookDoesNotRetryAFailedCalibrationUntilTruthsGrow(t *testing.T) {
	model := fakeAgentModel(t, func(string) float64 { return 0.2 })
	repo := gitRepo(t)
	agent := hookEnv(t, model, repo)
	lowerMinN(t, agent, "prompt-kind", 2)
	calls := 0
	oldCal, oldGap := hookCalibrate, hookCalibrateGap
	hookCalibrate = func(agent, path, storeDir, model string) { calls++ } // no line could be drawn (the spec does not change)
	hookCalibrateGap = 0
	t.Cleanup(func() { hookCalibrate, hookCalibrateGap = oldCal, oldGap })
	turn := func(session string) {
		runHook(t, "user-prompt-submit", map[string]any{"session_id": session, "cwd": repo, "prompt": "explain what the parser in " + session + " does"})
		runHook(t, "stop", map[string]any{"session_id": session, "cwd": repo, "last_assistant_message": "Explained."})
	}
	for _, s := range []string{"s1", "s2", "s3"} {
		turn(s)
	}
	if calls != 1 {
		t.Fatalf("the first attempt at min_n must run once: %d", calls)
	}
	for i := 0; i < 3; i++ {
		turn(fmt.Sprintf("more%d", i))
	}
	if calls != 1 {
		t.Fatalf("a failed line must not be retried before %d more truths: %d calls", hookRecalibrateEvery, calls)
	}
	// if the wording changed (or the record is in an old format), count again
	guard := filepath.Join(agent, "calibrating-prompt-kind")
	if err := os.WriteFile(guard, []byte("2026-10-22T08:20:03+09:00 kev-4b\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	turn("after-old-guard")
	if calls != 2 {
		t.Fatalf("an old-format guard must not block: %d calls", calls)
	}
}

func TestRestatesNamesTheEarlierRequest(t *testing.T) {
	t.Run("similar", func(t *testing.T) {
		repo, root, _ := planningFixture(t)
		turn, done := restateTurns(t, repo, root, "planning")
		third := loadSession(root, "planning").Completed.Targets
		runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": restateAgain})
		restateSubmit(t, root, "planning", turn)
		if l, ok := doneLabel(t, root, done); !ok || l.Truth != "no" || l.Weight != 1 || l.Source != "outcome" {
			t.Fatalf("a restatement of request 1 named by restates must give request 1 \"no\": %+v %v", l, ok)
		}
		runHook(t, "stop", map[string]any{"session_id": "planning", "cwd": repo, "last_assistant_message": "Done."})
		for _, target := range third {
			if l, ok := doneLabel(t, root, target.TraceID); target.Spec == "done-as-asked" && ok {
				t.Fatalf("the previous request is not credited as done by a restatement of an older one: %+v", l)
			}
		}
	})
	t.Run("not similar", func(t *testing.T) {
		repo, root, _ := planningFixture(t)
		turn, done := restateTurns(t, repo, root, "planning")
		runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": restateOther})
		restateSubmit(t, root, "planning", turn)
		runHook(t, "stop", map[string]any{"session_id": "planning", "cwd": repo, "last_assistant_message": "Done."})
		if l, ok := doneLabel(t, root, done); ok {
			t.Fatalf("a message not similar to the named request gives it nothing: %+v", l)
		}
	})
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
