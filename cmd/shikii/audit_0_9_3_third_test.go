package main

import (
	"strings"
	"testing"
)

// 0.9.3 audit 3. Truths about done-as-asked come only from fixed rules on the person's message, never from the agent's
// continues_previous (the agent being judged must not be able to erase a restatement or manufacture a "done" truth).

// a short correction or answer ("wrong", "no", "2") is not a sign the previous request was done
func TestAudit3ShortReplyIsNotDoneYes(t *testing.T) {
	for _, reply := range []string{"違う", "no", "2", "やり直して"} {
		t.Run(reply, func(t *testing.T) {
			repo, root, _ := planningFixture(t)
			done := continuesTurn(t, repo, root, reply)
			runHook(t, "stop", map[string]any{"session_id": "planning", "cwd": repo, "last_assistant_message": "Redid it."})
			if l, ok := doneLabel(t, root, done); ok && l.Truth == "yes" {
				t.Fatalf("a short reply %q must not label the previous request as done: %+v", reply, l)
			}
		})
	}
}

// a restatement of the same request labels the previous request as not done, whatever the agent says about grouping
func TestAudit3AgentCannotEraseRestatement(t *testing.T) {
	repo, root, _ := planningFixture(t)
	done := continuesTurn(t, repo, root, "Please use the agreed CSV columns and keep the existing filters.")
	continuesSubmit(t, root, true)
	runHook(t, "stop", map[string]any{"session_id": "planning", "cwd": repo, "last_assistant_message": "Done again."})
	if l, ok := doneLabel(t, root, done); !ok || l.Truth != "no" {
		t.Fatalf("a restatement must label the previous request as not done even when the agent says it continues: %+v %v", l, ok)
	}
}

// an acknowledgement never settles the previous outcome, even when the agent copies the skill's example (continues_previous:false)
func TestAudit3AgentCannotMakeDoneFromAck(t *testing.T) {
	repo, root, _ := planningFixture(t)
	done := continuesTurn(t, repo, root, "ok")
	continuesSubmit(t, root, false)
	if l, ok := doneLabel(t, root, done); ok {
		t.Fatalf("an acknowledgement must not become a done truth through the agent's grouping: %+v", l)
	}
}

// an organizing submitted while the Stop of the same turn is waiting for the model must not make that Stop drop the turn
func TestAudit3SubmitDuringStopKeepsTheJudgment(t *testing.T) {
	repo, root, _ := planningFixture(t)
	// a message of 40 characters or more: the Stop judges at once (a shorter one outside the list is first asked for continues_previous)
	continuesTurn(t, repo, root, "main を取り込んで、衝突があれば直し、テストが全部通ることを確かめてから知らせてください。")
	in := &hookInput{SessionID: "planning", CWD: repo, LastAssistantMessage: "Merged main."}
	snapshot := loadSession(root, "planning")
	_, muts, err := hookStop(root, in, snapshot) // the Stop has read its snapshot and asked the model (outside the lock)
	if err != nil {
		t.Fatal(err)
	}
	continuesSubmit(t, root, true) // the organizing lands before the Stop takes the lock
	if err := updateSession(root, "planning", func(s *hookSession) {
		for _, m := range muts {
			m(s)
		}
	}); err != nil {
		t.Fatal(err)
	}
	s := loadSession(root, "planning")
	if !s.Judged || s.Completed == nil || s.Completed.Request != s.Request {
		t.Fatalf("the Stop that judged this turn must keep its judgment and link the reply: judged=%v completed=%+v request=%q", s.Judged, s.Completed, s.Request)
	}
	b := mustRead(t, sessionFilesPath(root, "planning"))
	if !strings.HasPrefix(string(b), listingHeader(s.Request)) {
		t.Fatalf("the listing header must follow the kept request")
	}
}

// what people type as a bare acknowledgement in another width or with a variation selector is still one
func TestAudit3AckNormalization(t *testing.T) {
	for _, s := range []string{"ＯＫ", "ｏｋ！", "👍️", "✅", "了解しました", "わかった", "OK️"} {
		if !isAckReply(s) {
			t.Errorf("%q is a bare acknowledgement", s)
		}
	}
	for _, s := range []string{"ＯＫ、でもテストも", "２", "違う"} {
		if isAckReply(s) {
			t.Errorf("%q is not a bare acknowledgement", s)
		}
	}
}

// a turn whose organizing and Stop never came still settles the previous outcome at the next request (test hole: mutation M4)
func TestAudit3NextRequestSettlesDeferredOutcome(t *testing.T) {
	repo, root, _ := planningFixture(t)
	done := continuesTurn(t, repo, root, "次の版のリリースノートを書いて、0.9.2 からの変更をすべて並べ、英語と日本語の両方で用意してください。")
	if l, ok := doneLabel(t, root, done); ok {
		t.Fatalf("the outcome waits for the organizing or the Stop: %+v", l)
	}
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": "ok"})
	if l, ok := doneLabel(t, root, done); !ok || l.Truth != "yes" {
		t.Fatalf("the next request settles a deferred outcome: %+v %v", l, ok)
	}
}

// query reads; it does not take organized content such as continues_previous (test hole: mutation M8)
func TestAudit3QueryRefusesContinuesPrevious(t *testing.T) {
	repo, root, _ := planningFixture(t)
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": continuesFirst})
	if _, err := planningCall(t, "query", map[string]any{"session_id": "planning", "turn_id": loadSession(root, "planning").TurnID, "continues_previous": true}); err == nil {
		t.Fatal("query must refuse continues_previous")
	}
}
