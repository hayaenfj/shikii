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

// 0.8.0: with --port 0, serve takes a free port and writes the port and pid to ~/.shikii/agent/pages/<id>.json.
// livePage returns the URL of the page that is up; a dead pid or an endpoint that does not answer means "not up".
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
		t.Fatalf("records the free port and pid: %+v (port %d)", rec, port)
	}
	if livePage(repo) != "" {
		t.Fatal("not up until the endpoint answers")
	}
	if len(opened) != 1 || !strings.Contains(opened[0], strconv.Itoa(port)) {
		t.Fatalf("serve itself opens the page started by the hook (port 0) in the browser: %v", opened)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/status", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	go http.Serve(ln, mux)
	if url := livePage(repo); url != "http://127.0.0.1:"+strconv.Itoa(port) {
		t.Fatalf("up once it answers: %q", url)
	}
	if url := projectPageURL(filepath.Join(repo, ".shikii")); !strings.HasSuffix(url, "/") || !strings.Contains(url, strconv.Itoa(port)) {
		t.Fatalf("the URL of the project's page: %q", url)
	}
	// A mark with a dead pid is not up
	if err := writePageRecord(common, repo, port, 1<<22); err != nil {
		t.Fatal(err)
	}
	if livePage(repo) != "" {
		t.Fatal("not up if the started child is dead")
	}
	out := capture(t, func() error { return cmdStatus([]string{"--store", filepath.Join(repo, ".shikii")}) })
	if !strings.Contains(out, "page: not running") {
		t.Fatalf("status shows the page's state:\n%s", out)
	}
	// A serve started by hand (fixed port) writes no mark and opens no browser (so it does not overwrite the hook page's mark)
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
		t.Fatalf("a fixed-port serve writes no mark and opens no browser: %+v opened=%v", rec3, opened)
	}
}

// The first request's hook starts exactly one `serve --store <root> --port 0` if this project's page is not up (it does not wait; serve opens the browser).
// If it is up, it does not start one and tells the location once per session. Other projects' pages are not counted.
func TestFirstRequestStartsOneProjectPageAndOpensTheBrowser(t *testing.T) {
	model := fakeAgentModel(t, func(string) float64 { return .1 })
	hookEnv(t, model)
	common, _ := agentHome()
	repo := gitRepo(t)
	initAt(t, repo)
	root := filepath.Join(repo, ".shikii")
	// When started, serve writes the mark and answers (simulated)
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
		t.Fatalf("starts on a free port if there is no page: %v", spawned)
	}
	url := "http://127.0.0.1:" + strconv.Itoa(port)
	if len(opened) != 0 {
		t.Fatalf("the hook does not open the browser (serve does): %v", opened)
	}
	if strings.Contains(out, "このプロジェクトの画面") || strings.Contains(out, "画面を起こして") || strings.Contains(out, url) {
		t.Fatalf("when it starts the page, no page notice is shown (the hook stays quiet while there is no line):\n%s", out)
	}
	// If it is up (even from another agent), do not start one; tell the location once per session. Do not start even if the start-right guard is removed (even if startOnce allows it)
	spawned, opened = nil, nil
	os.Remove(filepath.Join(common, "starting-page-"+projectID(repo)))
	out = runHook(t, "user-prompt-submit", map[string]any{"session_id": "p2", "cwd": repo, "prompt": "次は画面の色を直して"})
	if len(spawned) != 0 || len(opened) != 0 || strings.Contains(out, "画面を起こし") || !strings.Contains(out, "画面: "+url+"/") {
		t.Fatalf("if up, do not start a second one, and tell the location: spawned=%v opened=%v\n%s", spawned, opened, out)
	}
	if out = runHook(t, "user-prompt-submit", map[string]any{"session_id": "p2", "cwd": repo, "prompt": "さらに色を直して"}); strings.Contains(out, "画面: ") {
		t.Fatalf("not said twice in the same session:\n%s", out)
	}
	// Another project has its own page
	other := gitRepo(t)
	initAt(t, other)
	hookSpawn = recordSpawn
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "p3", "cwd": other, "prompt": "README を直して"})
	if len(spawned) != 1 || !strings.HasPrefix(spawned[0], "serve --store "+filepath.Join(other, ".shikii")+" --port 0 --local ") {
		t.Fatalf("another project gets another page: %v", spawned)
	}
}

// Even right after the page stopped (the child died), it can be started again once pageStartGap (30 seconds) has passed. The model's 10-minute interval is not used.
func TestPageRestartsSoonAfterItDied(t *testing.T) {
	model := fakeAgentModel(t, func(string) float64 { return .1 })
	hookEnv(t, model)
	common, _ := agentHome()
	repo := gitRepo(t)
	initAt(t, repo)
	id := projectID(repo)
	// The child started a minute ago is already dead
	os.WriteFile(filepath.Join(common, "starting-page-"+id), []byte(fmt.Sprintf("%d %s\n", 1<<22, time.Now().Add(-time.Minute).Format(time.RFC3339))), 0o600)
	spawned = nil
	ensureProjectPage(repo)
	if len(spawned) != 1 {
		t.Fatalf("a dead page is started again after 30 seconds: %v", spawned)
	}
	// 10 seconds ago: not yet (so a serve that keeps failing is not hammered)
	os.WriteFile(filepath.Join(common, "starting-page-"+id), []byte(fmt.Sprintf("%d %s\n", 1<<22, time.Now().Add(-10*time.Second).Format(time.RFC3339))), 0o600)
	spawned = nil
	ensureProjectPage(repo)
	if len(spawned) != 0 {
		t.Fatalf("not started again within 30 seconds: %v", spawned)
	}
}

// Audit R5-3: look at the response code (503 is not up), hub writes no mark, shikii page starts and waits, and the second time is up.
func TestPageProbeHubAndCommand(t *testing.T) {
	model := fakeAgentModel(t, func(string) float64 { return .1 })
	hookEnv(t, model)
	common, _ := agentHome()
	repo := gitRepo(t)
	initAt(t, repo)
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(503) }))
	defer bad.Close()
	writePageRecord(common, repo, bad.Listener.Addr().(*net.TCPAddr).Port, os.Getpid())
	if livePage(repo) != "" {
		t.Fatal("an endpoint answering 503 is not up")
	}
	os.Remove(pagePath(common, repo))
	ln, err := listenPage(common, repo, true, 0)
	if err != nil {
		t.Fatal(err)
	}
	ln.Close()
	if _, ok := readPageRecord(common, repo); ok {
		t.Fatal("the shared entry point (hub) writes no mark")
	}
	// shikii page: start (the fake spawn writes the mark) and wait; the second time is up
	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }))
	defer good.Close()
	port := good.Listener.Addr().(*net.TCPAddr).Port
	hookSpawn = func(agent, logName string, args ...string) int {
		spawned = append(spawned, strings.Join(args, " "))
		writePageRecord(common, repo, port, os.Getpid())
		return os.Getpid()
	}
	spawned = nil
	t.Chdir(repo)
	out := capture(t, func() error { return cmdPage(nil) })
	if len(spawned) != 1 || !strings.Contains(out, "started now") || !strings.Contains(out, strconv.Itoa(port)) {
		t.Fatalf("page starts and waits if not up: spawned=%v\n%s", spawned, out)
	}
	if out = capture(t, func() error { return cmdPage(nil) }); len(spawned) != 1 || !strings.Contains(out, ", up)") {
		t.Fatalf("the second time is up and does not start: spawned=%v\n%s", spawned, out)
	}
}
