package main

import (
	"strings"
	"testing"
)

// 0.9.7 audit G-3: the full organizing notice and the notice for a missing query still taught lesson:<id>, which submit refuses; the key
// query returns is lesson:<id>:<revision>.
func TestReauditGNoticesTeachTheLessonKeyQueryReturns(t *testing.T) {
	for name, text := range map[string]string{"full": planningNotice("s", "0123456789abcdef"), "short": planningNoticeShort("s", "0123456789abcdef"), "no query": planningNoQueryNotice} {
		if !strings.Contains(text, "lesson:<id>:<revision>") {
			t.Errorf("%s: names the key query returns: %q", name, text)
		}
	}
}
