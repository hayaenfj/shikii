package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hayaenfj/shikii/internal/store"
)

// 0.7.3: whether delivered lessons changed behavior is counted from real-use observation, not a synthetic experiment. status shows the number of quotes delivered,
// the helpful / unused / harmful / unknown the reviews gave, and how many have no observation yet. Delivery of corrections is counted separately.
func TestStatusCountsDeliveredLessonsAndTheirEffect(t *testing.T) {
	st, scope, _, draft, revision := memoryEffectFixture(t)
	out := capture(t, func() error { return cmdStatus([]string{"--store", st.Dir}) })
	if !strings.Contains(out, "memory: no lessons delivered yet") {
		t.Fatalf("with no deliveries it must say so:\n%s", out)
	}
	memoryEffectPrompt(t, scope, "s1")
	out = capture(t, func() error { return cmdStatus([]string{"--store", st.Dir}) })
	if !strings.Contains(out, "memory: 1 quotes delivered") || !strings.Contains(out, "not yet assessed 1") {
		t.Fatalf("must count delivered quotes and show those awaiting observation:\n%s", out)
	}
	turn := memoryEffectComplete(t, st, scope, "s1") // an observation can be attached once the reply and the person's next message are both in
	if err := st.SaveMemoryAssessment(scope, store.MemoryAssessment{SourceID: turn, Key: "lesson:" + draft.ID + ":" + revision, Result: "helpful", Decision: "未初期化の場所の確認を手順に足した", Evidence: "返答に確認の手順が入った"}); err != nil {
		t.Fatal(err)
	}
	out = capture(t, func() error { return cmdStatus([]string{"--store", st.Dir}) })
	if !strings.Contains(out, "helpful 1, unused 0, harmful 0, unknown 0, not yet assessed 0") || !strings.Contains(out, "last 7 days: delivered 1, helpful 1") {
		t.Fatalf("once observed it counts as helpful and also shows the last 7 days:\n%s", out)
	}
}

// Audit R2: even in a store with no records and no specs, status shows the lines for delivered lessons and blocking checks (the README promises it).
func TestStatusWithoutRecordsStillPrintsMemoryAndGuards(t *testing.T) {
	dir := t.TempDir()
	store := filepath.Join(dir, ".shikii")
	if err := os.MkdirAll(store, 0o700); err != nil {
		t.Fatal(err)
	}
	out := capture(t, func() error { return cmdStatus([]string{"--store", store}) })
	if !strings.Contains(out, "memory:") || !strings.Contains(out, "guards:") {
		t.Fatalf("the memory and guards lines show even without records:\n%s", out)
	}
}

// Audit R3: records/guards.jsonl (the blocking checks' record) is not counted as a decision "guards". Even without spec paths it reads .shikii/decisions/*.yaml,
// counting the same way as the page (/v1/status) (a choice's discard counts as unsure).
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
		t.Fatalf("the blocking checks' record must not be counted as a decision:\n%s", out)
	}
	if !strings.Contains(out, "prompt-kind: records=1") || strings.Contains(out, "spec not given") {
		t.Fatalf("even without spec paths it must read the built-in questions in .shikii/decisions and show the calibration state:\n%s", out)
	}
}
