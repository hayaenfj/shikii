package systemone

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// "localhost:8086" parses as a URL whose scheme is "localhost", so a request can be built from it, but no
// request to it can ever be sent (only http and https are). WaitReady must say so at once instead of polling
// until the caller's deadline (15 minutes in serve). No server is started: nothing can be sent to these.
func TestWaitReadyReturnsAnErrorForABaseURLThatIsNotHTTP(t *testing.T) {
	for _, raw := range []string{"localhost:8086", "localhost:8086/", "ftp://127.0.0.1:8086", "http://", "http:///v1"} {
		t.Run(fmt.Sprintf("%q", raw), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			polls := 0
			err := WaitReady(ctx, raw, time.Millisecond, func(string, ...any) { polls++ })
			if err == nil {
				t.Fatalf("WaitReady(%q) reported the model as ready", raw)
			}
			if errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("WaitReady(%q) polled until the deadline for a URL that can never be requested: %v", raw, err)
			}
			if polls != 0 {
				t.Fatalf("WaitReady(%q) started waiting (%d log lines) before refusing the URL", raw, polls)
			}
			if !strings.Contains(err.Error(), "http://") {
				t.Fatalf("WaitReady(%q): the error must show how to write the URL: %v", raw, err)
			}
		})
	}
}

// Guard: an http URL is still polled until it answers, with or without a trailing slash or a path, and with
// credentials (which stay out of the error and the log).
func TestWaitReadyStillPollsHTTPBaseURLs(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/health") {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	t.Cleanup(srv.Close)
	host := strings.TrimPrefix(srv.URL, "http://")
	for _, raw := range []string{srv.URL, srv.URL + "/", srv.URL + "/proxy", "HTTP://" + host, "http://user:secret@" + host} {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		err := WaitReady(ctx, raw, 10*time.Millisecond, nil)
		cancel()
		if err != nil {
			t.Fatalf("WaitReady(%q) must become ready: %v", raw, err)
		}
	}
}
