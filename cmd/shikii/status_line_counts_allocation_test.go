//go:build !race

package main

import (
	"fmt"
	"runtime"
	"testing"

	"github.com/hayaenfj/shikii/internal/spec"
)

func TestStatusLineCountsBoundedAllocation(t *testing.T) {
	labs := statusLabelsFixture(5000, 50)
	sp := &spec.Spec{Type: spec.Noul}
	sp.Thresholds.Act = .75
	one := func() {
		c := newStatusLineCounts(labs)
		n := 0
		for key := range 50 {
			n += c.state(sp, 30, fmt.Sprint(key), "synthetic", false).N
		}
		if n != 5000 {
			t.Fatal(n)
		}
	}
	// The bytes one run allocates: a fixed number of runs, read from the runtime's count of all bytes allocated so far (it does not
	// depend on when the collector runs). testing.Benchmark did the same for B/op, but it repeats the function until a second has
	// passed, and the bytes of one run are the same every time (11.27 MB, to within a thousand bytes), so a few runs say as much.
	one() // the first run allocates what a later one does not (a map's first growth, the string table)
	const runs = 3
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	for range runs {
		one()
	}
	runtime.ReadMemStats(&after)
	if n := (after.TotalAlloc - before.TotalAlloc) / runs; n > 32<<20 {
		t.Fatalf("\u30ad\u30fc\u3054\u3068\u306b\u5168\u30e9\u30d9\u30eb\u3092\u518d\u96c6\u8a08: %d B/op", n)
	}
}
