//go:build !race

package notebook

import (
	"strings"
	"testing"
)

func TestEncodedSizeAvoidsResultCopy(t *testing.T) {
	v := strings.Repeat("x", 512<<10)
	m := testing.Benchmark(func(b *testing.B) {
		for range b.N {
			if encodedSize(v) != len(v)+2 {
				b.Fatal("size")
			}
		}
	})
	if n := m.AllocedBytesPerOp(); n > 64<<10 {
		t.Fatalf("\u30b5\u30a4\u30ba\u78ba\u8a8d\u3060\u3051\u306eJSON\u5168\u6587\u3092\u8907\u88fd: %d B/op", n)
	}
}
