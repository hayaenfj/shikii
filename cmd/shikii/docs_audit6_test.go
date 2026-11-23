package main

import (
	"strings"
	"testing"
)

// The sixth audit of 0.41.0: a withdrawal does not delete. The documents say that a withdrawn article stays, that the person restores or
// deletes it, that the person does, and that an article is withdrawn when every source it cites is named and it holds no correction; none
// of them still says that an article that cites a person's words is never withdrawn, or that a withdrawal needs a kind of source.
func TestTheDocsSayAWithdrawalKeepsTheArticleForAPerson(t *testing.T) {
	for _, c := range []struct {
		file, version string // version: only the section of that version of a changelog
		has, hasNot   []string
	}{
		{"docs/guide.md", "", []string{"stays as a withdrawn article", "holds no person's correction", "restores each", "deletes it (after a check", "`action: edit / delete / restore`", "`withdrawn`", "how many withdrawn articles are waiting"}, []string{"never one that cites", "kind `agent`, `code` or `test`", "deletes an article when the person deletes"}},
		{"CHANGELOG.md", "0.41.0", []string{"A withdrawn article is not deleted", "\"Withdrawn articles\"", "chooses to restore it", "holds no person's correction", "say how many withdrawn articles are waiting"}, []string{"is a record of the tool's own work", "is never withdrawn"}},
		{"skills/shikii/references/wiki.md", "", []string{"stays as a withdrawn article", "holds no person's correction", "restores each or deletes it", "no command or `shikii_wiki` operation does either"}, []string{"is a record of the tool's own work", "is never withdrawn"}},
		{"cmd/shikii/agent_usage.go", "", []string{"it stays as a withdrawn article", "a person restores or deletes it"}, []string{"never one that cites a person's words"}},
		{"docs/guide.ja.md", "", []string{"\u300c\u53d6\u308a\u4e0b\u3052\u305f\u8a18\u4e8b\u300d\u306b\u306a\u308a", "\u4eba\u306e\u8a02\u6b63\u3092\u6301\u305f\u306a\u3044\u6642\u3060\u3051\u53d6\u308a\u4e0b\u3052\u308b", "\u5143\u306b\u623b\u3059(\u307e\u305f\u914d\u3089\u308c\u308b)\u304b", "`action: edit / delete / restore`", "\u53d6\u308a\u4e0b\u3052\u305f\u8a18\u4e8b\u306e\u6570\u306f"}, []string{"kind \u304c `agent`\u30fb`code`\u30fb`test`", "\u4eba\u306e\u8a00\u8449\u3092\u5f15\u304f\u8a18\u4e8b"}},
		{"CHANGELOG_ja.md", "0.41.0", []string{"\u53d6\u308a\u4e0b\u3052\u305f\u8a18\u4e8b\u306f\u3001\u5f8c\u306e\u4f9d\u983c\u306b\u914d\u3089\u308c\u306a\u304f\u306a\u308a\u307e\u3059\u304c\u3001\u6d88\u3048\u307e\u305b\u3093", "\u300c\u5143\u306b\u623b\u3059\u300d", "\u4eba\u306e\u8a02\u6b63\u3092\u6301\u305f\u306a\u3044\u8a18\u4e8b\u3060\u3051\u3067\u3059"}, []string{"\u4eba\u306e\u8a00\u8449\u3092\u5f15\u304f\u8a18\u4e8b", "kind \u304c `agent`\u30fb`code`\u30fb`test`"}},
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
