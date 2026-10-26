package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hayaenfj/shikii/internal/store"
)

// 0.9.3 real-agent check, medium 3: Claude Code does not organize turns that change no files, so a short acknowledgement outside the
// fixed list (a polite Japanese "OK", "let's go with that") stayed a conversation of its own. The Stop now asks once for continues_previous only.

func stopReason(t *testing.T, out string) (string, bool) {
	t.Helper()
	if out == "" {
		return "", false
	}
	var parsed map[string]any
	if json.Unmarshal([]byte(out), &parsed) != nil {
		t.Fatalf("the Stop output is not JSON: %s", out)
	}
	reason, _ := parsed["reason"].(string)
	return reason, parsed["decision"] == "block"
}

// the JSON the block asks for, as written in its reason
func askedJSON(t *testing.T, reason string) map[string]any {
	t.Helper()
	start := strings.Index(reason, `{"agreed"`)
	end := strings.Index(reason[max(start, 0):], `"}`)
	if start < 0 || end < 0 {
		t.Fatalf("the block must carry the JSON to pass: %s", reason)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(reason[start:start+end+2]), &payload); err != nil {
		t.Fatalf("the JSON in the block must be valid: %v (%s)", err, reason)
	}
	return payload
}

func TestStopAsksContinuesPreviousForShortUnlistedMessage(t *testing.T) {
	for _, reply := range []string{"OKです", "それでいこう"} {
		t.Run(reply, func(t *testing.T) {
			repo, root, _ := planningFixture(t)
			continuesTurn(t, repo, root, reply)
			out := runHook(t, "stop", map[string]any{"session_id": "planning", "cwd": repo, "last_assistant_message": "Went ahead."})
			reason, block := stopReason(t, out)
			if !block || !strings.Contains(reason, "continues_previous") || !strings.Contains(reason, "hook plan submit") || strings.Contains(reason, "hook plan query") {
				t.Fatalf("a short message outside the list without an organizing: the Stop asks once, only for continues_previous: %s", out)
			}
			if s := loadSession(root, "planning"); s.Judged || s.Request != reply {
				t.Fatalf("the turn is not judged before the grouping is known: judged=%v request=%q", s.Judged, s.Request)
			}
			// the JSON in the block is accepted with the agent's answer in it, and its continues_previous still moves the grouping
			payload := askedJSON(t, reason)
			payload["continues_previous"] = true
			if _, err := planningCall(t, "submit", payload); err != nil {
				t.Fatalf("the asked submit must be accepted: %v", err)
			}
			if s := loadSession(root, "planning"); s.Request != continuesFirst+" / "+reply {
				t.Fatalf("continues_previous after the block must attach the message: %q", s.Request)
			}
			// the Stop after the block is not blocked and judges the attached request
			out = runHook(t, "stop", map[string]any{"session_id": "planning", "cwd": repo, "last_assistant_message": "Went ahead.", "stop_hook_active": true})
			if _, block := stopReason(t, out); block {
				t.Fatalf("a second Stop is not blocked: %s", out)
			}
			if s := loadSession(root, "planning"); !s.Judged || s.Completed == nil || s.Completed.Request != continuesFirst+" / "+reply {
				t.Fatalf("the Stop after the block judges the regrouped request: %+v", s)
			}
		})
	}
	t.Run("once per turn", func(t *testing.T) {
		repo, root, _ := planningFixture(t)
		continuesTurn(t, repo, root, "OKです")
		runHook(t, "stop", map[string]any{"session_id": "planning", "cwd": repo, "last_assistant_message": "Went ahead."})
		// the Stop after a background task's notice carries no stop_hook_active: the same turn is still not blocked again
		runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": "<task-notification>done</task-notification>"})
		out := runHook(t, "stop", map[string]any{"session_id": "planning", "cwd": repo, "last_assistant_message": "Went ahead."})
		if _, block := stopReason(t, out); block {
			t.Fatalf("the same turn is asked only once: %s", out)
		}
	})
	t.Run("no submit", func(t *testing.T) {
		repo, root, _ := planningFixture(t)
		continuesTurn(t, repo, root, "OKです")
		runHook(t, "stop", map[string]any{"session_id": "planning", "cwd": repo, "last_assistant_message": "Went ahead."})
		out := runHook(t, "stop", map[string]any{"session_id": "planning", "cwd": repo, "last_assistant_message": "Went ahead.", "stop_hook_active": true})
		if _, block := stopReason(t, out); block {
			t.Fatalf("a second Stop is not blocked: %s", out)
		}
		if s := loadSession(root, "planning"); !s.Judged || s.Request != "OKです" {
			t.Fatalf("without the organizing, the Stop after the block judges with the list's decision: judged=%v %q", s.Judged, s.Request)
		}
	})
}

func TestStopDoesNotAskContinuesPreviousOtherwise(t *testing.T) {
	cases := map[string]struct {
		reply  string
		submit bool
		bare   bool // organized, but continues_previous left out (unsure): not asked again
	}{
		"organizing without continues_previous": {reply: "OKです", bare: true},
		"listed acknowledgement":                {reply: "ok"},
		"long message":                          {reply: "次の版のリリースノートを書いて、0.9.2 からの変更をすべて並べ、英語と日本語の両方で用意してください。"},
		"organizing came":                       {reply: "OKです", submit: true},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			repo, root, _ := planningFixture(t)
			continuesTurn(t, repo, root, c.reply)
			if c.submit {
				continuesSubmit(t, root, true)
			}
			if c.bare {
				if _, err := planningCall(t, "submit", map[string]any{"session_id": "planning", "turn_id": loadSession(root, "planning").TurnID, "agreed": map[string]any{"unknown": true}, "open": map[string]any{"unknown": true}}); err != nil {
					t.Fatal(err)
				}
			}
			out := runHook(t, "stop", map[string]any{"session_id": "planning", "cwd": repo, "last_assistant_message": "Done."})
			if _, block := stopReason(t, out); block {
				t.Fatalf("the Stop must not ask for continues_previous here: %s", out)
			}
			if s := loadSession(root, "planning"); !s.Judged {
				t.Fatalf("the Stop judges at once: %+v", s)
			}
		})
	}
}

// 0.9.3 real-agent check, low 4: the agent names the earlier request a message restates (restates in hook plan submit), and shikii compares
// the message with that request's own text by the fixed rule.

const (
	restateFirst  = "invoices/ の請求書ごとに合計を検算するスクリプトを check-totals.sh に書いて、ずれていたら直し、全部通ることを見せて"
	restateSecond = "合計の検算も足して" // short (under 40 characters): request 1 gets no outcome from them, as in the real-agent check
	restateThird  = "READMEにも書いて"
	restateAgain  = "最初に頼んだ、invoices/ の請求書ごとに合計を検算するスクリプトを check-totals.sh に書いて、ずれていたら直し、全部通ることを見せてください。"
	restateOther  = "来週の打ち合わせの議題を、今週の作業の振り返りと、次の版で入れたいことの二つに分けて、箇条書きで用意してください。"
)

// restateTurns runs three requests with their Stops in the session and returns the first one's turn id and done-as-asked trace.
func restateTurns(t *testing.T, repo, root, session string) (string, string) {
	t.Helper()
	turn, done := "", ""
	for i, prompt := range []string{restateFirst, restateSecond, restateThird} {
		runHook(t, "user-prompt-submit", map[string]any{"session_id": session, "cwd": repo, "prompt": prompt})
		if i > 0 { // each short request is its own (the agent organizes the turn)
			restateSubmit(t, root, session, "")
		}
		out := runHook(t, "stop", map[string]any{"session_id": session, "cwd": repo, "last_assistant_message": "Done."})
		if reason, block := stopReason(t, out); block && strings.Contains(reason, "continues_previous") {
			t.Fatalf("request %d must not be asked for continues_previous: %s", i+1, out)
		}
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
		t.Fatal("the first request must have a turn and a done-as-asked decision")
	}
	return turn, done
}

func restateSubmit(t *testing.T, root, session, restates string) {
	t.Helper()
	payload := map[string]any{"session_id": session, "turn_id": loadSession(root, session).TurnID, "agreed": map[string]any{"unknown": true}, "open": map[string]any{"unknown": true}, "continues_previous": false, "restates": restates}
	if _, err := planningCall(t, "submit", payload); err != nil {
		t.Fatalf("plan submit with restates: %v", err)
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

func TestRestatesIgnoresOtherSessionsAndUnknownTurns(t *testing.T) {
	repo, root, _ := planningFixture(t)
	turn, done := restateTurns(t, repo, root, "planning")
	restateTurns(t, repo, root, "other")
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "other", "cwd": repo, "prompt": restateAgain})
	restateSubmit(t, root, "other", turn) // a turn of another session
	if l, ok := doneLabel(t, root, done); ok {
		t.Fatalf("restates naming another session's turn is ignored: %+v", l)
	}
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": restateAgain})
	restateSubmit(t, root, "planning", "0123456789abcdef") // an unknown turn
	if l, ok := doneLabel(t, root, done); ok {
		t.Fatalf("restates naming an unknown turn is ignored: %+v", l)
	}
}

// a request that already has the person's truth is not labeled again by a restatement
func TestRestatesKeepsThePersonsTruth(t *testing.T) {
	repo, root, _ := planningFixture(t)
	turn, done := restateTurns(t, repo, root, "planning")
	if err := labelTrace(store.Store{Dir: root}, "done-as-asked", done, "yes", 1, "human"); err != nil {
		t.Fatal(err)
	}
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": restateAgain})
	restateSubmit(t, root, "planning", turn)
	n := 0
	for _, line := range strings.Split(strings.TrimSpace(string(mustRead(t, filepath.Join(root, "labels", "done-as-asked.jsonl")))), "\n") {
		if strings.Contains(line, done) {
			n++
		}
	}
	if l, _ := doneLabel(t, root, done); n != 1 || l.Source != "human" {
		t.Fatalf("the person's truth stays the only label of that request: %d labels, %+v", n, l)
	}
}

// 0.9.3 audit 5 (test hole M23): with an enabled guard that fails, a short unlisted message still makes only one block; the Stop after
// the block is not blocked by the guard either (a second Stop is never blocked), and it judges the turn.
func TestGuardAndGroupingAskStillBlockOnlyOnce(t *testing.T) {
	for name, reply := range map[string]string{
		"short": "\u305d\u308c\u3067\u3044\u3053\u3046",
		"long":  "please also add a section about the archive format to the README and link it from the install steps",
	} {
		t.Run(name, func(t *testing.T) { guardAndGroupingOnce(t, reply) })
	}
}

func guardAndGroupingOnce(t *testing.T, reply string) {
	repo, root, _ := planningFixture(t)
	writeGuard(t, root, guard{ID: "no-word", Kind: guardKindForbiddenWords, Title: "no forbidden word", Words: []string{"forbidden"}})
	continuesTurn(t, repo, root, reply)
	if err := os.WriteFile(filepath.Join(repo, "NOTES.md"), []byte("a forbidden word\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := runHook(t, "stop", map[string]any{"session_id": "planning", "cwd": repo, "last_assistant_message": "Went ahead."})
	if _, block := stopReason(t, out); !block {
		t.Fatalf("the first Stop blocks (guard and grouping ask): %s", out)
	}
	out = runHook(t, "stop", map[string]any{"session_id": "planning", "cwd": repo, "last_assistant_message": "Went ahead.", "stop_hook_active": true})
	if _, block := stopReason(t, out); block {
		t.Fatalf("the Stop after the block is not blocked, even with the guard still failing: %s", out)
	}
	if s := loadSession(root, "planning"); !s.Judged {
		t.Fatalf("the Stop after the block judges the turn: %+v", s)
	}
}
