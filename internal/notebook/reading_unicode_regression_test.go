package notebook

import "testing"

func TestReadingUnicodeEvidenceIdentity(t *testing.T) {
	a, e := readingQualityIdentity("\u5408\u6210\u306e\u30ac\u30fc\u30c9\u6761\u4ef6\u3092\u78ba\u8a8d\u3057\u305f\u3002", ReadingDetails{})
	if e != nil {
		t.Fatal(e)
	}
	b, e := readingQualityIdentity("\u5408\u6210\u306e\u30ab\u3099\u30fc\u30c8\u3099\u6761\u4ef6\u3092\u78ba\u8a8d\u3057\u305f\u3002", ReadingDetails{})
	if e != nil {
		t.Fatal(e)
	}
	t.Logf("\u6b63\u898f\u7b49\u4fa1\u306e\u7d50\u679c\u304c\u540c\u4e00identity=%v", a == b)
	if a != b {
		t.Fatal("\u6b63\u898f\u7b49\u4fa1\u306e\u5225\u8868\u73fe\u304c\u5225\u306e\u5b9f\u884c\u7d50\u679c\u3068\u306a\u308b")
	}
}
