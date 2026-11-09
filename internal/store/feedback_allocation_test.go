//go:build !race

package store

import (
	"runtime"
	"testing"
)

// Race instrumentation has a different allocation budget; semantic checks run in both builds.
func TestFeedbackWindowWorkingSet(t *testing.T) {
	s, scope := feedbackWindowFixture(t, 300)
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	for range 5 {
		if _, err := s.RecentFeedback(scope); err != nil {
			t.Fatal(err)
		}
	}
	runtime.ReadMemStats(&after)
	if n := (after.TotalAlloc - before.TotalAlloc) / 5; n > 245<<10 {
		t.Fatalf("a bounded history must avoid growing and shifting its whole window, got %d bytes per read", n)
	}
}
