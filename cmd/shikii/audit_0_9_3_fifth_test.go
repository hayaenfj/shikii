package main

import (
	"strings"
	"testing"

	"github.com/hayaenfj/shikii/internal/store"
)

// audit 5 (0.9.3): a turn whose Stop asked for continues_previous is still judged when the Stop after the block never comes and the next request arrives
func TestAudit5AskedTurnIsJudgedWhenTheSecondStopNeverComes(t *testing.T) {
	repo, root, _ := planningFixture(t)
	continuesTurn(t, repo, root, "OKです")
	asked := loadSession(root, "planning").TurnID
	out := runHook(t, "stop", map[string]any{"session_id": "planning", "cwd": repo, "last_assistant_message": "Went ahead."})
	if _, block := stopReason(t, out); !block {
		t.Fatalf("premise: the Stop of a short message asks for continues_previous: %s", out)
	}
	// the person stopped the continuation (Esc): no Stop after the block, the next request comes
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": "次の版のリリースノートを書いて、0.9.2 からの変更をすべて並べ、英語と日本語の両方で用意してください。"})
	if s := loadSession(root, "planning"); s.TurnID == asked {
		t.Fatal("premise: the next request moves the turn on")
	}
	recs, err := (store.Store{Dir: root}).Records("done-as-asked", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 2 {
		t.Fatalf("the blocked turn must be judged with done-as-asked too: %d records", len(recs))
	}
}

// audit 5 (0.9.3): restates does not give "no" to a done request because a short command is said again after other work ("commit it" once more)
func TestAudit5RestatesIgnoresShortRepeats(t *testing.T) {
	repo, root, _ := planningFixture(t)
	session := "planning"
	turn, done := "", ""
	for i, prompt := range []string{"コミットして", "README の導入も直して"} {
		runHook(t, "user-prompt-submit", map[string]any{"session_id": session, "cwd": repo, "prompt": prompt})
		restateSubmit(t, root, session, "")
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
	if done == "" {
		t.Fatal("premise: the first request has a done-as-asked decision")
	}
	runHook(t, "user-prompt-submit", map[string]any{"session_id": session, "cwd": repo, "prompt": "コミットして"})
	restateSubmit(t, root, session, turn)
	if l, ok := doneLabel(t, root, done); ok {
		t.Fatalf("a short repeated command must not label an older request: %+v", l)
	}
}

// audit 5 (0.9.3): copying the JSON in the block as it is keeps the list's provisional grouping (a copy alone does not attach the message)
func TestAudit5CopiedGroupingJSONKeepsTheListDecision(t *testing.T) {
	repo, root, _ := planningFixture(t)
	continuesTurn(t, repo, root, "main取り込んで")
	out := runHook(t, "stop", map[string]any{"session_id": "planning", "cwd": repo, "last_assistant_message": "Merged main."})
	reason, block := stopReason(t, out)
	if !block {
		t.Fatalf("premise: the Stop blocks: %s", out)
	}
	if _, err := planningCall(t, "submit", askedJSON(t, reason)); err != nil {
		t.Fatal(err)
	}
	if s := loadSession(root, "planning"); !strings.HasPrefix(s.Request, "main取り込んで") {
		t.Fatalf("the copied JSON attached a short request to the previous one: %q", s.Request)
	}
}
