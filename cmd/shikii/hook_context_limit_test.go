package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/hayaenfj/shikii/internal/notebook"
)

// Measured on 2026-11-16 in this project's Claude Code sessions: Claude Code shows a hook's additionalContext whole up to 10,000
// characters and cuts the rest when it cannot save it to a file ("Hook additionalContext truncated at 10000 chars"; Claude Code
// 2.1.325 in a sandbox saved a 16,605-character one to a file and showed about 2 KB of its start), while the request hook delivered prepared work
// of up to 28 KiB, encoded with json.Marshal, which writes keys alphabetically: the receipt came after the lessons and the
// conversation, so 62 requests of one session reached the agent without their receipt, their maintenance tasks and most lessons.
// The whole context now fits that limit, and the prepared work is written in the reading order of every other reply.
func TestHookPreparedWorkFitsTheAgentsContextLimit(t *testing.T) {
	if got := hookContextLength("a\u3042\U0001F600"); got != 4 {
		t.Fatalf("the count must be what Claude Code counts (UTF-16 code units): got %d for a, a kana and an emoji", got)
	}
	repo, root, _ := planningFixture(t)
	b := notebook.Book{Root: root, Scope: feedbackScope(repo)}
	for i := 0; i < 6; i++ {
		interimAddArticle(t, b, fmt.Sprintf("Archive approval %d", i), strings.Repeat("Production restore requires owner approval before the archive is restored. ", 40))
	}
	prompt := "Explain archive approval for production restore. " + strings.Repeat("Keep the restore conditions and the owner approval. ", 40)
	out := runUnreviewedHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": prompt})
	context := feedbackContext(t, out)
	const prefix = "shikii prepared work:\n"
	i := strings.Index(context, prefix)
	if i < 0 {
		t.Fatalf("the hook delivered no prepared work: %.400s", context)
	}
	if n := hookContextLength(context); n > hookContextMax {
		t.Fatalf("the hook's context is %d characters; Claude Code cuts it at %d", n, hookContextMax)
	}
	prepared := context[i+len(prefix):]
	if !strings.HasPrefix(prepared, `{"receipt":`) {
		t.Fatalf("the receipt must come first, before anything a cut could take: %.200s", prepared)
	}
	if h, k := strings.Index(prepared, `"how_to_record":`), strings.Index(prepared, `"knowledge":`); h < 0 || k >= 0 && k < h {
		t.Fatalf("how_to_record must come before the lessons: how_to_record at %d, knowledge at %d", h, k)
	}
	var v map[string]any
	if err := json.Unmarshal([]byte(prepared), &v); err != nil || v["receipt"] == nil {
		t.Fatalf("the prepared work must be whole JSON with its receipt: %v", err)
	}
}

// preparedHookWork runs the request hook on prompt and returns its whole context and the prepared work it delivered (nil when it
// delivered none), after checking what holds for every preparation: it fits the bound and starts with its receipt.
func preparedHookWork(t *testing.T, repo, prompt string) (string, map[string]any) {
	t.Helper()
	context := feedbackContext(t, runUnreviewedHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": prompt}))
	if n := hookContextLength(context); n > hookContextMax {
		t.Fatalf("the hook's context is %d characters, over %d", n, hookContextMax)
	}
	const prefix = "shikii prepared work:\n"
	i := strings.Index(context, prefix)
	if i < 0 {
		return context, nil
	}
	prepared := context[i+len(prefix):]
	var v map[string]any
	if !strings.HasPrefix(prepared, `{"receipt":`) || json.Unmarshal([]byte(prepared), &v) != nil {
		t.Fatalf("the prepared work must be whole JSON that starts with its receipt: %.200s", prepared)
	}
	return context, v
}

// The bound is counted as Claude Code counts, in characters (TestHookPreparedWorkFitsTheAgentsContextLimit checks the count of
// one string). Counted in bytes, a context of Japanese lessons that has to be shortened stops near 7,000 characters, and lessons
// the agent could have read are left out (the check of 2026-11-16 found 7,033 characters, 8,763 bytes).
func TestHookCountsItsBoundInCharactersNotBytes(t *testing.T) {
	repo, root, _ := planningFixture(t)
	b := notebook.Book{Root: root, Scope: feedbackScope(repo)}
	for i := 0; i < 6; i++ {
		interimAddArticle(t, b, fmt.Sprintf("\u30a2\u30fc\u30ab\u30a4\u30d6\u306e\u627f\u8a8d %d", i), strings.Repeat("\u672c\u756a\u306e\u5fa9\u5143\u306b\u306f\u6240\u6709\u8005\u306e\u627f\u8a8d\u304c\u8981\u308b\u3002\u30a2\u30fc\u30ab\u30a4\u30d6\u3092\u623b\u3059\u524d\u306b\u627f\u8a8d\u3092\u5f97\u308b\u3053\u3068\u3002", 60))
	}
	context, v := preparedHookWork(t, repo, "\u30a2\u30fc\u30ab\u30a4\u30d6\u306e\u627f\u8a8d\u3068\u672c\u756a\u306e\u5fa9\u5143\u306e\u6761\u4ef6\u3092\u8aac\u660e\u3057\u3066\u3002"+strings.Repeat("\u5fa9\u5143\u306e\u6761\u4ef6\u3068\u6240\u6709\u8005\u306e\u627f\u8a8d\u3092\u4fdd\u3064\u3053\u3068\u3002", 20))
	if v == nil {
		t.Fatalf("the hook delivered no prepared work: %.300s", context)
	}
	if output, _ := v["output"].(map[string]any); output == nil || output["limited"] != true {
		t.Fatal("precondition: the lessons must not all fit, so that the bound decides where the prepared work stops")
	}
	if len(context) <= hookContextMax {
		t.Fatalf("the prepared work was shortened at %d characters (%d bytes): the bound was counted in bytes, not in the characters the agent is shown", hookContextLength(context), len(context))
	}
}

// A request the hook keeps (up to 64 KiB) can be longer than what the agent is shown with the rest of the prepared work. The
// preparation was then refused whole, and the agent got only the notice to prepare it again (the check of 2026-11-16: about 7,000
// characters of English, or 3,000 of pasted HTML, whose <, > and & took six characters each in the JSON). Now the hook writes the
// JSON without escaping those characters, and when the request still does not fit, it delivers the head of the request in task,
// with task_context saying so: the whole request is the person's message the agent has, and the hook keeps it uncut.
func TestHookPreparesALongRequestWithTheHeadOfItsText(t *testing.T) {
	for name, c := range map[string]struct {
		prompt string
		whole  bool // the whole request fits in task
	}{
		"8,000 characters of English": {"Explain archive approval for production restore. " + strings.Repeat("Keep the restore conditions. ", 270), false},
		"3,000 characters of HTML":    {"Explain archive approval for production restore. " + strings.Repeat("<a>", 1000), true},
		"5,000 kana":                  {"\u672c\u756a\u306e\u5fa9\u5143\u306e\u627f\u8a8d\u3092\u8aac\u660e\u3057\u3066\u3002" + strings.Repeat("\u3042", 5000), true},
	} {
		t.Run(name, func(t *testing.T) {
			repo, root, _ := planningFixture(t)
			context, v := preparedHookWork(t, repo, c.prompt)
			if v == nil {
				t.Fatalf("the hook kept the request but delivered no prepared work: %.300s", context)
			}
			task, _ := v["task"].(string)
			status := v["task_context"].(map[string]any)["text_status"]
			if c.whole && (task != c.prompt || status != "stored_request") {
				t.Fatalf("the whole request fits, but task is %d of its %d bytes (%v)", len(task), len(c.prompt), status)
			}
			if !c.whole {
				next, _ := v["task_context"].(map[string]any)["next_action"].(string)
				if status != "delivered_head" || len(task) >= len(c.prompt) || !strings.HasPrefix(c.prompt, task) || !strings.Contains(next, "the person's message") {
					t.Fatalf("a request that does not fit comes as its head, and task_context says where the rest is: task %d of %d bytes, %v, %q", len(task), len(c.prompt), status, next)
				}
			}
			if s := loadSession(root, "planning"); s.RequestText != strings.TrimSpace(c.prompt) { // the hook trims the white space around it
				t.Fatalf("the hook keeps the whole request: %d of %d bytes", len(s.RequestText), len(strings.TrimSpace(c.prompt)))
			}
		})
	}
}
