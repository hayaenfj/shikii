package main

import (
	"strings"
	"testing"
)

// The documents say what forget refuses and counts as the code does it (the fourth audit of 0.41.0): a subagent that is still open is
// a reason to refuse, a tool server the agent starts after the session began is counted, and a session whose agent is closed is not
// always removable at once. The sentences are found by the words they must hold (and, for the Japanese guide, one sentence that fell
// apart must be whole).
func TestTheDocsSayWhatForgetRefusesAndCounts(t *testing.T) {
	for _, c := range []struct {
		file, version string // version: only the section of that version of a changelog
		has, hasNot   []string
	}{
		{"skills/shikii/SKILL.md", "", []string{"a subagent the session started has not ended", "a session whose agent shikii noted can be removed", "refused until its newest record is 24 hours old"}, []string{"any session can be removed"}},
		{"docs/guide.md", "", []string{"a subagent that began under it after the hook first saw the agent run the session has not ended", "A tool server the agent starts later, while the session runs", "`caffeinate`"}, nil},
		{"CHANGELOG.md", "0.41.0", []string{"in four cases", "a subagent the session started is still open", "a tool server the agent starts later, while the session runs", "`caffeinate`"}, nil},
		{"docs/guide.ja.md", "", []string{"\u88cf\u3067\u52d5\u304f\u30b5\u30d6\u30a8\u30fc\u30b8\u30a7\u30f3\u30c8\u306f\u30a8\u30fc\u30b8\u30a7\u30f3\u30c8\u81ea\u8eab\u306e\u30d7\u30ed\u30bb\u30b9\u306e\u4e2d\u3067\u8003\u3048\u308b", "\u8a00\u8a9e\u30b5\u30fc\u30d0\u30fc\u3001`caffeinate`", "\u4f5c\u3063\u305f\u63a7\u3048\u306e\u30d5\u30a9\u30eb\u30c0\u306f\u6d88\u3059)\u3067\u3069\u308c\u304b\u3092\u4f1d\u3048\u308b\u3002"}, []string{"\u6d88\u3059\u305f\u3081\u3067\u3069\u308c\u304b\u3092\u4f1d\u3048\u308b"}},
		{"CHANGELOG_ja.md", "0.41.0", []string{"\u6b21\u306e 4 \u3064\u306e\u6642\u306b\u65ad\u308a", "`caffeinate`"}, nil},
	} {
		text := readDocT(t, c.file)
		if c.version != "" {
			text = changelogSectionT(t, text, c.version)
		}
		for _, want := range c.has {
			if !strings.Contains(text, want) {
				t.Errorf("%s does not say %q", c.file, want)
			}
		}
		for _, bad := range c.hasNot {
			if strings.Contains(text, bad) {
				t.Errorf("%s still says %q", c.file, bad)
			}
		}
	}
}
