package main

// RED tests from the 0.9.3 behaviour check with real agents (ba89f21). Each fails on ba89f21.

import (
	"path/filepath"
	"strings"
	"testing"
)

// 1. The agent's continues_previous changes the previous request's outcome truth: attaching a short request to the one before
// makes the previous request text long, so the person's restatement of the short request is no longer similar enough and the
// "no" is lost (Codex attached every short request and the restatement; Claude attached "write it in the README too").
func TestGroupingDoesNotEraseRestatement(t *testing.T) {
	for _, continues := range []bool{false, true} {
		repo, root, _ := planningFixture(t)
		continuesTurn(t, repo, root, "README\u306b\u3082\u66f8\u3044\u3066")
		continuesSubmit(t, root, continues)
		runHook(t, "stop", map[string]any{"session_id": "planning", "cwd": repo, "last_assistant_message": "Wrote the README."})
		done := ""
		for _, r := range loadSession(root, "planning").Reviews {
			if r.Spec == "done-as-asked" {
				done = r.Trace
			}
		}
		if done == "" {
			t.Fatalf("continues=%v: the README turn must have a done-as-asked decision", continues)
		}
		runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": "README\u306b\u3082\u66f8\u3044\u3066\u304f\u3060\u3055\u3044"})
		continuesSubmit(t, root, false)
		if l, ok := doneLabel(t, root, done); !ok || l.Truth != "no" || l.Source != "outcome" {
			t.Errorf("continues_previous=%v: the person restated the README request, so its outcome must be no whatever the grouping: %+v %v", continues, l, ok)
		}
	}
}

// 2. The organizing notice tells the agent to attach "a short follow-up on the same work". Codex then attached
// "also check the totals", "write it in the README too" and the restatement of request 1, and the whole session became one request.
// A short request that asks for new work must be its own request (e3ac97e).
func TestNoticeKeepsShortRequestsWithContentApart(t *testing.T) {
	n := planningNotice("s", "t")
	if strings.Contains(n, "short follow-up on the same work") {
		t.Fatalf("the notice invites attaching short requests with content: %s", n)
	}
}

// 3. wiki ingest (CLI) hides the JSON error: Claude sent applies_to as an array and a lesson object and got the same
// generic line 3 times. capture and maintain already pass the decoder's reason on.
func TestIngestErrorSaysWhatIsWrong(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("HOME", filepath.Join(dir, "home"))
	capture(t, func() error { return cmdInit(nil) })
	for input, want := range map[string]string{
		`{"sources":["x"],"title":"t","lesson":"l","applies_to":["a"]}`:            "cannot unmarshal array",  // applies_to must be a string
		`{"sources":["x"],"title":"t","lesson":{"kind":"howto"},"applies_to":"a"}`: "cannot unmarshal object", // lesson must be a string
		`{"title":"t","lesson":"l","applies_to":"a"}`:                              "sources",                 // MCP said "needs source ids"; Claude tried source_ids
	} {
		var err error
		withStdin(t, input, func() { err = cmdNotebook([]string{"ingest"}) })
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("the ingest error must say %q: %v (input %s)", want, err, input)
		}
	}
}
