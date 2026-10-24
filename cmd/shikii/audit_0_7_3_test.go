package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// 監査(2026-10-23、0.7.3)の指摘を固定するテスト。

// 1. 0.6.0〜0.7.2 が作った .shikii/ には guards/ が無い。それでも登録済みとして扱い、0.7.0 を跨ぐ消去も効く(無い置き場は後で作る)。
func TestStoresWithoutGuardsDirStayRegisteredAndAreWiped(t *testing.T) {
	model := fakeAgentModel(t, func(string) float64 { return .1 })
	hookEnv(t, model)
	repo := gitRepo(t)
	t.Chdir(repo)
	capture(t, func() error { return cmdInit(nil) })
	common, _ := agentHome()
	root := filepath.Join(repo, ".shikii")
	if err := os.RemoveAll(filepath.Join(root, "guards")); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, p := range registeredProjects(common) {
		found = found || p.Path == repo
	}
	if !found {
		t.Fatal("guards/ の無い(古い版の)置き場も登録済みとして扱う")
	}
	leftover := filepath.Join(root, "records", "old.jsonl")
	if err := os.WriteFile(leftover, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(root, "project.json"), []byte(`{"version":1,"scope":"`+repo+`"}`), 0o600) // 0.7.3 以前の --init の印
	os.Remove(installedVersionPath(common))
	v, _ := updateVersion("0.7.3")
	wiped, err := resetStoresOnFirstRun(common, v, func(path string) error {
		prev, _ := os.Getwd()
		if err := os.Chdir(path); err != nil {
			return err
		}
		defer os.Chdir(prev)
		return cmdInit(nil)
	})
	if err != nil || !wiped {
		t.Fatalf("古い置き場も消して初期化し直す: wiped=%v err=%v", wiped, err)
	}
	if _, err := os.Stat(leftover); !os.IsNotExist(err) {
		t.Fatal("古い記録は消える")
	}
	if _, err := os.Stat(filepath.Join(root, "guards")); err != nil {
		t.Fatal("初期化し直しで guards/ ができる")
	}
}

// 2. HOME を初期化しない。消去は共通の置き場(~/.shikii/agent の親)には触れず、知らない項目があれば消さず、project.json は残し、1 件の失敗で止まらない。
func TestWipeNeverTouchesTheSharedPlaceAndKeepsUnknownEntries(t *testing.T) {
	hookEnv(t, nil)
	home, _ := os.UserHomeDir()
	t.Chdir(home)
	if err := cmdInit(nil); err == nil {
		t.Fatal("HOME は初期化しない(消去が ~/.shikii/agent を巻き込む)")
	}
	common, _ := agentHome()
	if err := wipeProjectStores([]projectEntry{{Path: home}}, func(string) error { return nil }); err == nil {
		t.Fatal("共通の置き場の親は消さない")
	}
	if _, err := os.Stat(filepath.Join(common, "decisions")); err != nil {
		t.Fatal("共通の置き場は無事: ", err)
	}
	// 知らない項目があるプロジェクトは消さない。project.json は消さない。失敗しても次へ進む
	known := t.TempDir()
	for _, d := range []string{"decisions", "records", "wiki"} {
		os.MkdirAll(filepath.Join(known, ".shikii", d), 0o700)
	}
	os.WriteFile(filepath.Join(known, ".shikii", "records", "x.jsonl"), []byte("x"), 0o600)
	os.WriteFile(filepath.Join(known, ".shikii", "project.json"), []byte(`{"version":1}`), 0o600)
	odd := t.TempDir()
	os.MkdirAll(filepath.Join(odd, ".shikii", "records"), 0o700)
	os.WriteFile(filepath.Join(odd, ".shikii", "shikii_aside"), []byte("?"), 0o600)
	failing := t.TempDir()
	os.MkdirAll(filepath.Join(failing, ".shikii", "records"), 0o700)
	var reinit []string
	err := wipeProjectStores([]projectEntry{{Path: failing}, {Path: odd}, {Path: known}}, func(path string) error {
		reinit = append(reinit, path)
		if path == failing {
			return os.ErrPermission
		}
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), failing) || !strings.Contains(err.Error(), odd) {
		t.Fatalf("失敗と拒んだプロジェクトを全部報告する: %v", err)
	}
	if _, e := os.Stat(filepath.Join(odd, ".shikii", "shikii_aside")); e != nil {
		t.Fatal("知らない項目があるプロジェクトは何も消さない")
	}
	if _, e := os.Stat(filepath.Join(known, ".shikii", "records", "x.jsonl")); !os.IsNotExist(e) {
		t.Fatal("知っている項目は消す")
	}
	if _, e := os.Stat(filepath.Join(known, ".shikii", "project.json")); e != nil {
		t.Fatal("project.json は残す(初期化し直しに失敗しても登録から消えない)")
	}
	if len(reinit) != 2 || reinit[1] != known {
		t.Fatalf("1 件の失敗で止まらず次へ進む: %v", reinit)
	}
	// .shikii がリンクなら消さない(監査の変異 A)
	linked := t.TempDir()
	target := t.TempDir()
	if err := os.Symlink(target, filepath.Join(linked, ".shikii")); err != nil {
		t.Skip(err)
	}
	os.WriteFile(filepath.Join(target, "keep"), []byte("k"), 0o600)
	if err := wipeProjectStores([]projectEntry{{Path: linked}}, func(string) error { return nil }); err == nil {
		t.Fatal("リンクの .shikii は消さない")
	}
	if _, e := os.Stat(filepath.Join(target, "keep")); e != nil {
		t.Fatal("リンク先は無事")
	}
}

// 3. hook install の表示(--yes 無し)に、消す対象のプロジェクトが出る。
func TestHookInstallPreviewListsTheProjectsItWouldWipe(t *testing.T) {
	model := fakeAgentModel(t, func(string) float64 { return .1 })
	hookEnv(t, model)
	repo := gitRepo(t)
	t.Chdir(repo)
	capture(t, func() error { return cmdInit(nil) })
	common, _ := agentHome()
	os.WriteFile(filepath.Join(repo, ".shikii", "project.json"), []byte(`{"version":1,"scope":"`+repo+`"}`), 0o600) // 0.7.3 以前の --init の印
	os.Remove(installedVersionPath(common))
	// 監査 0.8.1-1 の 15: 作業記録(作り直せない書きかけのファイル)も消えることを件数で出す
	os.MkdirAll(filepath.Join(repo, ".shikii", "handoff", "job"), 0o700)
	os.WriteFile(filepath.Join(repo, ".shikii", "handoff", "job", "a.md"), []byte("a"), 0o600)
	os.WriteFile(filepath.Join(repo, ".shikii", "handoff", "job", "b.md"), []byte("b"), 0o600)
	os.WriteFile(filepath.Join(repo, ".shikii", "handoff", "c.md"), []byte("c"), 0o600) // 直下の項目は 2 つ、ファイルは 3 つ(一覧と同じくファイルを数える)
	out := capture(t, func() error { return cmdHook([]string{"install"}) })
	if !strings.Contains(out, "消して初期化し直します") || !strings.Contains(out, repo) {
		t.Fatalf("表示に消す対象が出る:\n%s", out)
	}
	if !strings.Contains(out, "作業記録 3 件(.shikii/handoff/)も消えます") {
		t.Fatalf("作業記録も消えることを件数で出す:\n%s", out)
	}
	if _, err := os.Stat(installedVersionPath(common)); err == nil {
		t.Fatal("表示だけでは印を書かない")
	}
	writeInstalledVersion(common, currentVersion())
	if out = capture(t, func() error { return cmdHook([]string{"install"}) }); strings.Contains(out, "消して初期化し直します") {
		t.Fatalf("跨がなければ出ない:\n%s", out)
	}
}

// 4. commit-body は、依頼の時の HEAD から今の HEAD が辿れない(ブランチを切り替えた・pull した)ならコミットを見ない。
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
		t.Fatalf("切り替え先の既存のコミットをこのターンのものにしない: %+v", in.Commits)
	}
	// 依頼の HEAD の上に積んだコミットは見る
	head, since = gitHead(repo), time.Now()
	write("c.txt", "c")
	gitCommitAll(t, repo, "made in this turn")
	if in = guardTurnInput(repo, "", nil, false, head, since); !in.CommitsKnown || len(in.Commits) != 1 {
		t.Fatalf("このターンのコミットは見る: %+v", in)
	}
}

// 5. 止めた Stop では範囲・完了の自己評価をしない(直す前の変更と返答で判定しない)。
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
		t.Fatalf("止まる: %s", out)
	}
	if loadSession(root, "j").Judged {
		t.Fatal("止めたターンは判定済みにしない")
	}
	if b, _ := os.ReadFile(filepath.Join(root, "records", "done-as-asked.jsonl")); strings.TrimSpace(string(b)) != "" {
		t.Fatal("直す前の返答で完了を判定しない")
	}
}

// 6. except が広い glob でも enable できる(負例のパスは except に合わない名前にする)。
func TestGuardEnableWorksWhenExceptIsBroad(t *testing.T) {
	hookEnv(t, nil)
	repo := gitRepo(t)
	t.Chdir(repo)
	capture(t, func() error { return cmdInit(nil) })
	root := filepath.Join(repo, ".shikii")
	if err := saveGuard(root, guard{ID: "no-todo", Kind: guardKindForbiddenWords, Title: "TODO を残さない", Words: []string{"TODO"}, Except: []string{"*.txt", "docs/**"}}); err != nil {
		t.Fatal(err)
	}
	out := capture(t, func() error { return cmdGuard([]string{"enable", "no-todo"}) })
	if !strings.Contains(out, "RED:") {
		t.Fatalf("except が広くても負例で止まる: %s", out)
	}
}

// 7. パスの照合は名前だけでも合う(監査の変異 B)。
func TestMatchPathMatchesByBaseName(t *testing.T) {
	if !matchPath("CHANGELOG.md", "docs/CHANGELOG.md") || matchPath("CHANGELOG.md", "docs/CHANGELOG_ja.md") || !matchPath("docs/**", "docs/a/b.md") || matchPath("docs/**", "src/docs.md") {
		t.Fatal("名前だけの一致と配下の一致")
	}
}
