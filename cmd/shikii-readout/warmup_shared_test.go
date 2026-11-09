package main

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

type sharedWarmTransport struct {
	mu        sync.Mutex
	calls     int
	started   chan struct{}
	release   chan struct{}
	cancelled chan struct{}
	status    int
}

func (f *sharedWarmTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	f.mu.Lock()
	f.calls++
	f.mu.Unlock()
	f.started <- struct{}{}
	select {
	case <-r.Context().Done():
		f.cancelled <- struct{}{}
		return nil, r.Context().Err()
	case <-f.release:
	}
	code := f.status
	if code == 0 {
		code = 200
	}
	return &http.Response{StatusCode: code, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"timings":{"prompt_n":100,"cache_n":0}}`)), Request: r}, nil
}

func sharedWarmFixture() (*server, *sharedWarmTransport) {
	f := &sharedWarmTransport{started: make(chan struct{}, 10), release: make(chan struct{}), cancelled: make(chan struct{}, 10)}
	return &server{cfg: config{Upstream: "http://synthetic.invalid", Timeout: time.Second}, http: &http.Client{Transport: f}}, f
}

func waitWarmWaiters(t *testing.T, s *server, f *sharedWarmTransport, n int) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		f.mu.Lock()
		calls := f.calls
		f.mu.Unlock()
		if calls > 1 {
			t.Fatalf("same prefix issued %d warm-ups", calls)
		}
		s.mu.Lock()
		flight := s.warmups["prefix"]
		ready := flight != nil && flight.waiters == n
		s.mu.Unlock()
		if ready {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("waiters did not share the in-flight prefix")
}

func TestPrefixWarmupSharesWorkAndKeepsIndividualCancellation(t *testing.T) {
	s, f := sharedWarmFixture()
	ctx, cancel := context.WithCancel(context.Background())
	first, second := make(chan error, 1), make(chan error, 1)
	go func() { first <- s.warmPrefixFor(ctx, "prefix<state>first", "state-last") }()
	<-f.started
	go func() { second <- s.warmPrefixFor(context.Background(), "prefix<state>second", "state-last") }()
	waitWarmWaiters(t, s, f, 2)
	cancel()
	if err := <-first; !errors.Is(err, context.Canceled) {
		t.Fatalf("first cancellation: %v", err)
	}
	select {
	case <-f.cancelled:
		t.Fatal("one cancellation stopped another waiter")
	default:
	}
	close(f.release)
	if err := <-second; err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	calls := f.calls
	f.mu.Unlock()
	if calls != 1 {
		t.Fatalf("warm-up calls=%d", calls)
	}
	if err := s.warmPrefixFor(context.Background(), "prefix<state>third", "state-last"); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	calls = f.calls
	f.mu.Unlock()
	if calls != 1 {
		t.Fatal("the warmed prefix was not reused")
	}
}

func TestPrefixWarmupLastCancellationAndFailureAllowRetry(t *testing.T) {
	s, f := sharedWarmFixture()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.warmPrefixFor(ctx, "prefix<state>one", "state-last") }()
	<-f.started
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	select {
	case <-f.cancelled:
	case <-time.After(time.Second):
		t.Fatal("last waiter did not cancel upstream")
	}
	f.status = 500
	close(f.release)
	if err := s.warmPrefixFor(context.Background(), "prefix<state>two", "state-last"); err == nil {
		t.Fatal("failed warm-up accepted")
	}
	f.status = 200
	if err := s.warmPrefixFor(context.Background(), "prefix<state>three", "state-last"); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	calls := f.calls
	f.mu.Unlock()
	if calls != 3 {
		t.Fatalf("cancel, failure and retry need separate requests: %d", calls)
	}
}

func TestPrefixWarmupDoesNotShareDifferentPrefixes(t *testing.T) {
	s, f := sharedWarmFixture()
	done := make(chan error, 2)
	go func() { done <- s.warmPrefixFor(context.Background(), "first<state>one", "state-last") }()
	<-f.started
	go func() { done <- s.warmPrefixFor(context.Background(), "second<state>two", "state-last") }()
	select {
	case <-f.started:
	case <-time.After(time.Second):
		t.Fatal("different prefixes were serialized")
	}
	close(f.release)
	for range 2 {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
}
