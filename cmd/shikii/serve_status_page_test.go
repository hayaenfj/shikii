package main

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// The page's own /v1/status says where the page is. Serve writes its record under pageScope (links resolved, the spelling the hook
// uses), so status must read it under the same key whatever path named the store: here the store is reached through a link.
func TestStatusNamesItsOwnPageWhenTheStoreIsReachedThroughALink(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	real := t.TempDir()
	specsDir := filepath.Join(real, "decisions")
	if err := os.MkdirAll(specsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeSpec(t, specsDir, "false")
	link := filepath.Join(t.TempDir(), "via-link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	storeDir := filepath.Join(link, ".shikii")
	if err := os.MkdirAll(storeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	common, err := agentHome()
	if err != nil {
		t.Fatal(err)
	}
	absStore, err := filepath.Abs(storeDir)
	if err != nil {
		t.Fatal(err)
	}
	// the writing side, as cmdServe does it: the scope is pageScope(absStore), and listenPage writes the record on a free port
	ln, err := listenPage(common, pageScope(absStore), false, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	port := ln.Addr().(*net.TCPAddr).Port

	h := newServer(serveOpts{specsDir: filepath.Join(link, "decisions"), storeDir: storeDir})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/status", nil))
	if rec.Code != 200 {
		t.Fatalf("status: %d %s", rec.Code, rec.Body.String())
	}
	var stat struct {
		Page *struct {
			URL  string
			Port int
			PID  int
		}
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &stat); err != nil {
		t.Fatal(err)
	}
	if stat.Page == nil {
		t.Fatalf("status of the page's own process names its page (record written under %q, store given as %q): page is null", pageScope(absStore), storeDir)
	}
	if stat.Page.Port != port || stat.Page.PID != os.Getpid() {
		t.Fatalf("status names the recorded port and pid: %+v, want port %d pid %d", *stat.Page, port, os.Getpid())
	}
}
