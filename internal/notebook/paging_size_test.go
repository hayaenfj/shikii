package notebook

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
)

func TestEncodedSizeMatchesMarshal(t *testing.T) {
	for _, v := range []any{nil, []string(nil), []string{}, map[string]any{}, map[string]any{"text": "\"\n<>&\u2028\U0001f331", "number": 123.5}, strings.Repeat("large\n", 10000), math.NaN(), json.RawMessage(`{"bad"`)} {
		raw, _ := json.Marshal(v)
		if n := encodedSize(v); n != len(raw) {
			t.Fatalf("\u5fdc\u7b54\u91cf\u306e\u8a08\u6e2c\u304c\u7570\u306a\u308b: %d != %d", n, len(raw))
		}
	}
}
