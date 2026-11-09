package notebook

import (
	"reflect"
	"strings"
	"testing"
	"unicode"
)

// The previous rune-buffer implementation is an independent compatibility
// oracle for the byte-offset tokenizer.
func originalSearchTerms(text string) map[string]bool {
	out := map[string]bool{}
	var word, ja []rune
	flush := func() {
		if len(word) >= 2 {
			out[string(word)] = true
		}
		for i := 1; i < len(ja); i++ {
			out[string(ja[i-1:i+1])] = true
		}
		word, ja = nil, nil
	}
	for _, r := range strings.ToLower(text) {
		switch {
		case unicode.In(r, unicode.Han, unicode.Hiragana, unicode.Katakana) || r == '\u30fc':
			if len(word) > 0 {
				flush()
			}
			ja = append(ja, r)
		case unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_':
			if len(ja) > 0 {
				flush()
			}
			word = append(word, r)
		default:
			flush()
		}
	}
	flush()
	for _, term := range []string{"the", "and", "for", "with", "this", "that", "please"} {
		delete(out, term)
	}
	return out
}

func TestSearchTermsRetainOriginalTokens(t *testing.T) {
	pieces := []string{"", "A", "AB", "x_", "12", "THE", "Owner", "e\u0301", "\u0391\u03a3", "\u65e5\u672c\u8a9e", "\u30c6\u30b9\u30c8", "\u30d5\u30a9\u30fc\u30de\u30c3\u30c8", "\u65e5\u3002\u8a9e", string([]byte{0xff, 0xfe}), "_"}
	for _, a := range pieces {
		for _, b := range pieces {
			for _, separator := range []string{"", " ", ".", "\n"} {
				text := a + separator + b
				if got, want := searchTerms(text), originalSearchTerms(text); !reflect.DeepEqual(got, want) {
					t.Fatalf("token meanings changed for %q: got=%v want=%v", text, got, want)
				}
			}
		}
	}
}

func TestSearchTermsRepeatedTextAllocationBudget(t *testing.T) {
	text := strings.Repeat("owner routing unresolved choice ", 300)
	allocs := testing.AllocsPerRun(3, func() {
		got := searchTerms(text)
		if len(got) != 4 || !got["unresolved"] {
			t.Fatal("the full token set must survive")
		}
	})
	if allocs > 16 {
		t.Fatalf("repeated tokens exceed the allocation budget: %.0f > 16", allocs)
	}
}
