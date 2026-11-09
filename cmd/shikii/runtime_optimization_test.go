package main

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestPageProbeSkipsStatusAndChecksIdentity(t *testing.T) {
	hookEnv(t, nil)
	scope := t.TempDir()
	var statusCalls atomic.Int32
	h := newServer(serveOpts{storeDir: filepath.Join(scope, ".shikii"), projectScope: scope})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/status" {
			statusCalls.Add(1)
		}
		h.ServeHTTP(w, r)
	}))
	defer srv.Close()
	rec := pageRecord{PID: os.Getpid(), Scope: scope, Port: srv.Listener.Addr().(*net.TCPAddr).Port}
	if !pageUp(rec) || statusCalls.Load() != 0 {
		t.Fatalf("\u8efd\u3044\u751f\u5b58\u78ba\u8a8d\u304c\u5fc5\u8981: status=%d", statusCalls.Load())
	}
	wrong := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/page-health" {
			json.NewEncoder(w).Encode(map[string]any{"service": "shikii-page", "pid": os.Getpid() + 1, "scope": scope})
			return
		}
		w.WriteHeader(200)
	}))
	defer wrong.Close()
	rec.Port = wrong.Listener.Addr().(*net.TCPAddr).Port
	if pageUp(rec) {
		t.Fatal("\u7570\u306a\u308b\u30d7\u30ed\u30bb\u30b9\u306e\u751f\u5b58\u5fdc\u7b54\u306f\u62d2\u5426\u3059\u308b")
	}
}

func TestRegistrationDoesNotReplaceIdenticalRegularEntry(t *testing.T) {
	hookEnv(t, nil)
	scope := gitRepo(t)
	initAt(t, scope)
	common, _ := agentHome()
	path := filepath.Join(common, "projects", storeID(filepath.Join(scope, ".shikii"), false)+".json")
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = registerProject(scope); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(before, after) {
		t.Fatal("\u540c\u4e00\u5185\u5bb9\u306e\u901a\u5e38\u30d5\u30a1\u30a4\u30eb\u306f\u4ea4\u63db\u3057\u306a\u3044")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(t.TempDir(), "entry.json")
	if err = os.WriteFile(other, data, 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err = os.Symlink(other, path); err != nil {
		t.Fatal(err)
	}
	if err = registerProject(scope); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		t.Fatalf("\u540c\u4e00\u5185\u5bb9\u306e\u30ea\u30f3\u30af\u306f\u901a\u5e38\u30d5\u30a1\u30a4\u30eb\u3078\u76f4\u3059: %v", err)
	}
}

func TestMCPProjectResolutionRechecksStoreReplacement(t *testing.T) {
	hookEnv(t, nil)
	scope := gitRepo(t)
	initAt(t, scope)
	router := &mcpRouter{}
	if _, err := router.handler(scope, true); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(scope, ".shikii")
	marker, err := os.ReadFile(filepath.Join(root, "project.json"))
	if err != nil {
		t.Fatal(err)
	}
	id, err := os.ReadFile(filepath.Join(root, "records", "store-id"))
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Rename(root, root+"-previous"); err != nil {
		t.Fatal(err)
	}
	if err = checkProjectStore(root, true); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(root, "project.json"), marker, 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(root, "records", "store-id"), id, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = router.handler(scope, true); err == nil {
		t.Fatal("\u7f6e\u304d\u5834\u3092\u30b3\u30d4\u30fc\u3067\u5dee\u3057\u66ff\u3048\u305f\u5f8c\u306f\u65e2\u5b58handler\u3092\u4f7f\u308f\u306a\u3044")
	}
}

func TestPageProbeRejectsMalformedIdentifiedHealth(t *testing.T) {
	scope := t.TempDir()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/page-health" {
			w.Header().Set("X-Shikii-Page", "1")
			fmt.Fprintf(w, `{"service":"shikii-page","pid":%d,"scope":%q} trailing`, os.Getpid(), scope)
			return
		}
		w.WriteHeader(200)
	}))
	defer srv.Close()
	if pageUp(pageRecord{PID: os.Getpid(), Scope: scope, Port: srv.Listener.Addr().(*net.TCPAddr).Port}) {
		t.Fatal("\u8b58\u5225\u3057\u305f\u751f\u5b58\u5fdc\u7b54\u306e\u4e0d\u6b63JSON\u3092\u62d2\u5426\u3059\u308b")
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "http://attacker.invalid/v1/page-health", nil)
	localHostOnly(newServer(serveOpts{storeDir: filepath.Join(scope, ".shikii"), projectScope: scope})).ServeHTTP(rec, req)
	if rec.Code != 403 {
		t.Fatalf("\u751f\u5b58\u78ba\u8a8d\u3082\u65e2\u5b58Host\u5883\u754c\u306b\u5f93\u3046: %d", rec.Code)
	}
}

func TestPageProbeRejectsOversizedIdentifiedHealth(t *testing.T) {
	scope := t.TempDir()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/page-health" {
			w.Header().Set("X-Shikii-Page", "1")
			fmt.Fprintf(w, `{"service":"shikii-page","pid":%d,"scope":%q}%s trailing`, os.Getpid(), scope, strings.Repeat(" ", 5000))
			return
		}
		w.WriteHeader(200)
	}))
	defer srv.Close()
	if pageUp(pageRecord{PID: os.Getpid(), Scope: scope, Port: srv.Listener.Addr().(*net.TCPAddr).Port}) {
		t.Fatal("oversized health must not pass from a valid leading object")
	}
}
