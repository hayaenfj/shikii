package main

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"sync/atomic"
	"testing"
	"time"
)

// 0.9.7 full audit 2-8 (eaf2c9b: a slow connection must not hold resources): a request body sent one byte at a time was never cut by
// readout. Reading the request is bounded; here the bound is shortened to see that a dribbled body is cut by it.
func TestAuditReadoutSlowBodyHeld(t *testing.T) {
	var done atomic.Bool
	srv := newReadoutServer("", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.ReadAll(io.LimitReader(r.Body, 4<<20))
		done.Store(true)
	}))
	if srv.ReadTimeout <= 0 || srv.ReadTimeout > time.Minute {
		t.Fatalf("reading a request is bounded: %v", srv.ReadTimeout)
	}
	srv.ReadTimeout = 500 * time.Millisecond
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	go srv.Serve(ln)
	defer srv.Close()
	c, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	fmt.Fprintf(c, "POST /v1/systemone HTTP/1.1\r\nHost: x\r\nContent-Length: 1000\r\n\r\n")
	for i := 0; i < 30; i++ { // 3 seconds, one byte every 100ms
		if _, err := c.Write([]byte("x")); err != nil {
			return // cut: fine
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !done.Load() {
		t.Fatal("a body dribbled for 3s, past the read bound, still holds the handler")
	}
}
