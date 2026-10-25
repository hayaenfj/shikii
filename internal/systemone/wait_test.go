package systemone

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// shikii itself waits for the model endpoint to come up (without relying on external commands such as curl / wget).
// It retries until /health returns 200 and gives up at the deadline. Progress goes to logf.
func TestWaitReadyPollsUntilHealthy(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			http.NotFound(w, r)
			return
		}
		if atomic.AddInt32(&calls, 1) < 3 {
			http.Error(w, "loading", 503)
			return
		}
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	t.Cleanup(srv.Close)
	var logs []string
	logf := func(format string, a ...any) { logs = append(logs, format) }
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := WaitReady(ctx, srv.URL, 10*time.Millisecond, logf); err != nil {
		t.Fatalf("must become ready on the 3rd poll: %v", err)
	}
	if calls != 3 {
		t.Fatalf("polled %d times, want 3", calls)
	}
	// Deadline: if ctx ends while the endpoint is still down, it is an error (with a message saying what it was waiting for)
	dead := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "no", 503) }))
	t.Cleanup(dead.Close)
	ctx2, cancel2 := context.WithTimeout(context.Background(), 60*time.Millisecond)
	defer cancel2()
	err := WaitReady(ctx2, dead.URL, 10*time.Millisecond, logf)
	if err == nil || !strings.Contains(err.Error(), dead.URL) {
		t.Fatalf("a model that never comes up must be reported with its URL: %v", err)
	}
	if len(logs) == 0 {
		t.Fatalf("waiting must say what it waits for")
	}
}
