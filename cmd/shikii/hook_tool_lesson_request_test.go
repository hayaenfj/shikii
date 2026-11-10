package main

import (
	"strings"
	"testing"
	"time"
)

// A lesson delivered at a tool call is not delivered again while the same request runs, however long that takes. The next request
// gets it again once the hour has passed.
func TestToolLessonIsNotRepeatedWhileTheSameRequestRuns(t *testing.T) {
	st, scope, _, ids := toolLessonFixture(t)
	commit := map[string]any{"command": "git commit -m x"}
	if ctx := feedbackContext(t, toolHook(t, scope, "long", toolPrompt, "Bash", commit)); !strings.Contains(ctx, ids["commit"]) {
		t.Fatalf("precondition: the lesson arrives at the first commit:\n%s", ctx)
	}
	// two hours later the same request is still running
	if err := updateSession(st.Dir, "long", func(s *hookSession) {
		s.HeadAt = s.HeadAt.Add(-3 * time.Hour)
		for i := range s.Memories {
			s.Memories[i].At = s.Memories[i].At.Add(-2 * time.Hour)
		}
	}); err != nil {
		t.Fatal(err)
	}
	// another command line that names git commit (a delivery is recorded per command line, so this one is a new delivery)
	again := map[string]any{"command": "git status && git commit -m y"}
	if out := toolHook(t, scope, "long", "", "Bash", again); out != "" {
		t.Fatalf("the same request does not get the lesson again after an hour:\n%s", out)
	}
	if ctx := feedbackContext(t, toolHook(t, scope, "long", "now tidy the commit history", "Bash", again)); !strings.Contains(ctx, ids["commit"]) {
		t.Fatalf("the next request gets it again once the hour has passed:\n%s", ctx)
	}
}
