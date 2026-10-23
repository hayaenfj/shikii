package main

import (
	"os"
	"path/filepath"
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

// 監査 R2: 記録も spec も無い置き場でも、status は届いた教訓と止める検査の行を出す(README が約束している)。
func TestStatusWithoutRecordsStillPrintsMemoryAndGuards(t *testing.T) {
	dir := t.TempDir()
	store := filepath.Join(dir, ".shikii")
	if err := os.MkdirAll(store, 0o700); err != nil {
		t.Fatal(err)
	}
	out := capture(t, func() error { return cmdStatus([]string{"--store", store}) })
	if !strings.Contains(out, "memory:") || !strings.Contains(out, "guards:") {
		t.Fatalf("記録が無くても memory と guards の行は出る:\n%s", out)
	}
}

// 監査 R3: records/guards.jsonl(止める検査の記録)を判定「guards」として数えない。spec のパスを省いても .shikii/decisions/*.yaml を読み、
// 画面(/v1/status)と同じ数え方(choice の discard を迷いに数える)になる。
func TestStatusIgnoresGuardRecordsAndReadsTheStoresSpecsByDefault(t *testing.T) {
	model := fakeAgentModel(t, func(string) float64 { return .1 })
	hookEnv(t, model)
	repo := gitRepo(t)
	t.Chdir(repo)
	capture(t, func() error { return cmdInit(nil) })
	root := filepath.Join(repo, ".shikii")
	writeGuard(t, root, guard{ID: "no-ne", Kind: guardKindForbiddenWords, Title: "x", Words: []string{"xyz"}})
	if err := appendGuardRecords(root, []guardRecord{{Session: "s", Turn: "t", Guard: "no-ne", Pass: false, Reason: "r"}}); err != nil {
		t.Fatal(err)
	}
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "st", "cwd": repo, "prompt": "README を直して"})
	out := capture(t, func() error { return cmdStatus([]string{"--store", root}) })
	if strings.Contains(out, "guards: records=") {
		t.Fatalf("止める検査の記録を判定として数えない:\n%s", out)
	}
	if !strings.Contains(out, "prompt-kind: records=1") || strings.Contains(out, "spec not given") {
		t.Fatalf("spec のパスを省いても .shikii/decisions の既製の問いを読み、較正の状態を出す:\n%s", out)
	}
}
