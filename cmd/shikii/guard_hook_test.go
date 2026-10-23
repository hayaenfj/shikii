package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func writeGuard(t *testing.T, root string, g guard) {
	t.Helper()
	g.Enabled = true
	if err := saveGuard(root, g); err != nil {
		t.Fatal(err)
	}
}

func gitCommitAll(t *testing.T, repo, message string) {
	t.Helper()
	if out, err := exec.Command("git", "-C", repo, "add", "-A").CombinedOutput(); err != nil {
		t.Fatalf("git add: %v %s", err, out)
	}
	if out, err := exec.Command("git", "-C", repo, "-c", "user.name=t", "-c", "user.email=t@example.com", "commit", "-q", "-m", message).CombinedOutput(); err != nil {
		t.Fatalf("git commit: %v %s", err, out)
	}
}

// ターンの終わりに有効な検査が引っかかると、Stop は decision: block と理由を返す。止めた後の Stop(stop_hook_active)では止めず、
// 直ったかを記録する。status に止めた数と直った数が出る。
func TestStopIsBlockedByAnEnabledGuardAndTheFixIsRecorded(t *testing.T) {
	model := fakeAgentModel(t, func(string) float64 { return .1 })
	hookEnv(t, model)
	repo := gitRepo(t)
	t.Chdir(repo)
	capture(t, func() error { return cmdInit(nil) })
	root := filepath.Join(repo, ".shikii")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("# shikii\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitCommitAll(t, repo, "init")
	writeGuard(t, root, guard{ID: "no-ne", Kind: guardKindForbiddenWords, Title: "根を使わない", Words: []string{"根"}, Except: []string{"docs/operations.md"}})

	runHook(t, "user-prompt-submit", map[string]any{"session_id": "g", "cwd": repo, "prompt": "README に置き場の説明を足して"})
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("# shikii\n\n記録はプロジェクトの根に置く\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := runHook(t, "stop", map[string]any{"session_id": "g", "cwd": repo, "last_assistant_message": "足しました"})
	var parsed map[string]any
	if err := json.Unmarshal([]byte(out), &parsed); err != nil || parsed["decision"] != "block" {
		t.Fatalf("引っかかった Stop は decision: block を返す: %s", out)
	}
	reason, _ := parsed["reason"].(string)
	if !strings.Contains(reason, "no-ne") || !strings.Contains(reason, "README.md: 「根」") || !strings.Contains(reason, "直してから終えて") {
		t.Fatalf("理由にどの検査がどこで引っかかったかを書く: %s", reason)
	}
	recs := guardRecordsFor(root, "no-ne") // 組み込みの作法の検査(plan-before-work など)の記録は別に数える(0.7.6)
	if len(recs) != 1 || recs[0].Pass || recs[0].Recheck || recs[0].Guard != "no-ne" {
		t.Fatalf("止めた記録が残る: %+v", recs)
	}
	// 止めた後の Stop: まだ直っていなくても止めない(無限ループを避ける)が、記録は残す
	out = runHook(t, "stop", map[string]any{"session_id": "g", "cwd": repo, "last_assistant_message": "まだ", "stop_hook_active": true})
	if strings.Contains(out, "block") {
		t.Fatalf("stop_hook_active の Stop では止めない: %s", out)
	}
	if recs = guardRecordsFor(root, "no-ne"); len(recs) != 2 || recs[1].Pass || !recs[1].Recheck {
		t.Fatalf("直っていない記録: %+v", recs)
	}
	sOut := capture(t, func() error { return cmdStatus([]string{"--store", root}) })
	if !strings.Contains(sOut, "guards: 1 of 1 enabled; stopped 1 turns, fixed before the turn ended 0") {
		t.Fatalf("status に止めた数と直った数:\n%s", sOut)
	}
	// 直してから止めた後の Stop: 直った記録
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("# shikii\n\n記録はプロジェクトルートに置く\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runHook(t, "stop", map[string]any{"session_id": "g", "cwd": repo, "last_assistant_message": "直した", "stop_hook_active": true})
	if recs = guardRecordsFor(root, "no-ne"); len(recs) != 3 || !recs[2].Pass || !recs[2].Recheck {
		t.Fatalf("直った記録: %+v", recs)
	}
	if sOut = capture(t, func() error { return cmdStatus([]string{"--store", root}) }); !strings.Contains(sOut, "stopped 1 turns, fixed before the turn ended 1") {
		t.Fatalf("直った数が増える:\n%s", sOut)
	}
	// 次のターンで引っかからなければ、普通に終わる(block 無し)。作法の検査(計画の整理)は済ませておく(0.7.6)
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "g", "cwd": repo, "prompt": "README の誤字を直して"})
	if _, err := planningCall(t, "submit", map[string]any{"session_id": "g", "turn_id": loadSession(root, "g").TurnID, "agreed": map[string]any{"unknown": true}, "open": map[string]any{"unknown": true}}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("# shikii\n\n記録はプロジェクトルートに置く。\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if out = runHook(t, "stop", map[string]any{"session_id": "g", "cwd": repo, "last_assistant_message": "直した"}); strings.Contains(out, "block") {
		t.Fatalf("引っかからなければ止めない: %s", out)
	}
}

// 片方だけ変えた変更履歴と、本文の無いコミットも止める。依頼の時の HEAD から今までのコミットだけを見る。
func TestStopIsBlockedByPairedFilesAndCommitBody(t *testing.T) {
	model := fakeAgentModel(t, func(string) float64 { return .1 })
	hookEnv(t, model)
	repo := gitRepo(t)
	t.Chdir(repo)
	capture(t, func() error { return cmdInit(nil) })
	root := filepath.Join(repo, ".shikii")
	for _, f := range []string{"CHANGELOG.md", "CHANGELOG_ja.md"} {
		if err := os.WriteFile(filepath.Join(repo, f), []byte("# log\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	gitCommitAll(t, repo, "subject only before the request") // 依頼の前のコミットは見ない
	writeGuard(t, root, guard{ID: "changelog-pair", Kind: guardKindPairedFiles, Title: "変更履歴は英語と日本語を揃える", Pairs: [][]string{{"CHANGELOG.md", "CHANGELOG_ja.md"}}})
	writeGuard(t, root, guard{ID: "commit-body", Kind: guardKindCommitBody, Title: "コミット本文に元の問題と検証", Required: []string{"元の問題", "検証"}})

	runHook(t, "user-prompt-submit", map[string]any{"session_id": "p", "cwd": repo, "prompt": "変更履歴に 0.7.3 の項目を足してコミットして"})
	if err := os.WriteFile(filepath.Join(repo, "CHANGELOG.md"), []byte("# log\n\n- 0.7.3\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitCommitAll(t, repo, "docs: add 0.7.3")
	out := runHook(t, "stop", map[string]any{"session_id": "p", "cwd": repo, "last_assistant_message": "足してコミットしました"})
	if !strings.Contains(out, `"decision":"block"`) || !strings.Contains(out, "CHANGELOG_ja.md が変わっていない") || !strings.Contains(out, "本文に 「元の問題」・「検証」 が無い") {
		t.Fatalf("両方の検査で止める: %s", out)
	}
	// 直す: 日本語も足し、本文付きで積み直す
	if err := os.WriteFile(filepath.Join(repo, "CHANGELOG_ja.md"), []byte("# log\n\n- 0.7.3\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if o, err := exec.Command("git", "-C", repo, "add", "-A").CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, o)
	}
	if o, err := exec.Command("git", "-C", repo, "-c", "user.name=t", "-c", "user.email=t@example.com", "commit", "-q", "--amend", "-m", "docs: add 0.7.3\n\n元の問題: 項目が無かった。\n検証: 両方の変更履歴を目で確かめた。").CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, o)
	}
	runHook(t, "stop", map[string]any{"session_id": "p", "cwd": repo, "last_assistant_message": "直した", "stop_hook_active": true})
	recs := readGuardRecords(root)
	fixed := 0
	for _, r := range recs {
		if r.Recheck && r.Pass {
			fixed++
		}
	}
	if fixed != 2 {
		t.Fatalf("直した後の記録は両方 pass: %+v", recs)
	}
}

// guardRecordsFor は 1 つの検査の記録だけ(組み込みの作法の記録と分けて見る)。
func guardRecordsFor(root, id string) []guardRecord {
	var out []guardRecord
	for _, r := range readGuardRecords(root) {
		if r.Guard == id {
			out = append(out, r)
		}
	}
	return out
}
