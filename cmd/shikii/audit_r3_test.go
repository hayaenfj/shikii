package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 監査 第 3 ラウンド(2026-10-23)の指摘を固定する。

// 1. 0.7.3 以前の印(version 1)の置き場に新しい本体で --init をやり直しても、0.7.0 を跨ぐ消去がまだなら version 2 を書かない(消去が永久に飛ぶのを防ぐ)。
func TestReinitDoesNotMarkAnOldStoreDoneWhileTheWipeIsPending(t *testing.T) {
	model := fakeAgentModel(t, func(string) float64 { return .1 })
	hookEnv(t, model)
	common, _ := agentHome()
	repo := gitRepo(t)
	initAt(t, repo)
	root := filepath.Join(repo, ".shikii")
	os.WriteFile(filepath.Join(root, "project.json"), []byte(`{"version":1,"scope":"`+repo+`"}`), 0o600)
	os.WriteFile(filepath.Join(root, "records", "old.jsonl"), []byte("old"), 0o600)
	os.Remove(installedVersionPath(common))
	initAt(t, repo) // 新しい本体で --init をやり直した(README の手順)
	if v := projectInitVersion(root); v != 1 {
		t.Fatalf("消去が済んでいない置き場は version 1 のまま: %d", v)
	}
	v, _ := updateVersion("0.7.5")
	wiped, err := resetStoresOnFirstRun(common, v, func(path string) error { initAt(t, path); return nil })
	if err != nil || !wiped {
		t.Fatalf("その後の hook install で消える: wiped=%v err=%v", wiped, err)
	}
	if _, err := os.Stat(filepath.Join(root, "records", "old.jsonl")); !os.IsNotExist(err) {
		t.Fatal("古い記録は消える")
	}
	if v := projectInitVersion(root); v != 2 {
		t.Fatalf("消した後は version 2: %d", v)
	}
}

// 3. .DS_Store のような迷子のファイルがあっても消去を拒まない(消してよい迷子として扱う)。
func TestWipeTreatsFinderJunkAsDisposable(t *testing.T) {
	hookEnv(t, nil)
	repo := gitRepo(t)
	initAt(t, repo)
	root := filepath.Join(repo, ".shikii")
	os.WriteFile(filepath.Join(root, ".DS_Store"), []byte("x"), 0o600)
	os.WriteFile(filepath.Join(root, ".project-init-123"), []byte("x"), 0o600)
	os.WriteFile(filepath.Join(root, "project.json"), []byte(`{"version":1,"scope":"`+repo+`"}`), 0o600)
	plan := planWipe([]projectEntry{{Path: repo, InitVersion: 1}})
	if len(plan) != 1 || plan[0].Action != wipeDo {
		t.Fatalf(".DS_Store と init の残骸では拒まない: %+v", plan)
	}
	if err := wipeProjectStores([]projectEntry{{Path: repo}}, func(string) error { return nil }); err != nil {
		t.Fatalf("消せる: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".DS_Store")); !os.IsNotExist(err) {
		t.Fatal("迷子も消える")
	}
}

// 4. 共通の置き場の判定は実体で比べる(綴りが違っても同じ)。
func TestSharedPlaceComparesByIdentity(t *testing.T) {
	hookEnv(t, nil)
	home, _ := os.UserHomeDir()
	upper := strings.ToUpper(home)
	if _, err := os.Stat(upper); err != nil || upper == home {
		t.Skip("大文字小文字を区別する FS")
	}
	if !isSharedPlaceScope(upper) {
		t.Fatal("綴りが違っても HOME は共通の置き場")
	}
	if !isSharedPlaceScope(filepath.Join(home, "sub", "..")) {
		t.Fatal("HOME/sub/.. も HOME")
	}
	if isSharedPlaceScope(filepath.Join(home, "sub")) {
		t.Fatal("HOME の下のフォルダは共通の置き場ではない")
	}
}
