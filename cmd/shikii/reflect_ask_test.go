package main

import (
	"fmt"
	"strings"
	"testing"

	"github.com/hayaenfj/shikii/internal/store"
)

// The text asking for reflection contains none of the conversation's raw text (request, summary, the person's reply) (checked by content, not size)
func TestReflectAskCarriesNoOriginalText(t *testing.T) {
	repo := gitRepo(t)
	agent := hookEnv(t, nil, repo)
	t.Chdir(repo)
	st, scope := store.Store{Dir: agent}, feedbackScope(repo)
	var ctx string
	var ids []string
	for n := 1; n <= 2; n++ {
		id, _ := reviewSeed(t, st, scope, n)
		ids = append(ids, id)
		ctx = feedbackContext(t, runHook(t, "user-prompt-submit", map[string]any{"session_id": fmt.Sprint(n), "cwd": repo, "prompt": "合成の記録を調べてください。今回の内容を確認します。"}))
	}
	t.Logf("text size: %d bytes", len(ctx))
	// If the text does not appear, checking that no raw text is in it means nothing (final audit 0.9.1 T2). The asking text and the ids of the conversations to reflect on are there
	if !strings.Contains(ctx, "wrap up the reflection") {
		t.Fatalf("the text asking for reflection does not appear: %q", ctx)
	}
	for _, id := range ids {
		if !strings.Contains(ctx, id) {
			t.Errorf("the id %s of a conversation to reflect on is missing: %q", id, ctx)
		}
	}
	for _, raw := range []string{"README の起動手順を日本語にする", "起動手順を直しました", "まだ英語が残っています"} {
		if strings.Contains(ctx, raw) {
			t.Errorf("the conversation's raw text %q got into the text asking for reflection", raw)
		}
	}
}
