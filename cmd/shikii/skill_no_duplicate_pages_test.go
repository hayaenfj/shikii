package main

import (
	"os"
	"strings"
	"testing"
)

// The person asked (2026-10-26) that the skill says not to add duplicate wiki pages: one topic, one page; merge into the page
// that already covers the topic (id), and fold existing duplicates into one (replaces) instead of keeping both.
func TestSkillSaysNotToAddDuplicatePages(t *testing.T) {
	for path, wants := range map[string][]string{
		"../../skills/shikii/SKILL.md":             {"Do not add duplicate pages", "one page per topic"},
		"../../skills/shikii/references/wiki.md":   {"Do not add duplicate pages", "one page per topic", "`replaces`"},
		"../../skills/shikii/references/review.md": {"duplicate"},
	} {
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, w := range wants {
			if !strings.Contains(string(b), w) {
				t.Fatalf("%s must say %q", path, w)
			}
		}
	}
}
