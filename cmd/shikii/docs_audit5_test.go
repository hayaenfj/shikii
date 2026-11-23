package main

import (
	"strings"
	"testing"
)

// The fifth audit of 0.41.0: the documents say what a withdrawal takes (a record of the tool's own work only), what a subagent the agent
// continued is counted by, that OpenCode tells every turn of a subagent, and when the state of a session is gone. The sentences are found
// by the words they must hold, in English and in Japanese.
func TestTheDocsSayWhatTheFifthAuditChanged(t *testing.T) {
	for _, c := range []struct {
		file, version string // version: only the section of that version of a changelog
		has           []string
	}{
		{"docs/guide.md", "", []string{
			"`shikii wiki withdraw`",
			"(kind `agent`, `code` or `test`)",
			"an event of the session that carries a subagent's id",
			"every turn of a subagent's session",
			"not the time that hook finished",
			"7 days after its last event",
		}},
		{"CHANGELOG.md", "0.41.0", []string{
			"is a record of the tool's own work",
			"from its first event that shikii sees",
			"every turn of a subagent's session",
			"7 days after the session's last event",
		}},
		{"skills/shikii/references/wiki.md", "", []string{
			"is a record of the tool's own work (kind `agent`, `code` or `test`)",
		}},
		{"docs/guide.ja.md", "", []string{
			"`shikii wiki withdraw`",
			"kind \u304c `agent`\u30fb`code`\u30fb`test`",
			"\u30b5\u30d6\u30a8\u30fc\u30b8\u30a7\u30f3\u30c8\u306e id \u3092\u6301\u3064\u30bb\u30c3\u30b7\u30e7\u30f3\u306e\u51fa\u6765\u4e8b",
			"\u30bf\u30fc\u30f3\u3054\u3068\u306b\u59cb\u307e\u308a\u3068\u7d42\u308f\u308a\u3092\u4f1d\u3048\u308b",
			"7 \u65e5\u305f\u3063\u305f\u72b6\u614b\u306f shikii \u304c\u6d88\u3059",
			"\u59cb\u307e\u3063\u305f\u6642\u523b\u306a\u306e\u3067\u3001\u305d\u306e\u540c\u3058 hook \u304c\u59cb\u3081\u305f\u30b5\u30d6\u30a8\u30fc\u30b8\u30a7\u30f3\u30c8\u3082\u6570\u3048\u308b",
		}},
		{"CHANGELOG_ja.md", "0.41.0", []string{
			"\u9053\u5177\u304c\u81ea\u5206\u306e\u4ed5\u4e8b\u304b\u3089\u4fdd\u5b58\u3057\u305f\u8a18\u9332",
			"\u3092 shikii \u304c\u898b\u305f\u6642\u304b\u3089\u3001\u305d\u306e\u7d42\u308f\u308a\u307e\u3067",
			"\u30bf\u30fc\u30f3\u3054\u3068\u306b\u59cb\u307e\u308a\u3068\u7d42\u308f\u308a\u3092\u4f1d\u3048\u308b",
			"7 \u65e5\u305f\u3064\u3068 shikii \u304c\u6d88\u3057\u305f",
		}},
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
	}
}
