package main

import (
	"strings"
	"testing"

	"github.com/hayaenfj/shikii/internal/handoff"
)

// audit E: which texts a work record takes. A text that shows something is taken, in any script; only a text with nothing to read
// is refused, and then nothing is written.
func TestAuditEHandoffTextsThatMustBeTaken(t *testing.T) {
	repo, root, _ := planningFixture(t)
	t.Chdir(repo)
	take := map[string]string{
		"an emoji alone":              "\U0001F44D",
		"an emoji with a selector":    "\u2764\ufe0f",
		"a flag":                      "\U0001F1EF\U0001F1F5",
		"a dash":                      "-",
		"three dots":                  "...",
		"a markdown rule":             "---\n",
		"a number":                    "0",
		"Japanese":                    "\u5b8c\u4e86",
		"Arabic":                      "\u062a\u0645",
		"Thai":                        "\u0e40\u0e2a\u0e23\u0e47\u0e08",
		"a combining mark on a space": " \u0301",
		"a mark, then text":           "\ufeffdone",
		"a tab and a letter":          "\tx",
	}
	refuse := map[string]string{
		"empty":                   "",
		"spaces and breaks":       " \n\t\r\n",
		"an ideographic space":    "\u3000",
		"a byte order mark":       "\ufeff",
		"a zero-width space":      "\u200b",
		"a zero-width joiner":     "\u200d",
		"a no-break space":        "\u00a0",
		"a control character":     "\x00\x1b",
		"a soft hyphen":           "\u00ad",
		"a line separator":        "\u2028",
		"a private-use character": "\ue0a0",
	}
	for _, op := range []string{"handoff_write", "handoff_append"} {
		for name, text := range take {
			if _, err := runHandoff(root, notebookRequest{Operation: op, Path: "audit-e/" + op + ".md", Content: text}); err != nil {
				t.Errorf("%s, %s (%q): refused: %v", op, name, text, err)
			}
			if !handoff.HasText(text) {
				t.Errorf("%s (%q) counts as a text with nothing to read", name, text)
			}
		}
		before, _ := runHandoff(root, notebookRequest{Operation: "handoff_read", Path: "audit-e/" + op + ".md"})
		for name, text := range refuse {
			_, err := runHandoff(root, notebookRequest{Operation: op, Path: "audit-e/" + op + ".md", Content: text})
			if err == nil || !strings.Contains(err.Error(), "nothing was written") {
				t.Errorf("%s, %s (%q): taken, or refused in other words: %v", op, name, text, err)
			}
		}
		after, _ := runHandoff(root, notebookRequest{Operation: "handoff_read", Path: "audit-e/" + op + ".md"})
		if auditJSON(before) != auditJSON(after) {
			t.Errorf("%s: a refused text changed the record", op)
		}
	}
}
