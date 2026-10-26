package main

import (
	"encoding/json"
	"strings"
	"testing"
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
			// the JSON in the block is accepted as it is, and its continues_previous still moves the grouping
			if _, err := planningCall(t, "submit", askedJSON(t, reason)); err != nil {
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
