package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// 0.9.7, after audits A to G: whether a reply that waits for the calibration lock is cut by the write timeout must not depend on how the
// handler reached the lock. The server keeps every reply in flight alive while a calibration holds or waits for the lock, so a new route,
// a wrapped handler or one run in process cannot get it wrong. This route is on no list and passes no writer.
func TestAnyReplyWaitingForACalibrationOutlivesTheWriteTimeout(t *testing.T) {
	dir := t.TempDir()
	specsDir := filepath.Join(dir, "decisions")
	if err := os.MkdirAll(specsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeSpec(t, specsDir, "true")
	s := newServerState(serveOpts{specsDir: specsDir, storeDir: filepath.Join(dir, ".shikii")})
	quickReplyCheck(t)
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer s.lockCalibration()() // a path nobody listed, with no writer
		io.WriteString(w, "done")
	})
	ts := httptest.NewUnstartedServer(h)
	ts.Config = newHTTPServer(h)
	ts.Config.WriteTimeout = 100 * time.Millisecond
	ts.Start()
	defer ts.Close()
	release := s.lockCalibration() // a background tune holds the lock
	time.AfterFunc(400*time.Millisecond, release)
	resp, err := http.Get(ts.URL)
	var body []byte
	if err == nil {
		body, err = io.ReadAll(resp.Body)
		resp.Body.Close()
	}
	if err != nil || string(body) != "done" {
		t.Fatalf("the reply waiting for the lock arrives: %q %v", body, err)
	}
}

// quickReplyCheck makes replies in flight look for an active calibration often enough for a test's short write timeout.
func quickReplyCheck(t *testing.T) {
	t.Helper()
	old := replyCheck
	replyCheck = 20 * time.Millisecond
	t.Cleanup(func() { replyCheck = old })
}
