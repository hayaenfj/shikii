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

// モデルの口が立つのを shikii 自身が待つ(curl / wget などの外部コマンドに頼らない)。
// /health が 200 を返すまで繰り返し、締め切りで諦める。進み具合は logf に出る。
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
	// 締め切り: 立たないまま ctx が切れたら誤り(何を待っていたかが分かる文で)
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
