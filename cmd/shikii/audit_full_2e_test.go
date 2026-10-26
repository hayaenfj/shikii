package main

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"
)

// E2: once the lock is released, a reply in flight is cut by the write timeout again (here 100ms) - not kept for longResponseLimit.
func TestAuditE2DeadlineReturnsAfterRelease(t *testing.T) {
	quickReplyCheck(t)
	s := newServerState(serveOpts{storeDir: t.TempDir(), specsDir: t.TempDir()})
	h := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(700 * time.Millisecond) // long work that does not use the lock
		io.WriteString(w, "done")
	})
	ts := httptest.NewUnstartedServer(h)
	ts.Config = newHTTPServer(h)
	ts.Config.WriteTimeout = 100 * time.Millisecond
	ts.Start()
	defer ts.Close()
	release := s.lockCalibration() // a short calibration elsewhere, over by 150ms
	time.AfterFunc(150*time.Millisecond, release)
	resp, err := http.Get(ts.URL)
	var body []byte
	if err == nil {
		body, err = io.ReadAll(resp.Body)
		resp.Body.Close()
	}
	if err == nil && string(body) == "done" {
		t.Fatal("E2: the lock was free for 550ms (5x the write timeout), yet the reply was not cut: the 45-minute deadline set during the calibration stayed")
	}
}

// eaf2c9b: a slow reader must not hold a connection past the write timeout. While a calibration is active, it is held without bound.
func TestAuditSlowReaderHeldDuringCalibration(t *testing.T) {
	quickReplyCheck(t)
	s := newServerState(serveOpts{storeDir: t.TempDir(), specsDir: t.TempDir()})
	var finished atomic.Int64 // 1: write failed (cut), 2: write succeeded
	big := bytes.Repeat([]byte("x"), 64<<20)
	h := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if _, err := w.Write(big); err != nil {
			finished.Store(1)
			return
		}
		finished.Store(2)
	})
	ts := httptest.NewUnstartedServer(h)
	ts.Config = newHTTPServer(h)
	ts.Config.WriteTimeout = 100 * time.Millisecond
	ts.Start()
	defer ts.Close()
	release := func() {}
	if os.Getenv("AUDIT_NO_CALIB") == "" {
		release = s.lockCalibration() // a background tune
	}
	defer func() {
		if release != nil {
			release()
		}
	}()
	c, err := net.Dial("tcp", ts.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	fmt.Fprintf(c, "GET / HTTP/1.1\r\nHost: 127.0.0.1\r\n\r\n") // and never read
	time.Sleep(1 * time.Second)
	during := finished.Load()
	release()
	release = nil
	time.Sleep(1 * time.Second)
	after := finished.Load()
	if during == 0 || after == 0 {
		t.Fatalf("a client that reads nothing held the reply: 10x the write timeout during the calibration (cut=%v), and 10x more after it ended (cut=%v)", during == 1, after == 1)
	}
}
