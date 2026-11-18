package main

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/notebook"
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

// The session's record of deliveries keeps every delivery of the running request: after sixteen other lessons arrived at tool
// calls of one request, the first lesson still does not arrive a second time. (The record used to keep the latest sixteen only.)
func TestToolLessonIsNotRepeatedAfterManyOtherLessonsInTheSameRequest(t *testing.T) {
	st, scope, _, ids := toolLessonFixture(t)
	book := notebook.Book{Root: st.Dir, Scope: scope}
	current, err := book.Query("lesson:" + strings.Split(ids["commit"], ":")[1])
	if err != nil || len(current.Lessons) != 1 {
		t.Fatalf("the commit lesson: %v %v", current, err)
	}
	sources := []string{}
	for _, s := range current.Lessons[0].Sources {
		sources = append(sources, s.ID)
	}
	const others = 16
	for i := 0; i < others; i++ {
		name := fmt.Sprintf("alpha%02d.md", i)
		if _, err := book.Ingest(notebook.Draft{Title: "Keep " + name + " short", Lesson: "Keep " + name + " under one screen and link the details.", AppliesTo: "when editing " + name, Kind: "rule", Scene: "when editing " + name, Sources: sources}); err != nil {
			t.Fatal(err)
		}
	}
	if ctx := feedbackContext(t, toolHook(t, scope, "long", toolPrompt, "Bash", map[string]any{"command": "git commit -m x"})); !strings.Contains(ctx, ids["commit"]) {
		t.Fatalf("the lesson arrives at the first commit:\n%s", ctx)
	}
	for i := 0; i < others; i++ {
		name := fmt.Sprintf("alpha%02d.md", i)
		if out := toolHook(t, scope, "long", "", "Edit", map[string]any{"file_path": scope + "/" + name, "old_string": "a", "new_string": "b"}); !strings.Contains(out, name) {
			t.Fatalf("the lesson of %s arrives at its edit: %q", name, cut(out, 200))
		}
	}
	if out := toolHook(t, scope, "long", "", "Bash", map[string]any{"command": "git status && git commit -m y"}); out != "" {
		t.Errorf("the same request got the same lesson a second time at a tool call:\n%s", cut(out, 400))
	}
}

// Deliveries from before the running request are trimmed to the latest sixteen, as before.
func TestSessionKeepsTheLatestDeliveriesOfEarlierRequests(t *testing.T) {
	trimmed := keepMemories(append(make([]memoryDelivery, 40), memoryDelivery{Key: "lesson:x:y", At: time.Now()}), time.Now().Add(time.Hour))
	if len(trimmed) != 16 || trimmed[15].Key != "lesson:x:y" {
		t.Errorf("deliveries from before the running request are trimmed to the latest 16: %d", len(trimmed))
	}
	if kept := keepMemories(make([]memoryDelivery, 40), time.Time{}); len(kept) != 16 {
		t.Errorf("a session without a request time keeps the latest 16: %d", len(kept))
	}
}

// A subagent of Claude Code or OpenCode does not see what the parent was given, so a lesson delivered to the parent (at its
// request or at its own tool call) still reaches a subagent right before the subagent runs that command. The subagent does not get
// it twice, and the parent does not get it again because a subagent got it.
func TestToolLessonReachesASubagentEvenWhenTheParentGotIt(t *testing.T) {
	_, scope, _, ids := toolLessonFixture(t)
	commit := map[string]any{"command": "git commit -m x"}
	if ctx := feedbackContext(t, toolHook(t, scope, "s", toolPrompt, "Bash", commit)); !strings.Contains(ctx, ids["commit"]) {
		t.Fatalf("precondition: the parent gets the lesson at its commit:\n%s", ctx)
	}
	child := func(id, command string) string {
		out := runHook(t, "tool-lesson", map[string]any{"session_id": "s", "cwd": scope, "agent_id": id, "agent_type": "general-purpose", "hook_event_name": "PreToolUse", "tool_name": "Bash", "tool_input": map[string]any{"command": command}})
		assertOnlyContext(t, out)
		return out
	}
	if out := child("agent-1", "git commit -m y"); !strings.Contains(out, ids["commit"]) {
		t.Fatalf("the subagent gets the lesson right before its own commit, though the parent already got it:\n%s", cut(out, 400))
	}
	if out := child("agent-1", "git status && git commit -m z"); out != "" {
		t.Errorf("the same subagent does not get it a second time:\n%s", cut(out, 400))
	}
	if out := child("agent-2", "git commit -m w"); !strings.Contains(out, ids["commit"]) {
		t.Errorf("another subagent gets it too:\n%s", cut(out, 400))
	}
	if out := toolHook(t, scope, "s", "", "Bash", map[string]any{"command": "git add . && git commit -m v"}); out != "" {
		t.Errorf("the parent does not get it again:\n%s", cut(out, 400))
	}
}
