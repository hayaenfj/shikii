package main

import (
	"strings"
	"testing"
)

// 0.9.7: the pre-start organizing notice (about 1,700 characters) was added in full to every request. The full text now comes on the
// session's first request, after a request whose organizing was not submitted, and every noticeFullEvery requests (so it survives context
// compaction); in between a short form names the same commands and this turn's IDs. A miss is also caught by the Stop check, which repeats the steps.
func TestPlanningNoticeIsShortAfterTheFirstRequest(t *testing.T) {
	repo, root, _ := planningFixture(t)
	const full = "putting topic words in query"
	turn := func(prompt string) string {
		t.Helper()
		return feedbackContext(t, runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": prompt}))
	}
	submit := func() {
		t.Helper()
		if _, err := planningCall(t, "submit", planningPayload(root)); err != nil {
			t.Fatal(err)
		}
	}
	first := turn(planningTestRequest)
	if !strings.Contains(first, full) {
		t.Fatalf("the session's first request gets the full text: %q", first)
	}
	submit()
	second := turn("Now add a tax rate to the agreed CSV columns and keep the existing filters.")
	id := loadSession(root, "planning").TurnID
	if strings.Contains(second, full) || !strings.Contains(second, "hook plan query") || !strings.Contains(second, "hook plan submit") || !strings.Contains(second, id) {
		t.Fatalf("after an organized request the short form names the commands and this turn's id %s: %q", id, second)
	}
	if len(second) >= len(first)/3 {
		t.Fatalf("the short form is short: %d of %d characters", len(second), len(first))
	}
	// the second request's organizing is not submitted: the next request gets the full text again
	if third := turn("Then sort the agreed CSV columns by date and keep the existing filters."); !strings.Contains(third, full) {
		t.Fatalf("after a request whose organizing was missed, the full text comes again: %q", third)
	}
	submit()
	fulls := 0
	for i := 0; i < noticeFullEvery; i++ {
		if strings.Contains(turn("Request "+strings.Repeat("x", i+1)+": use the agreed CSV columns and keep the existing filters."), full) {
			fulls++
		}
		submit()
	}
	if fulls != 1 {
		t.Fatalf("the full text comes back once every %d requests: %d times", noticeFullEvery, fulls)
	}
}

// The reflection request (about 1,700 characters) follows the same rule: the full text the first time in a session, when the conversations
// asked last time are still waiting (missed), and every noticeFullEvery requests; a short form in between.
func TestReflectionNoticeIsShortAfterTheFirstAsk(t *testing.T) {
	st, scope, _, _, _ := memoryEffectFixture(t)
	const full = "only sources when there is no lesson"
	prompt := func(text string) string {
		t.Helper()
		return feedbackContext(t, runHook(t, "user-prompt-submit", map[string]any{"session_id": "s1", "cwd": scope, "prompt": text}))
	}
	memoryEffectPrompt(t, scope, "s1")
	var first string
	for i := 0; i < 4 && !strings.Contains(first, "wrap up the reflection"); i++ {
		runHook(t, "stop", map[string]any{"session_id": "s1", "cwd": scope, "last_assistant_message": "README \u3092\u76f4\u3057\u307e\u3057\u305f\u3002"})
		first = prompt("README \u306e\u5225\u306e\u7bc0\u3082\u76f4\u3057\u3066\u304f\u3060\u3055\u3044 " + strings.Repeat("x", i+1))
	}
	if !strings.Contains(first, "wrap up the reflection") || !strings.Contains(first, full) {
		t.Fatalf("precondition and the first ask: the full text: %q", first)
	}
	runHook(t, "stop", map[string]any{"session_id": "s1", "cwd": scope, "last_assistant_message": "README \u3092\u76f4\u3057\u307e\u3057\u305f\u3002"})
	if again := prompt("README \u306e\u898b\u51fa\u3057\u3082\u76f4\u3057\u3066\u304f\u3060\u3055\u3044"); !strings.Contains(again, full) {
		t.Fatalf("the conversations asked last time are still waiting (missed): the full text again: %q", again)
	}
	// the last ask was handled (other conversations were asked) and the full text came one request ago: the short form
	if err := updateSession(st.Dir, "s1", func(s *hookSession) { s.ReflectAsked, s.ReflectShortTurns = []string{"ffffffffffffffff"}, 1 }); err != nil {
		t.Fatal(err)
	}
	runHook(t, "stop", map[string]any{"session_id": "s1", "cwd": scope, "last_assistant_message": "README \u3092\u76f4\u3057\u307e\u3057\u305f\u3002"})
	short := prompt("README \u306e\u8868\u3082\u76f4\u3057\u3066\u304f\u3060\u3055\u3044")
	if !strings.Contains(short, "wrap up the reflection") || strings.Contains(short, full) || !strings.Contains(short, `{"sources":[`) || !strings.Contains(short, "reflect query") {
		t.Fatalf("the short form names the command and the conversations: %q", short)
	}
}
