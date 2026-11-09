//go:build !race

package main

import (
	"runtime"
	"testing"
)

// Race instrumentation has a different allocation budget; semantic checks run in both builds.
func TestReflectReadBoundsDuplicateHistoryAllocation(t *testing.T) {
	book, _ := reflectionHistoryFixture(t, 20000)
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	v, err := reflectQueryBook(book.ReadSnapshot(), reflectHeadsMax)
	if err != nil {
		t.Fatal(err)
	}
	runtime.ReadMemStats(&after)
	if len(v.Sources) != reflectRawSources+reflectHeadsMax || v.MorePending != 256-len(v.Sources) {
		t.Fatal(len(v.Sources), v.MorePending)
	}
	if n := after.TotalAlloc - before.TotalAlloc; n > 20<<20 {
		t.Fatalf("one reflection must share the bounded history instead of decoding it repeatedly: %d bytes", n)
	}
}
