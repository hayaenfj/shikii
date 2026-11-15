package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The documents say what shikii does for OpenCode as the code does it. These tests read the current descriptions (not the sections
// of past versions, which stay as they were written).

// readDocT reads a file of the repository by its path from the repository's top folder.
func readDocT(t *testing.T, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// From 0.34.0 OpenCode 2.x has shikii's hooks through the plugin; only OpenCode 1.x (or 2.x without shikii's plugin) is without them.
// The guide's current section on article kinds lists the agents without hooks: it must name the version. The sentences are found by
// their start, in both languages (the 0.27.0 section of the guide is a past version's and is not this sentence).
func TestGuideNamesOnlyOpenCode1AmongAgentsWithoutHooks(t *testing.T) {
	for file, start := range map[string]string{
		"docs/guide.md":    "Agents without hooks (",
		"docs/guide.ja.md": "hook の無いエージェント(",
	} {
		text := readDocT(t, file)
		i := strings.Index(text, start)
		if i < 0 {
			t.Fatalf("%s: the sentence %q is gone; check the new wording by hand", file, start)
		}
		sentence := text[i:]
		if j := strings.IndexAny(sentence, ")）"); j >= 0 {
			sentence = sentence[:j+1]
		}
		if strings.Contains(sentence, "OpenCode") && !strings.Contains(sentence, "OpenCode 1.x") {
			t.Errorf("%s: %q lists OpenCode as an agent without hooks, but 0.34.0 gives OpenCode 2.x the hooks", file, sentence)
		}
	}
}
