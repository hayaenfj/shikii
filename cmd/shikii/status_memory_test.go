package main

import (
	"strings"
	"testing"

	"github.com/hayaenfj/shikii/internal/store"
)

// 0.7.3: 届いた教訓が行動を変えたかは、合成実験ではなく本番の観察で数える。status に、届けた引用の数と
// 振り返りが付けた helpful / unused / harmful / unknown、まだ観察の無い数を出す。訂正の配送は別に数える。
func TestStatusCountsDeliveredLessonsAndTheirEffect(t *testing.T) {
	st, scope, _, draft, revision := memoryEffectFixture(t)
	out := capture(t, func() error { return cmdStatus([]string{"--store", st.Dir}) })
	if !strings.Contains(out, "memory: no lessons delivered yet") {
		t.Fatalf("配送が無ければそう言う:\n%s", out)
	}
	memoryEffectPrompt(t, scope, "s1")
	out = capture(t, func() error { return cmdStatus([]string{"--store", st.Dir}) })
	if !strings.Contains(out, "memory: 1 quotes delivered") || !strings.Contains(out, "not yet assessed 1") {
		t.Fatalf("届けた引用を数え、観察待ちを出す:\n%s", out)
	}
	turn := memoryEffectComplete(t, st, scope, "s1") // 返答と次の人の発言が揃ってから観察を付けられる
	if err := st.SaveMemoryAssessment(scope, store.MemoryAssessment{SourceID: turn, Key: "lesson:" + draft.ID + ":" + revision, Result: "helpful", Decision: "未初期化の場所の確認を手順に足した", Evidence: "返答に確認の手順が入った"}); err != nil {
		t.Fatal(err)
	}
	out = capture(t, func() error { return cmdStatus([]string{"--store", st.Dir}) })
	if !strings.Contains(out, "helpful 1, unused 0, harmful 0, unknown 0, not yet assessed 0") || !strings.Contains(out, "last 7 days: delivered 1, helpful 1") {
		t.Fatalf("観察が付けば helpful に数え、直近 7 日も出す:\n%s", out)
	}
}
