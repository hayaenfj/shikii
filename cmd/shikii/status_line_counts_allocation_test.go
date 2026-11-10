//go:build !race

package main

import (
	"fmt"
	"testing"

	"github.com/hayaenfj/shikii/internal/spec"
)

func TestStatusLineCountsBoundedAllocation(t *testing.T) {
	labs := statusLabelsFixture(5000, 50)
	sp := &spec.Spec{Type: spec.Noul}
	sp.Thresholds.Act = .75
	measure := testing.Benchmark(func(b *testing.B) {
		for range b.N {
			c := newStatusLineCounts(labs)
			n := 0
			for key := range 50 {
				n += c.state(sp, 30, fmt.Sprint(key), "synthetic", false).N
			}
			if n != 5000 {
				b.Fatal(n)
			}
		}
	})
	if n := measure.AllocedBytesPerOp(); n > 32<<20 {
		t.Fatalf("\u30ad\u30fc\u3054\u3068\u306b\u5168\u30e9\u30d9\u30eb\u3092\u518d\u96c6\u8a08: %d B/op", n)
	}
}
