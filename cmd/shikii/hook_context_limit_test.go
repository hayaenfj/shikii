package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/hayaenfj/shikii/internal/notebook"
	"github.com/hayaenfj/shikii/internal/store"
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

// A request the hook keeps can be longer than what the agent is shown with the rest of the prepared work. The preparation was then
// refused whole, and the agent got only the notice to prepare it again (the checks of 2026-11-16: about 7,000 characters of
// English, 3,000 of pasted HTML, whose <, > and & took six characters each in the JSON, and any request over the 64 KiB the hook
// keeps). Now the hook writes its JSON without escaping those characters, a request that fits comes whole, and one that does not
// comes as its head, of at most hookTaskHeadMax characters as delivered, with task_context saying where the rest is. The work is
// matched (the reading case, the focus of the lessons, the procedure) on the request the hook keeps, not on the head it delivers.
func TestHookPreparesAnyRequestWithinTheBound(t *testing.T) {
	const en, ja = "Explain archive approval for production restore. ", "\u672c\u756a\u306e\u5fa9\u5143\u306e\u627f\u8a8d\u3092\u8aac\u660e\u3057\u3066\u3002"
	for name, c := range map[string]struct {
		prompt  string
		lessons string // "", "en" or "ja": six long lessons the request finds
		want    string // "whole", "head" (delivered_head) or "kept" (over what the hook keeps: stored_request_truncated)
	}{
		"8,000 characters of English":               {en + strings.Repeat("Keep the restore conditions. ", 270), "", "head"},
		"20,000 characters of English with lessons": {en + strings.Repeat("Keep the restore conditions. ", 690), "en", "head"},
		"70,000 characters of English":              {en + strings.Repeat("Keep the restore conditions. ", 2420), "", "kept"},
		"3,000 characters of HTML":                  {en + strings.Repeat("<a>", 1000), "en", "whole"},
		"25,000 characters of &<>":                  {en + strings.Repeat("&<>", 8400), "", "head"},
		"5,000 kana":                                {ja + strings.Repeat("\u3042", 5000), "", "whole"},
		"9,000 kana with Japanese lessons":          {ja + strings.Repeat("\u3042", 9000), "ja", "head"},
		"25,000 kana":                               {ja + strings.Repeat("\u3042", 25000), "", "kept"},
		"20,000 emoji":                              {en + strings.Repeat("\U0001F600", 20000), "", "kept"},
		"5,000 control characters":                  {en + strings.Repeat("\x01", 5000), "", "head"},
		"quotes, backslashes and new lines":         {en + strings.Repeat("<&>\"\\\n", 1500), "", "head"},
	} {
		t.Run(name, func(t *testing.T) {
			repo, root, _ := planningFixture(t)
			b := notebook.Book{Root: root, Scope: feedbackScope(repo)}
			for i := 0; i < 6 && c.lessons != ""; i++ {
				if c.lessons == "en" {
					interimAddArticle(t, b, fmt.Sprintf("Archive approval %d", i), strings.Repeat("Production restore requires owner approval before the archive is restored. ", 40))
				} else {
					interimAddArticle(t, b, fmt.Sprintf("\u30a2\u30fc\u30ab\u30a4\u30d6\u306e\u627f\u8a8d %d", i), strings.Repeat("\u672c\u756a\u306e\u5fa9\u5143\u306b\u306f\u6240\u6709\u8005\u306e\u627f\u8a8d\u304c\u8981\u308b\u3002\u30a2\u30fc\u30ab\u30a4\u30d6\u3092\u623b\u3059\u524d\u306b\u627f\u8a8d\u3092\u5f97\u308b\u3053\u3068\u3002", 60))
				}
			}
			context, v := preparedHookWork(t, repo, c.prompt)
			if v == nil {
				t.Fatalf("the hook kept the request (%d bytes) but delivered no prepared work: %.200s", len(c.prompt), context)
			}
			whole := strings.TrimSpace(c.prompt) // the hook trims the white space around a request
			task, _ := v["task"].(string)
			taskContext, _ := v["task_context"].(map[string]any)
			status, _ := taskContext["text_status"].(string)
			next, _ := taskContext["next_action"].(string)
			if provided, _ := taskContext["provided_bytes"].(float64); int(provided) != len(task) || !strings.HasPrefix(whole, task) {
				t.Fatalf("task must be a head of the request and provided_bytes its length: provided_bytes %v, task %d bytes", taskContext["provided_bytes"], len(task))
			}
			delivered, _ := orderedText(task)
			size := hookContextLength(string(delivered))
			switch c.want {
			case "whole":
				if task != whole || status != "stored_request" {
					t.Fatalf("the whole request fits, but task is %d of its %d bytes (%s)", len(task), len(whole), status)
				}
			case "head", "kept":
				wantStatus, wantNext := "delivered_head", "the person's message"
				if c.want == "kept" {
					wantStatus, wantNext = "stored_request_truncated", "original human message"
				}
				if status != wantStatus || !strings.Contains(next, wantNext) {
					t.Fatalf("a request that does not fit comes as its head, and task_context says where the rest is: %s, %q", status, next)
				}
				// the head is measured as it is delivered: the next character would not fit (one character is at most \u0001)
				if size > hookTaskHeadMax || size <= hookTaskHeadMax-len(`\u0001`) {
					t.Fatalf("the head of the request is %d characters as delivered, not the longest within %d", size, hookTaskHeadMax)
				}
			}
			s := loadSession(root, "planning")
			if want := cut(whole, store.RecordTextLimit+4); s.RequestText != want {
				t.Fatalf("the hook keeps the request up to its bound: %d bytes, want %d", len(s.RequestText), len(want))
			}
			reading := s.Planning.Work.Reasoning.Reading
			if reading == nil {
				t.Fatal("precondition: the preparation keeps a reading trace")
			}
			if want := workHash(cut(s.RequestText, store.RecordTextLimit))[:16]; reading.Case != want {
				t.Fatalf("the reading case is %s, the hash of the delivered head; the request the hook keeps hashes to %s", reading.Case, want)
			}
		})
	}
}
