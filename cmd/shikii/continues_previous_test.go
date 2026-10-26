package main

import (
	"strings"
	"testing"

	"github.com/hayaenfj/shikii/internal/store"
)

// 0.9.3: whether a message continues the previous request is decided by the agent in its pre-start organizing
// (continues_previous in hook plan submit). The fixed acknowledgement list is only the provisional decision and the fallback.

const continuesFirst = "Use the agreed CSV columns and keep the existing filters."

// continuesTurn runs the first request and its Stop (which records done-as-asked), then the person's next message.
// It returns the done-as-asked trace of the first request.
func continuesTurn(t *testing.T, repo, root, reply string) string {
	t.Helper()
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": continuesFirst})
	runHook(t, "stop", map[string]any{"session_id": "planning", "cwd": repo, "last_assistant_message": "Kept the filters and used the agreed columns."})
	done := ""
	for _, r := range loadSession(root, "planning").Reviews {
		if r.Spec == "done-as-asked" {
			done = r.Trace
		}
	}
	if done == "" {
		t.Fatalf("the first request must have a done-as-asked decision: %+v", loadSession(root, "planning"))
	}
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": reply})
	return done
}

func continuesSubmit(t *testing.T, root string, continues bool) {
	t.Helper()
	payload := map[string]any{
		"session_id": "planning", "turn_id": loadSession(root, "planning").TurnID,
		"agreed":             map[string]any{"unknown": true},
		"open":               map[string]any{"unknown": true},
		"continues_previous": continues,
	}
	if _, err := planningCall(t, "submit", payload); err != nil {
		t.Fatalf("plan submit: %v", err)
	}
}

func doneLabel(t *testing.T, root, trace string) (store.Label, bool) {
	t.Helper()
	m, err := (store.Store{Dir: root}).LabeledTraces("done-as-asked")
	if err != nil {
		t.Fatal(err)
	}
	l, ok := m[trace]
	return l, ok
}

// a short request with content (the list treats it as new) that the agent says continues the previous one is attached to it,
// and the previous request's outcome is not settled from it (not at plan submit, not at the turn's Stop)
func TestPlanContinuesMergesShortRequest(t *testing.T) {
	for _, reply := range []string{"main取り込んで", "それでいこう"} { // "merge main", "let's go with that" (not in the list)
		t.Run(reply, func(t *testing.T) {
			repo, root, _ := planningFixture(t)
			done := continuesTurn(t, repo, root, reply)
			if s := loadSession(root, "planning"); s.Request != reply {
				t.Fatalf("provisional decision: the list treats %q as new, got %q", reply, s.Request)
			}
			continuesSubmit(t, root, true)
			s := loadSession(root, "planning")
			if s.Request != continuesFirst+" / "+reply || s.RequestText != continuesFirst+" / "+reply {
				t.Fatalf("the agent said it continues: must be attached to the previous request, got %q / %q", s.Request, s.RequestText)
			}
			if l, ok := doneLabel(t, root, done); ok {
				t.Fatalf("a continuation must not settle the previous request's outcome: %+v", l)
			}
			runHook(t, "stop", map[string]any{"session_id": "planning", "cwd": repo, "last_assistant_message": "Merged."})
			if s := loadSession(root, "planning"); !s.Judged || s.Completed == nil || s.Completed.Request != continuesFirst+" / "+reply {
				t.Fatalf("the Stop must judge the attached request (listing kept for it): %+v", s)
			}
			if l, ok := doneLabel(t, root, done); ok {
				t.Fatalf("the Stop must not settle the previous outcome from a continuation: %+v", l)
			}
		})
	}
}

// a list acknowledgement that the agent says is a new request becomes its own request, and the previous outcome is settled from it
func TestPlanNewSplitsAcknowledgement(t *testing.T) {
	repo, root, _ := planningFixture(t)
	done := continuesTurn(t, repo, root, "ok")
	if s := loadSession(root, "planning"); s.Request != continuesFirst+" / ok" {
		t.Fatalf("provisional decision: the list attaches ok, got %q", s.Request)
	}
	continuesSubmit(t, root, false)
	s := loadSession(root, "planning")
	if s.Request != "ok" || s.RequestText != "ok" {
		t.Fatalf("the agent said it is new: must stand alone, got %q / %q", s.Request, s.RequestText)
	}
	if l, ok := doneLabel(t, root, done); ok {
		t.Fatalf("the grouping never decides a truth: an acknowledgement does not settle the previous outcome (audit 3): %+v", l)
	}
	runHook(t, "stop", map[string]any{"session_id": "planning", "cwd": repo, "last_assistant_message": "Done."})
	if s := loadSession(root, "planning"); !s.Judged || s.Completed == nil || s.Completed.Request != "ok" {
		t.Fatalf("the Stop must judge the request standing alone: %+v", s)
	}
}

// without an organizing, the list decides (today's behaviour), settled at the turn's Stop at the latest
func TestNoPlanKeepsListDecision(t *testing.T) {
	t.Run("content", func(t *testing.T) {
		repo, root, _ := planningFixture(t)
		done := continuesTurn(t, repo, root, "main取り込んで")
		runHook(t, "stop", map[string]any{"session_id": "planning", "cwd": repo, "last_assistant_message": "Merged main."})
		if s := loadSession(root, "planning"); s.Request != "main取り込んで" {
			t.Fatalf("no organizing: the list keeps it as its own request, got %q", s.Request)
		}
		if l, ok := doneLabel(t, root, done); ok {
			t.Fatalf("a short new request is too short to show the person moved on (audit 3): %+v", l)
		}
		// an organizing that arrives after the Stop judged the turn no longer moves the request
		if _, err := planningCall(t, "submit", map[string]any{"session_id": "planning", "turn_id": loadSession(root, "planning").TurnID, "agreed": map[string]any{"unknown": true}, "open": map[string]any{"unknown": true}, "continues_previous": true}); err == nil {
			if s := loadSession(root, "planning"); s.Request != "main取り込んで" {
				t.Fatalf("a late organizing must not regroup a judged turn: %q", s.Request)
			}
		}
	})
	t.Run("long", func(t *testing.T) {
		repo, root, _ := planningFixture(t)
		done := continuesTurn(t, repo, root, "次の版のリリースノートを書いて、0.9.2 からの変更をすべて並べ、英語と日本語の両方で用意してください。")
		runHook(t, "stop", map[string]any{"session_id": "planning", "cwd": repo, "last_assistant_message": "Wrote them."})
		if l, ok := doneLabel(t, root, done); !ok || l.Truth != "yes" || l.Source != "outcome" {
			t.Fatalf("a long, different request settles the previous outcome by the Stop: %+v %v", l, ok)
		}
	})
	t.Run("ack", func(t *testing.T) {
		repo, root, _ := planningFixture(t)
		done := continuesTurn(t, repo, root, "ok")
		runHook(t, "stop", map[string]any{"session_id": "planning", "cwd": repo, "last_assistant_message": "Continued."})
		if s := loadSession(root, "planning"); s.Request != continuesFirst+" / ok" {
			t.Fatalf("no organizing: the list attaches ok, got %q", s.Request)
		}
		if l, ok := doneLabel(t, root, done); ok {
			t.Fatalf("no organizing: an acknowledgement does not settle the previous outcome: %+v", l)
		}
	})
}

// the agent is told about continues_previous where it is asked to organize, and where the Stop asks again (planningShape)
func TestPlanningNoticeNamesContinuesPrevious(t *testing.T) {
	if !strings.Contains(planningShape, "continues_previous is true, false or left out") {
		t.Fatalf("the shape must show continues_previous: %s", planningShape)
	}
	if n := planningNotice("s", "0123456789abcdef"); !strings.Contains(n, "continues_previous to true") || !strings.Contains(n, "false when it is a new request") || !strings.Contains(n, "omit it when unsure") {
		t.Fatalf("the notice must say when to set continues_previous and when to omit it: %s", n)
	}
}

// continues_previous for another turn or another session changes nothing
func TestPlanContinuesOtherTurnOrSessionIgnored(t *testing.T) {
	repo, root, _ := planningFixture(t)
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": continuesFirst})
	oldTurn := loadSession(root, "planning").TurnID
	runHook(t, "stop", map[string]any{"session_id": "planning", "cwd": repo, "last_assistant_message": "Done."})
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": "main取り込んで"})
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "other", "cwd": repo, "prompt": "Write the release notes for the next version."})
	runHook(t, "stop", map[string]any{"session_id": "other", "cwd": repo, "last_assistant_message": "Wrote them."})
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "other", "cwd": repo, "prompt": "main取り込んで"})
	for _, p := range []map[string]any{
		{"session_id": "planning", "turn_id": oldTurn},
		{"session_id": "other", "turn_id": loadSession(root, "planning").TurnID},
	} {
		p["agreed"], p["open"], p["continues_previous"] = map[string]any{"unknown": true}, map[string]any{"unknown": true}, true
		if _, err := planningCall(t, "submit", p); err == nil {
			t.Fatalf("an organizing for another turn or session must be refused: %v", p)
		}
	}
	if s := loadSession(root, "planning"); s.Request != "main取り込んで" {
		t.Fatalf("another turn's organizing must not regroup this one: %q", s.Request)
	}
	if s := loadSession(root, "other"); s.Request != "main取り込んで" {
		t.Fatalf("another session's organizing must not regroup this one: %q", s.Request)
	}
}
