package notebook

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestSourcePartHintBoundsProbeBytes(t *testing.T) {
	text := strings.Repeat("x", 1<<20)
	probed := 0
	end := 0
	e := fitSourcePart(text, func(n int) bool { probed += n; end = n; return n <= 4096 }, 8192)
	if e != nil || end != 4096 {
		t.Fatal(end, e)
	}
	if probed > len(text)+(128<<10) {
		t.Fatalf("\u5927\u304d\u3044\u6b8b\u308a\u672c\u6587\u3092\u7e70\u308a\u8fd4\u3057\u8a08\u6e2c: %d bytes", probed)
	}
}
func TestSourcePartHintRetainsPresentationFallback(t *testing.T) {
	text := strings.Repeat("x", 512)
	for _, test := range []struct {
		name string
		fits func(int) bool
	}{{"middle", func(n int) bool { return n >= 240 && n <= 300 }}, {"complete", func(n int) bool { return n == 512 }}, {"none", func(int) bool { return false }}} {
		t.Run(test.name, func(t *testing.T) {
			end := 0
			e := fitSourcePart(text, func(n int) bool { end = n; return test.fits(n) }, 32)
			if test.name == "none" {
				if e == nil {
					t.Fatal("\u9069\u5408\u3057\u306a\u3044\u5fdc\u7b54\u3092\u62d2\u5426\u3059\u308b")
				}
				return
			}
			if e != nil || !test.fits(end) {
				t.Fatal(end, e)
			}
		})
	}
	text = strings.Repeat("\u20ac\U0001f331", 1000)
	end := 0
	e := fitSourcePart(text, func(n int) bool {
		if !utf8.ValidString(text[:n]) {
			t.Fatal("UTF-8")
		}
		end = n
		return n <= 997
	}, 1111)
	if e != nil || end != 997 {
		t.Fatal(end, e)
	}
}
