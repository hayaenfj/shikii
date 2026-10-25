package main

import (
	"fmt"
	"strings"
	"testing"

	"github.com/hayaenfj/shikii/internal/store"
)

// 振り返りを頼む文に、会話の原文(依頼・まとめ・人の返事)のどれも入らない(大きさではなく中身で見る)
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
	t.Logf("文の大きさ: %d バイト", len(ctx))
	// 文が出ていなければ、原文が入らないことを見ても意味が無い(最終監査 0.9.1 の T2)。頼む文と、振り返る会話の id は載る
	if !strings.Contains(ctx, "振り返りをまとめます") {
		t.Fatalf("振り返りを頼む文が出ていない: %q", ctx)
	}
	for _, id := range ids {
		if !strings.Contains(ctx, id) {
			t.Errorf("振り返る会話の id %s が載っていない: %q", id, ctx)
		}
	}
	for _, raw := range []string{"README の起動手順を日本語にする", "起動手順を直しました", "まだ英語が残っています"} {
		if strings.Contains(ctx, raw) {
			t.Errorf("振り返りを頼む文に会話の原文 %q が入った", raw)
		}
	}
}
