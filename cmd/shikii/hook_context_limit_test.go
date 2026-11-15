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
