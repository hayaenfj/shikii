package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/hayaenfj/shikii/internal/decide"
	"github.com/hayaenfj/shikii/internal/store"
)

// 終わった後の 2 問にも結果の正解が付く: 変えたファイルが次の依頼の間に戻されたら範囲の外、同じ依頼の言い直しは終えていない。
func TestHookScopeAndDoneGetOutcomeTruths(t *testing.T) {
	model := fakeAgentModel(t, func(string) float64 { return 0.6 }) // 迷い(線の無い ask)
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

	// A: 足したファイルを次の依頼の間に消した → 範囲の外(no、重み 1)。次の発言ははっきり別の依頼 → 終えた(yes、部分)
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

	// B: 同じ依頼の言い直し → 終えていない(no、重み 1)。変えたファイルが次の次の依頼まで残った → 範囲の中(yes、部分)
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

	// D: 裏の作業の通知で同じ依頼が続き、エージェントが自分で戻したものは「人が戻した」に数えない
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

	// C: 短い返事は終えたかを決めない
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

// 人の答えを待つのは、結果から正解が付かない判定の迷いと、線のある判定の抜き取りだけ。
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
