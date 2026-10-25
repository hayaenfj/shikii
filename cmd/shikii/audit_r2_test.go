package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 監査 第 2 ラウンド(2026-10-23)の消去まわりの指摘を固定する。

func initAt(t *testing.T, dir string) {
	t.Helper()
	prev, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(prev)
	capture(t, func() error { return cmdInit(nil) })
}

// 1. 消去が 1 件拒まれて印が書けなくても、消して初期化し直した方のプロジェクトは次の実行で二度消されない(各プロジェクトの印で判断)。
// 2. --init は 0.7.4 以降の印(version 2)を書き、共通の印が無ければ今の版を書くので、go install → --init → hook install --yes の順でも消されない。
func TestWipeMarksEachProjectDoneAndInitWritesTheMarkers(t *testing.T) {
	model := fakeAgentModel(t, func(string) float64 { return .1 })
	hookEnv(t, model)
	common, _ := agentHome()
	os.Remove(installedVersionPath(common))
	os.RemoveAll(filepath.Join(common, "projects")) // hookEnv が登録した分を消し、この機械で最初の --init にする
	a := gitRepo(t)
	initAt(t, a)
	if _, err := os.Stat(installedVersionPath(common)); err != nil {
		t.Fatal("--init は共通の印が無ければ今の版を書く(go install で入れた人が初回の hook install で消されない): ", err)
	}
	if v := projectInitVersion(filepath.Join(a, ".shikii")); v < 2 {
		t.Fatalf("--init は各プロジェクトの印を version 2 で書く: %d", v)
	}
	// 0.6.0 相当の 2 プロジェクト(version 1、共通の印なし)。片方には知らない項目
	os.Remove(installedVersionPath(common))
	b, c := gitRepo(t), gitRepo(t)
	initAt(t, b)
	initAt(t, c)
	for _, d := range []string{b, c} {
		os.WriteFile(filepath.Join(d, ".shikii", "project.json"), []byte(`{"version":1,"scope":"`+d+`"}`), 0o600)
		os.WriteFile(filepath.Join(d, ".shikii", "records", "old.jsonl"), []byte("old"), 0o600)
	}
	os.WriteFile(filepath.Join(c, ".shikii", "mystery"), []byte("?"), 0o600)
	os.Remove(installedVersionPath(common))
	v, _ := updateVersion("0.7.4")
	reinit := func(path string) error { initAt(t, path); return nil }
	if _, err := resetStoresOnFirstRun(common, v, reinit); err == nil || !strings.Contains(err.Error(), "mystery") {
		t.Fatalf("知らない項目のあるプロジェクトを拒み、理由を返す: %v", err)
	}
	if _, err := os.Stat(filepath.Join(b, ".shikii", "records", "old.jsonl")); !os.IsNotExist(err) {
		t.Fatal("拒まれなかった方は消して初期化し直す")
	}
	if _, err := os.Stat(installedVersionPath(common)); err == nil {
		t.Fatal("失敗が残れば共通の印は書かない(拒まれた方を次の実行で試すため)")
	}
	os.WriteFile(filepath.Join(b, ".shikii", "records", "after.jsonl"), []byte("new"), 0o600)
	if _, err := resetStoresOnFirstRun(common, v, reinit); err == nil {
		t.Fatal("まだ拒まれる方があるので失敗のまま")
	}
	if _, err := os.Stat(filepath.Join(b, ".shikii", "records", "after.jsonl")); err != nil {
		t.Fatal("初期化し直したプロジェクトは二度消さない(version 2 の印があるため): ", err)
	}
	os.Remove(filepath.Join(c, ".shikii", "mystery"))
	if _, err := resetStoresOnFirstRun(common, v, reinit); err != nil {
		t.Fatalf("片付けたら残りを消して成功する: %v", err)
	}
	if b, _ := os.ReadFile(installedVersionPath(common)); strings.TrimSpace(string(b)) != "0.7.4" {
		t.Fatalf("全部済めば共通の印を書く: %q", b)
	}
}

// 3. 壊れた・空の印は 0.0.0 とみなさず、消さずに止める。
func TestBrokenInstalledVersionMarkerRefusesToWipe(t *testing.T) {
	model := fakeAgentModel(t, func(string) float64 { return .1 })
	hookEnv(t, model)
	common, _ := agentHome()
	repo := gitRepo(t)
	initAt(t, repo)
	os.WriteFile(filepath.Join(repo, ".shikii", "project.json"), []byte(`{"version":1,"scope":"`+repo+`"}`), 0o600)
	keep := filepath.Join(repo, ".shikii", "records", "keep.jsonl")
	os.WriteFile(keep, []byte("k"), 0o600)
	v, _ := updateVersion("0.7.4")
	for _, broken := range []string{"", "garbage\n", "0.7\n"} {
		os.WriteFile(installedVersionPath(common), []byte(broken), 0o644)
		wiped, err := resetStoresOnFirstRun(common, v, func(string) error { return nil })
		if err == nil || wiped {
			t.Fatalf("印 %q は読めないので消さずに止める: wiped=%v err=%v", broken, wiped, err)
		}
		if _, err := os.Stat(keep); err != nil {
			t.Fatalf("印 %q: 記録は無事", broken)
		}
	}
}

// 4. HOME の判定は実体(SameFile)で: 大文字小文字の違う綴りでも拒む(大文字小文字を区別しない FS でだけ意味がある)。
func TestInitRefusesHomeSpelledDifferently(t *testing.T) {
	hookEnv(t, nil)
	home, _ := os.UserHomeDir()
	upper := strings.ToUpper(home)
	if upper == home {
		t.Skip("大文字の綴りが作れない")
	}
	if _, err := os.Stat(upper); err != nil {
		t.Skip("大文字小文字を区別する FS")
	}
	if err := os.Chdir(upper); err != nil {
		t.Skip(err)
	}
	t.Cleanup(func() { os.Chdir(home) })
	if err := cmdInit(nil); err == nil || !strings.Contains(err.Error(), "ホーム") {
		t.Fatalf("綴りが違っても HOME は初期化しない: %v", err)
	}
}

// 5. hook install の表示(--yes 無し)は、消すものと消さないもの(知らない項目、version 2)を分けて出す。
func TestHookInstallPreviewExplainsWhatItWouldSkip(t *testing.T) {
	model := fakeAgentModel(t, func(string) float64 { return .1 })
	hookEnv(t, model)
	common, _ := agentHome()
	old, odd, fresh := gitRepo(t), gitRepo(t), gitRepo(t)
	for _, d := range []string{old, odd, fresh} {
		initAt(t, d)
	}
	for _, d := range []string{old, odd} {
		os.WriteFile(filepath.Join(d, ".shikii", "project.json"), []byte(`{"version":1,"scope":"`+d+`"}`), 0o600)
	}
	os.WriteFile(filepath.Join(odd, ".shikii", "mystery"), []byte("?"), 0o600)
	os.Remove(installedVersionPath(common))
	out := capture(t, func() error { return cmdHook([]string{"install"}) })
	if !strings.Contains(out, old) || !strings.Contains(out, "is removed and initialized again") {
		t.Fatalf("消す対象を出す:\n%s", out)
	}
	if !strings.Contains(out, odd) || !strings.Contains(out, "not removed: ") || !strings.Contains(out, "mystery") {
		t.Fatalf("知らない項目があるプロジェクトは消さないと理由付きで出す:\n%s", out)
	}
	if !strings.Contains(out, "done: "+fresh) {
		t.Fatalf("この版で初期化したプロジェクト(version 2)は「済み」と出す:\n%s", out)
	}
}

// 6. 共通の置き場の判定は単独でも効く(監査 R2 の変異 M6: 許可リストに頼らない)。planWipe は HOME を「消しません」にする。
func TestSharedPlaceIsRefusedByItself(t *testing.T) {
	hookEnv(t, nil)
	home, _ := os.UserHomeDir()
	if !isSharedPlaceScope(home) || !isSharedPlaceScope(home+string(filepath.Separator)) {
		t.Fatal("HOME は共通の置き場")
	}
	if isSharedPlaceScope(t.TempDir()) {
		t.Fatal("他のフォルダは共通の置き場ではない")
	}
	plan := planWipe([]projectEntry{{Path: home, InitVersion: 1}})
	if len(plan) != 1 || plan[0].Action != wipeRefuse || !strings.Contains(plan[0].Reason, "共通の置き場") {
		t.Fatalf("HOME は許可リストを見る前に拒む: %+v", plan)
	}
}
