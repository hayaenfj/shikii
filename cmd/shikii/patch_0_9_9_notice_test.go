package main

import (
	"strings"
	"testing"
)

// C DF-4, DF-5, DF-6: the short notices lacked instructions the full text gives, and neither said where memory's source_id comes from (a
// lesson delivered to a subagent has its own id, not the conversation's). The checks are the words each text must hold.
func TestPatch099ShortNoticesKeepTheFullTextsInstructions(t *testing.T) {
	short := planningNoticeShort("s", "0123456789abcdef")
	for _, w := range []string{"write it to the wiki", "false"} {
		if !strings.Contains(short, w) {
			t.Errorf("DF-4: the short organizing notice says %q: %q", w, short)
		}
	}
	shortReflect, fullReflect := reflectAskShort("X reflect", []string{"a"}), reflectAsk("X reflect")
	for _, w := range []string{"shikii guard add", "complete:true", "observations"} {
		if !strings.Contains(shortReflect, w) {
			t.Errorf("DF-5: the short reflection request says %q: %q", w, shortReflect)
		}
	}
	for name, text := range map[string]string{"full": fullReflect, "short": shortReflect} {
		if !strings.Contains(text, "memories[].source_id") {
			t.Errorf("DF-6: the %s reflection request says memory's source_id is query's memories[].source_id: %q", name, text)
		}
	}
}
