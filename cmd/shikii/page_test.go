package main

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// 0.8.0: serve は --port 0 で空いているポートを取り、ポートと pid を ~/.shikii/agent/pages/<id>.json に書く。
// livePage は立っている画面の URL を返し、死んだ pid や応えない口は「立っていない」。
func TestServePicksAFreePortAndRecordsIt(t *testing.T) {
	hookEnv(t, nil)
	common, _ := agentHome()
	repo := gitRepo(t)
	initAt(t, repo)
	ln, err := listenPage(common, repo, false, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port
	rec, ok := readPageRecord(common, repo)
	if !ok || rec.Port != port || rec.PID != os.Getpid() || rec.Scope != repo {
		t.Fatalf("空いているポートと pid を記録する: %+v (port %d)", rec, port)
	}
	if livePage(repo) != "" {
		t.Fatal("口が応えるまでは立っていない")
	}
	if len(opened) != 1 || !strings.Contains(opened[0], strconv.Itoa(port)) {
		t.Fatalf("hook が起こした画面(port 0)は serve 自身がブラウザで開く: %v", opened)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/status", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	go http.Serve(ln, mux)
	if url := livePage(repo); url != "http://127.0.0.1:"+strconv.Itoa(port) {
		t.Fatalf("応えれば立っている: %q", url)
	}
	if url := projectPageURL(filepath.Join(repo, ".shikii")); !strings.HasSuffix(url, "/") || !strings.Contains(url, strconv.Itoa(port)) {
		t.Fatalf("プロジェクトの画面の URL: %q", url)
	}
	// 死んだ pid の印は立っていない
	if err := writePageRecord(common, repo, port, 1<<22); err != nil {
		t.Fatal(err)
	}
	if livePage(repo) != "" {
		t.Fatal("起こした子が死んでいれば立っていない")
	}
	out := capture(t, func() error { return cmdStatus([]string{"--store", filepath.Join(repo, ".shikii")}) })
	if !strings.Contains(out, "page: not running") {
		t.Fatalf("status は画面の状態を出す:\n%s", out)
	}
	// 手で起こした serve(固定ポート)は印を書かず、ブラウザも開かない(hook の画面の印を上書きしない)
	opened = nil
	ln2, err := listenPage(common, repo, false, 0)
	if err != nil {
		t.Fatal(err)
	}
	ln2.Close()
	rec2, _ := readPageRecord(common, repo)
	ln3, err := listenPage(common, repo, false, 18095)
	if err != nil {
		t.Skip("port 18095 busy")
	}
	defer ln3.Close()
	if rec3, _ := readPageRecord(common, repo); rec3.Port != rec2.Port || len(opened) != 1 {
		t.Fatalf("固定ポートの serve は印を書かずブラウザも開かない: %+v opened=%v", rec3, opened)
	}
}

// 最初の依頼の hook は、このプロジェクトの画面が立っていなければ `serve --store <root> --port 0` を 1 つだけ起こす(待たない。ブラウザは serve が開く)。
// 立っていれば起こさず、場所をセッションに 1 回伝える。別のプロジェクトの画面は数えない。
func TestFirstRequestStartsOneProjectPageAndOpensTheBrowser(t *testing.T) {
	model := fakeAgentModel(t, func(string) float64 { return .1 })
	hookEnv(t, model)
	common, _ := agentHome()
	repo := gitRepo(t)
	initAt(t, repo)
	root := filepath.Join(repo, ".shikii")
	// 起こすと、serve が印を書いて応える(のを真似る)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }))
	defer srv.Close()
	port := srv.Listener.Addr().(*net.TCPAddr).Port
	hookSpawn = func(agent, logName string, args ...string) int {
		spawned = append(spawned, strings.Join(args, " "))
		writePageRecord(common, repo, port, os.Getpid())
		return os.Getpid()
	}
	out := runHook(t, "user-prompt-submit", map[string]any{"session_id": "p1", "cwd": repo, "prompt": "README を直して"})
	want := "serve --store " + root + " --port 0 --local "
	if len(spawned) != 1 || !strings.HasPrefix(spawned[0], want) {
		t.Fatalf("画面が無ければ空いているポートで起こす: %v", spawned)
	}
	url := "http://127.0.0.1:" + strconv.Itoa(port)
	if len(opened) != 0 {
		t.Fatalf("hook はブラウザを開かない(serve が開く): %v", opened)
	}
	if !strings.Contains(out, "画面を起こしています") {
		t.Fatalf("起こした時はそう伝える:\n%s", out)
	}
	// 立っていれば(別のエージェントでも)起こさず、場所をセッションに 1 回伝える。起こす権利の guard を消しても(startOnce が許しても)起こさない
	spawned, opened = nil, nil
	os.Remove(filepath.Join(common, "starting-page-"+projectID(repo)))
	out = runHook(t, "user-prompt-submit", map[string]any{"session_id": "p2", "cwd": repo, "prompt": "次は画面の色を直して"})
	if len(spawned) != 0 || len(opened) != 0 || strings.Contains(out, "画面を起こし") || !strings.Contains(out, "画面: "+url+"/") {
		t.Fatalf("立っていれば二つ目を起こさず、場所を伝える: spawned=%v opened=%v\n%s", spawned, opened, out)
	}
	if out = runHook(t, "user-prompt-submit", map[string]any{"session_id": "p2", "cwd": repo, "prompt": "さらに色を直して"}); strings.Contains(out, "画面: ") {
		t.Fatalf("同じセッションでは二度言わない:\n%s", out)
	}
	// 別のプロジェクトは自分の画面を持つ
	other := gitRepo(t)
	initAt(t, other)
	hookSpawn = recordSpawn
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "p3", "cwd": other, "prompt": "README を直して"})
	if len(spawned) != 1 || !strings.HasPrefix(spawned[0], "serve --store "+filepath.Join(other, ".shikii")+" --port 0 --local ") {
		t.Fatalf("別のプロジェクトは別の画面: %v", spawned)
	}
}

// 画面を止めた(子が死んだ)直後でも、pageStartGap(30 秒)を過ぎれば起こし直せる。モデルの 10 分の間隔は使わない。
func TestPageRestartsSoonAfterItDied(t *testing.T) {
	model := fakeAgentModel(t, func(string) float64 { return .1 })
	hookEnv(t, model)
	common, _ := agentHome()
	repo := gitRepo(t)
	initAt(t, repo)
	id := projectID(repo)
	// 1 分前に起こした子はもう死んでいる
	os.WriteFile(filepath.Join(common, "starting-page-"+id), []byte(fmt.Sprintf("%d %s\n", 1<<22, time.Now().Add(-time.Minute).Format(time.RFC3339))), 0o600)
	spawned = nil
	ensureProjectPage(repo)
	if len(spawned) != 1 {
		t.Fatalf("死んだ画面は 30 秒を過ぎれば起こし直す: %v", spawned)
	}
	// 10 秒前なら(失敗し続ける serve を連打しないため)まだ起こさない
	os.WriteFile(filepath.Join(common, "starting-page-"+id), []byte(fmt.Sprintf("%d %s\n", 1<<22, time.Now().Add(-10*time.Second).Format(time.RFC3339))), 0o600)
	spawned = nil
	ensureProjectPage(repo)
	if len(spawned) != 0 {
		t.Fatalf("30 秒の中では起こし直さない: %v", spawned)
	}
}
