//go:build !race

package notebook

import (
	"strings"
	"testing"
)

func TestEncodedSizeAvoidsResultCopy(t *testing.T) {
	v := strings.Repeat("x", 512<<10)
	// A copy of the encoded text is over 512 KiB a call; the budget is 64 KiB, and 50 calls keep what the runtime allocates by itself
	// well under it.
	n := bytesPerOp(50, func() {
		if encodedSize(v) != len(v)+2 {
			t.Fatal("size")
		}
	})
	if n > 64<<10 {
		t.Fatalf("\u30b5\u30a4\u30ba\u78ba\u8a8d\u3060\u3051\u306eJSON\u5168\u6587\u3092\u8907\u88fd: %d B/op", n)
	}
}
