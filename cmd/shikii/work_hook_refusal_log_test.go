package main

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/hayaenfj/shikii/internal/notebook"
)

// lastHookLogLine is the last line the hook wrote to its log (the test's own HOME, so the log is this test's).
func lastHookLogLine(t *testing.T) string {
	t.Helper()
	home, _ := agentHome()
	logged, err := os.ReadFile(filepath.Join(home, "hook.log"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(logged)), "\n")
	return lines[len(lines)-1]
}

// A refused preparation says in the hook's log how much the hook had written and how large each field of the reply was when it was
// refused, by name and number alone: never the request, an article or any other text.
func TestARefusedHookPreparationLogsTheSizesAndNoText(t *testing.T) {
	repo, root, _ := planningFixture(t)
	t.Chdir(repo)
	const request = "Check the CSV export filters and totals for the secret-tango project."
	interimAddArticle(t, notebook.Book{Root: root, Scope: feedbackScope(repo)}, "Secret quartz handling", "The quartz token is stored in the vault and never printed in a log.")
	runUnreviewedHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": request})
	turn := loadSession(root, "planning").TurnID
	if err := updateSession(root, "planning", func(s *hookSession) { s.Planning.Work = nil }); err != nil {
		t.Fatal(err)
	}
	in := hookInput{SessionID: "planning", CWD: repo, Prompt: request, workTurn: turn}
	before := planningNotice("planning", turn) + strings.Repeat("Retain this existing context. ", 1500)
	out := hookOut{Context: before}
	prepareHookWork(root, &in, &out)
	if out.Context != before {
		t.Fatal("precondition: the preparation was refused")
	}
	line := lastHookLogLine(t)
	if !strings.Contains(line, "work prepare: the task and reference inventory exceed max_bytes (10000)") {
		t.Fatalf("the refusal's own message stays in the line: %s", line)
	}
	// what the hook had written before the prepared work: its text, with the notice the preparation puts in the planning notice's place
	written := strings.Replace(before, planningNotice("planning", turn), hookPreparedNotice("planning", turn, in.ending), 1)
	for _, want := range []string{"the hook's text before the prepared work: " + strconv.Itoa(hookContextLength(written)) + " of 10000 characters", "reply fields left, in characters:", " how_to_record=", " task_context=", " receipt=", " task=", " knowledge="} {
		if !strings.Contains(line, want) {
			t.Fatalf("the line names the hook's text and the fields of the reply with their sizes (missing %q): %s", want, line)
		}
	}
	for _, not := range []string{"secret-tango", "Secret quartz", "quartz token", "vault", "Preparation registered", "Conversation references", "Relevant wiki", "Retain this existing"} {
		if strings.Contains(line, not) {
			t.Fatalf("the line carries no text of the request, an article or the reply (%q): %s", not, line)
		}
	}
	if strings.Contains(line, "\n") || len(line) > hookLogLineMax+hookRefusalLogExtra+40 {
		t.Fatalf("one line within its bound: %d bytes", len(line))
	}
	// a refusal that built no reply names no fields
	if got := refusalSizes("abc", errors.New("another reason")); got != " | the hook's text before the prepared work: 3 of 10000 characters | no reply was built" {
		t.Fatalf("a refusal without a reply: %q", got)
	}
}

// Only the refused preparation's line is wider than an ordinary one: every other message of the hook keeps the bound it always had (the
// model's response bodies, which may carry requests or replies, are cut there and fold into one line).
func TestAnOrdinaryHookLogLineKeepsItsBound(t *testing.T) {
	repo, _, _ := planningFixture(t)
	t.Chdir(repo)
	hookLog("a model said:\n" + strings.Repeat("secret words ", 400))
	line := lastHookLogLine(t)
	if strings.Contains(line, "\n") || len(line) > hookLogLineMax+len("2026-01-02T03:04:05+09:00 ") {
		t.Fatalf("an ordinary line of the hook's log is cut at %d bytes and is one line: %d bytes", hookLogLineMax, len(line))
	}
	if !strings.Contains(line, "a model said: secret words") {
		t.Fatalf("the cut keeps the head of the message: %.80s", line)
	}
}

// Every field a reply can carry fits in the refused preparation's line, beside the refusal's message: the line is wider than an ordinary
// one because the sizes would be cut off at the ordinary bound, and a field the registry names later is held to the same room.
func TestARefusedPreparationLineHoldsEveryFieldOfAReply(t *testing.T) {
	repo, _, _ := planningFixture(t)
	t.Chdir(repo)
	reply := map[string]any{}
	for name := range workReplyFields {
		reply[name] = strings.Repeat("x", 1234)
	}
	err := &hookRefusal{err: errors.New("the task and reference inventory exceed max_bytes (10000); prepare again with a larger max_bytes (up to 393216)"), reply: reply}
	logRefusedPreparation(strings.Repeat("y", 20000), err)
	line := lastHookLogLine(t)
	if strings.Contains(line, "\n") || len(line) > hookLogLineMax+hookRefusalLogExtra+len("2026-01-02T03:04:05+09:00 ") {
		t.Fatalf("one line within its bound: %d bytes", len(line))
	}
	for name := range workReplyFields {
		if !strings.Contains(line, " "+name+"=1236") {
			t.Fatalf("the line names %s with its size (the line is %d bytes, within %d): %s", name, len(line), hookLogLineMax+hookRefusalLogExtra, line)
		}
	}
	t.Logf("the longest line a reply with every registered field makes: %d bytes of %d", len(line), hookLogLineMax+hookRefusalLogExtra)
}
