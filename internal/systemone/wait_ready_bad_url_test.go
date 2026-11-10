package systemone

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

// A base URL that cannot be turned into a request (no scheme before an IP and port, a control character)
// must come back as an error at once. No server is started and nothing is sent: the URL never parses.
func TestWaitReadyReturnsAnErrorForAURLThatCannotBeRequested(t *testing.T) {
	for _, raw := range []string{"127.0.0.1:8086", "http://127.0.0.1:8086\n"} {
		t.Run(fmt.Sprintf("%q", raw), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			var err error
			panicked := func() (v any) {
				defer func() { v = recover() }()
				err = WaitReady(ctx, raw, time.Hour, nil)
				return nil
			}()
			if panicked != nil {
				t.Fatalf("WaitReady(%q) panicked instead of returning an error: %v", raw, panicked)
			}
			if err == nil {
				t.Fatalf("WaitReady(%q) reported the model as ready", raw)
			}
			if errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("WaitReady(%q) waited for a URL that can never be requested: %v", raw, err)
			}
		})
	}
}
