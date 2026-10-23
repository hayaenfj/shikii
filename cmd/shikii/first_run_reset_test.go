package main

import (
	"os"
	"path/filepath"
	"testing"
)

// 0.6.0 からの update は古い本体が実行し、最後に新しい本体の `hook install --yes` を呼ぶ。update の中の reset は古い本体には無いので、
// 0.7.0 を跨ぐ消去は新しい本体の最初の hook install が共通側の印(~/.shikii/agent/version)で判断する:
// 印が無い(0.6.0 以前)か 0.7.0 より前なら各プロジェクトの .shikii/ の中を消して初期化し直し、印を今の版にする。二度目は消さない。
func TestFirstHookInstallAfter070WipesRegisteredProjectsOnce(t *testing.T) {
	model := fakeAgentModel(t, func(string) float64 { return .1 })
	hookEnv(t, model)
	repo := gitRepo(t)
	t.Chdir(repo)
	capture(t, func() error { return cmdInit(nil) })
	common, _ := agentHome()
	if len(registeredProjects(common)) != 2 { // hookEnv が初期化した 1 件 + この repo
		t.Fatal("init でプロジェクトが登録される")
	}
	// hookEnv の hook install が印を書いているので、0.6.0 以前の状態(印なし、project.json は version 1)に戻す
	if err := os.Remove(installedVersionPath(common)); err != nil {
		t.Fatal("新しい本体の hook install は印を書く: ", err)
	}
	for _, p := range registeredProjects(common) {
		os.WriteFile(filepath.Join(p.Path, ".shikii", "project.json"), []byte(`{"version":1,"scope":"`+p.Path+`"}`), 0o600)
	}
	store := filepath.Join(repo, ".shikii")
	leftover := filepath.Join(store, "records", "prompt-kind.jsonl")
	write := func(path, body string) {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(leftover, "old")
	var reinit []string
	run := func(to string) bool {
		v, _ := updateVersion(to)
		var wiped bool
		capture(t, func() error {
			var err error
			wiped, err = resetStoresOnFirstRun(common, v, func(path string) error {
				reinit = append(reinit, path)
				prev, _ := os.Getwd()
				if err := os.Chdir(path); err != nil {
					return err
				}
				defer os.Chdir(prev)
				return cmdInit(nil)
			})
			return err
		})
		return wiped
	}

	// 印が無い(0.6.0 以前から) → 消して初期化し直し、印は 0.7.0
	if !run("0.7.0") {
		t.Fatal("印が無ければ 0.6.0 以前からの更新として消す")
	}
	if _, err := os.Stat(leftover); !os.IsNotExist(err) {
		t.Fatal("古い記録は消える")
	}
	if _, err := os.Stat(filepath.Join(store, "project.json")); err != nil {
		t.Fatal("消した後に初期化し直す: ", err)
	}
	if b, _ := os.ReadFile(installedVersionPath(common)); string(b) != "0.7.0\n" {
		t.Fatalf("印を今の版にする: %q", b)
	}
	if len(reinit) != 2 || (reinit[0] != repo && reinit[1] != repo) {
		t.Fatalf("登録済みのプロジェクトを一度ずつ初期化し直す: %v", reinit)
	}

	// 同じ版でもう一度(hook install を手で流した)→ 消さない
	write(leftover, "new")
	if run("0.7.0") {
		t.Fatal("印が今の版なら消さない")
	}
	if b, _ := os.ReadFile(leftover); string(b) != "new" {
		t.Fatal("二度目は記録を残す")
	}

	// 0.7.0 → 0.7.1 は跨がない → 消さず、印だけ進む
	if run("0.7.1") {
		t.Fatal("0.7.0 を跨がない更新では消さない")
	}
	if b, _ := os.ReadFile(installedVersionPath(common)); string(b) != "0.7.1\n" {
		t.Fatalf("印は新しい版に進む: %q", b)
	}
	if b, _ := os.ReadFile(leftover); string(b) != "new" {
		t.Fatal("跨がない更新は記録を残す")
	}

	// 印が 0.6.0(将来、古い本体が印を書いていた場合)→ 0.7.3 以前の印(version 1)のプロジェクトは消す
	for _, p := range registeredProjects(common) {
		write(filepath.Join(p.Path, ".shikii", "project.json"), `{"version":1,"scope":"`+p.Path+`"}`)
	}
	write(installedVersionPath(common), "0.6.0\n")
	if !run("0.7.0") {
		t.Fatal("印が 0.7.0 より前なら消す")
	}
	if _, err := os.Stat(leftover); !os.IsNotExist(err) {
		t.Fatal("印が古ければ記録は消える")
	}
}
