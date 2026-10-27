package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

// When an enabled check fails at the end of a turn, Stop returns decision: block and the reason. On the Stop after a block (stop_hook_active) it does not block;
// it records whether the problem was fixed. status shows how many turns were stopped and how many were fixed.
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
		t.Fatalf("a failing Stop returns decision: block: %s", out)
	}
	reason, _ := parsed["reason"].(string)
	if !strings.Contains(reason, "no-ne") || !strings.Contains(reason, `README.md: "根"`) || !strings.Contains(reason, "Fix it before finishing") {
		t.Fatalf("the reason says which check failed and where: %s", reason)
	}
	recs := guardRecordsFor(root, "no-ne") // records of built-in ritual checks (plan-before-work etc.) are counted separately (0.8.0)
	if len(recs) != 1 || recs[0].Pass || recs[0].Recheck || recs[0].Guard != "no-ne" {
		t.Fatalf("the block is recorded: %+v", recs)
	}
	// The Stop after a block: even if not fixed yet it does not block (avoids an endless loop), but it keeps a record
	out = runHook(t, "stop", map[string]any{"session_id": "g", "cwd": repo, "last_assistant_message": "まだ", "stop_hook_active": true})
	if strings.Contains(out, "block") {
		t.Fatalf("a Stop with stop_hook_active does not block: %s", out)
	}
	if recs = guardRecordsFor(root, "no-ne"); len(recs) != 2 || recs[1].Pass || !recs[1].Recheck {
		t.Fatalf("record of not fixed: %+v", recs)
	}
	sOut := capture(t, func() error { return cmdStatus([]string{"--store", root}) })
	if !strings.Contains(sOut, "guards: 1 of 1 enabled; stopped 1 turns, fixed before the turn ended 0") {
		t.Fatalf("status shows stopped and fixed counts:\n%s", sOut)
	}
	// The Stop after a block, once fixed: a record of the fix
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("# shikii\n\n記録はプロジェクトルートに置く\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runHook(t, "stop", map[string]any{"session_id": "g", "cwd": repo, "last_assistant_message": "直した", "stop_hook_active": true})
	if recs = guardRecordsFor(root, "no-ne"); len(recs) != 3 || !recs[2].Pass || !recs[2].Recheck {
		t.Fatalf("record of the fix: %+v", recs)
	}
	if sOut = capture(t, func() error { return cmdStatus([]string{"--store", root}) }); !strings.Contains(sOut, "stopped 1 turns, fixed before the turn ended 1") {
		t.Fatalf("the fixed count goes up:\n%s", sOut)
	}
	// If nothing fails in the next turn, it ends normally (no block). The ritual check (sorting out the plan) is done beforehand (0.8.0)
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "g", "cwd": repo, "prompt": "README の誤字を直して"})
	if _, err := planningCall(t, "submit", map[string]any{"session_id": "g", "turn_id": loadSession(root, "g").TurnID, "agreed": map[string]any{"unknown": true}, "open": map[string]any{"unknown": true}}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("# shikii\n\n記録はプロジェクトルートに置く。\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if out = runHook(t, "stop", map[string]any{"session_id": "g", "cwd": repo, "last_assistant_message": "直した"}); strings.Contains(out, "block") {
		t.Fatalf("does not block when nothing fails: %s", out)
	}
}

// It also stops a changelog where only one side changed, and a commit with no body. It only looks at commits from the HEAD at the request until now.
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
	gitCommitAll(t, repo, "subject only before the request") // commits before the request are not checked
	writeGuard(t, root, guard{ID: "changelog-pair", Kind: guardKindPairedFiles, Title: "変更履歴は英語と日本語を揃える", Pairs: [][]string{{"CHANGELOG.md", "CHANGELOG_ja.md"}}})
	writeGuard(t, root, guard{ID: "commit-body", Kind: guardKindCommitBody, Title: "コミット本文に元の問題と検証", Required: []string{"元の問題", "検証"}})

	runHook(t, "user-prompt-submit", map[string]any{"session_id": "p", "cwd": repo, "prompt": "変更履歴に 0.7.3 の項目を足してコミットして"})
	if err := os.WriteFile(filepath.Join(repo, "CHANGELOG.md"), []byte("# log\n\n- 0.7.3\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitCommitAll(t, repo, "docs: add 0.7.3")
	out := runHook(t, "stop", map[string]any{"session_id": "p", "cwd": repo, "last_assistant_message": "足してコミットしました"})
	if !strings.Contains(out, `"decision":"block"`) || !strings.Contains(out, "CHANGELOG_ja.md did not") || !strings.Contains(out, `lacks \"元の問題\", \"検証\"`) {
		t.Fatalf("both checks stop: %s", out)
	}
	// Fix: add the Japanese side too, and redo the commit with a body
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
		t.Fatalf("after the fix, both records pass: %+v", recs)
	}
}

// guardRecordsFor returns only one check's records (kept apart from the built-in ritual records).
func guardRecordsFor(root, id string) []guardRecord {
	var out []guardRecord
	for _, r := range readGuardRecords(root) {
		if r.Guard == id {
			out = append(out, r)
		}
	}
	return out
}

// 4. commit-body does not look at commits when the current HEAD cannot be reached from the HEAD at request time (switched branches or pulled).
func TestCommitBodyIgnoresCommitsNotMadeInTheTurn(t *testing.T) {
	repo := gitRepo(t)
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(repo, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("a.txt", "a")
	gitCommitAll(t, repo, "base")
	if out, err := exec.Command("git", "-C", repo, "checkout", "-q", "-b", "feature").CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	write("b.txt", "b")
	if out, err := exec.Command("git", "-C", repo, "add", "-A").CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	old := exec.Command("git", "-C", repo, "-c", "user.name=t", "-c", "user.email=t@example.com", "commit", "-q", "-m", "feature: subject only")
	old.Env = append(os.Environ(), "GIT_COMMITTER_DATE=2026-10-21T00:00:00Z", "GIT_AUTHOR_DATE=2026-10-21T00:00:00Z")
	if out, err := old.CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	if out, err := exec.Command("git", "-C", repo, "checkout", "-q", "-").CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	head, since := gitHead(repo), time.Now()
	if out, err := exec.Command("git", "-C", repo, "checkout", "-q", "feature").CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	in := guardTurnInput(repo, "", nil, false, head, since)
	if len(in.Commits) != 0 {
		t.Fatalf("do not treat existing commits on the switched-to branch as this turn's: %+v", in.Commits)
	}
	// Commits stacked on top of the request's HEAD are looked at
	head, since = gitHead(repo), time.Now()
	write("c.txt", "c")
	gitCommitAll(t, repo, "made in this turn")
	if in = guardTurnInput(repo, "", nil, false, head, since); !in.CommitsKnown || len(in.Commits) != 1 {
		t.Fatalf("this turn's commits are looked at: %+v", in)
	}
}

// 5. A Stop that was blocked does not self-assess scope and completion (do not judge on the changes and reply from before the fix).
func TestBlockedStopDoesNotJudgeTheUnfinishedTurn(t *testing.T) {
	model := fakeAgentModel(t, func(string) float64 { return .1 })
	hookEnv(t, model)
	repo := gitRepo(t)
	t.Chdir(repo)
	capture(t, func() error { return cmdInit(nil) })
	root := filepath.Join(repo, ".shikii")
	os.WriteFile(filepath.Join(repo, "README.md"), []byte("# x\n"), 0o644)
	gitCommitAll(t, repo, "init")
	writeGuard(t, root, guard{ID: "no-ne", Kind: guardKindForbiddenWords, Title: "x", Words: []string{"根"}})
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "j", "cwd": repo, "prompt": "README を直して"})
	os.WriteFile(filepath.Join(repo, "README.md"), []byte("# x\n根\n"), 0o644)
	out := runHook(t, "stop", map[string]any{"session_id": "j", "cwd": repo, "last_assistant_message": "直した"})
	if !strings.Contains(out, "block") {
		t.Fatalf("blocks: %s", out)
	}
	if loadSession(root, "j").Judged {
		t.Fatal("a blocked turn is not marked as decided")
	}
	if b, _ := os.ReadFile(filepath.Join(root, "records", "done-as-asked.jsonl")); strings.TrimSpace(string(b)) != "" {
		t.Fatal("do not judge completion on the reply from before the fix")
	}
}

// With the English setting the terminal line and the reason stay English only.
func TestAuditTranslationGuardStopLineInEnglish(t *testing.T) {
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
	writeGuard(t, root, guard{ID: "no-foo", Kind: guardKindForbiddenWords, Title: "no foo", Words: []string{"foo"}})
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "g", "cwd": repo, "prompt": "add a line to the README"})
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("# shikii\n\nfoo bar\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := runHook(t, "stop", map[string]any{"session_id": "g", "cwd": repo, "last_assistant_message": "done"})
	var parsed map[string]any
	_ = json.Unmarshal([]byte(out), &parsed)
	sys, _ := parsed["systemMessage"].(string)
	recs := guardRecordsFor(root, "no-foo")
	if !strings.Contains(sys, "(no-foo)") || strings.Contains(sys, "\u6b62") || len(recs) != 1 || strings.Contains(recs[0].Reason, "\u4f7f") {
		t.Errorf("with the English setting there is no Japanese: %q %+v", sys, recs)
	}
}
