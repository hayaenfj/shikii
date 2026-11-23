package main

import (
	"strings"
	"testing"
)

// The seventh audit of 0.41.0: what a shikii older than 0.41.0 does with a wiki that holds a withdrawn article (it saves nothing into it,
// not only "cannot read" the one article), what to do after updating, and who can restore or delete: the person on the page, with no
// operation on the command line or MCP, not "only a person" (the page's HTTP request does not tell a program on this machine from the page).
// A page that is still running after the update is old too: it does not list the withdrawn articles, so the update note names it with the MCP
// server and the agent sessions (audit 8, low-2).
func TestTheDocsSayWhatAnOldShikiiDoesWithAWithdrawnArticleAndWhoRestoresIt(t *testing.T) {
	for _, c := range []struct {
		file, version string // version: only the section of that version of a changelog
		has, hasNot   []string
	}{
		{"CHANGELOG.md", "0.41.0", []string{"cannot save any article into a wiki that holds a withdrawn article", "reopen the running page (`shikii serve`), the running MCP server and your agent sessions", "an old page does not list the withdrawn articles", "the command line and MCP have no operation for either"}, []string{"cannot read a withdrawn article", "Only the person restores or deletes"}},
		{"CHANGELOG_ja.md", "0.41.0", []string{"\u53d6\u308a\u4e0b\u3052\u305f\u8a18\u4e8b\u304c\u3042\u308b wiki \u306b\u3001\u8a18\u4e8b\u3092 1 \u4ef6\u3082\u4fdd\u5b58\u3067\u304d\u307e\u305b\u3093", "\u52d5\u3044\u3066\u3044\u308b\u30da\u30fc\u30b8(`shikii serve`)\u3068 MCP \u306e\u672c\u4f53\u3068\u30a8\u30fc\u30b8\u30a7\u30f3\u30c8\u306e\u30bb\u30c3\u30b7\u30e7\u30f3\u3082\u958b\u304d\u76f4\u3057\u3066\u304f\u3060\u3055\u3044", "\u53e4\u3044\u30da\u30fc\u30b8\u306b\u306f\u53d6\u308a\u4e0b\u3052\u305f\u8a18\u4e8b\u306e\u4e00\u89a7\u304c\u51fa\u307e\u305b\u3093", "\u30b3\u30de\u30f3\u30c9\u3068 MCP \u306b\u306f\u3001\u305d\u306e\u305f\u3081\u306e\u64cd\u4f5c\u304c\u3042\u308a\u307e\u305b\u3093"}, []string{"\u53d6\u308a\u4e0b\u3052\u305f\u8a18\u4e8b\u3092\u8aad\u3081\u306a\u3044", "\u4f7f\u3046\u4eba\u3060\u3051\u3067"}},
		{"docs/guide.md", "", []string{"older than 0.41.0 cannot save any article into a wiki that holds a withdrawn article", "reopen the running page (`shikii serve`), the MCP server and the agent sessions", "an old page does not list the withdrawn articles"}, nil},
		{"docs/guide.ja.md", "", []string{"0.41.0 \u3088\u308a\u524d\u306e shikii \u306f\u3001\u53d6\u308a\u4e0b\u3052\u305f\u8a18\u4e8b\u304c\u3042\u308b wiki \u306b\u8a18\u4e8b\u3092 1 \u4ef6\u3082\u4fdd\u5b58\u3067\u304d\u306a\u3044", "\u52d5\u3044\u3066\u3044\u308b\u30da\u30fc\u30b8(`shikii serve`)\u3068 MCP \u306e\u672c\u4f53\u3068\u30a8\u30fc\u30b8\u30a7\u30f3\u30c8\u306e\u30bb\u30c3\u30b7\u30e7\u30f3\u3082\u958b\u304d\u76f4\u3059", "\u53e4\u3044\u30da\u30fc\u30b8\u306b\u306f\u53d6\u308a\u4e0b\u3052\u305f\u8a18\u4e8b\u306e\u4e00\u89a7\u304c\u51fa\u306a\u3044"}, nil},
		{"cmd/shikii/agent_usage.go", "", []string{"a person restores or deletes it on the Knowledge page"}, []string{"only a person restores or deletes it"}},
		{"internal/notebook/withdraw.go", "", []string{"stops a request from another site"}, []string{"never by an agent"}},
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
