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
		"docs/guide.ja.md": "hook \u306e\u7121\u3044\u30a8\u30fc\u30b8\u30a7\u30f3\u30c8(",
	} {
		text := readDocT(t, file)
		i := strings.Index(text, start)
		if i < 0 {
			t.Fatalf("%s: the sentence %q is gone; check the new wording by hand", file, start)
		}
		sentence := text[i:]
		if j := strings.IndexAny(sentence, ")\uff09"); j >= 0 {
			sentence = sentence[:j+1]
		}
		if strings.Contains(sentence, "OpenCode") && !strings.Contains(sentence, "OpenCode 1.x") {
			t.Errorf("%s: %q lists OpenCode as an agent without hooks, but 0.34.0 gives OpenCode 2.x the hooks", file, sentence)
		}
	}
}

// changelogSectionT is the section of one version in a changelog (from its "## <version>" heading to the next "## " heading).
func changelogSectionT(t *testing.T, text, version string) string {
	t.Helper()
	i := strings.Index(text, "\n## "+version+" ")
	if i < 0 {
		t.Fatalf("the changelog has no section for %s", version)
	}
	section := text[i+1:]
	if j := strings.Index(section[3:], "\n## "); j >= 0 {
		section = section[:j+3]
	}
	return section
}

// OpenCode 2.3.0 reads its plugins only from $OPENCODE_CONFIG_DIR/plugins when that is set, so it does not read shikii's plugin. Every
// document that says where shikii puts the plugin says so too (doctor warns about it: TestDoctorSaysWhenOpenCodeReadsPluginsFromAnotherConfigFolder).
func TestDocsSayOpenCodeReadsNoPluginOfShikiisUnderOPENCODE_CONFIG_DIR(t *testing.T) {
	docs := map[string]string{}
	for _, f := range []string{"README.md", "docs/readme/README_ja.md", "integrations/claude-code.md", "integrations/claude-code.ja.md"} {
		docs[f] = readDocT(t, f)
	}
	docs["CHANGELOG.md 0.34.0"] = changelogSectionT(t, readDocT(t, "CHANGELOG.md"), "0.34.0")
	docs["CHANGELOG_ja.md 0.34.0"] = changelogSectionT(t, readDocT(t, "CHANGELOG_ja.md"), "0.34.0")
	for name, text := range docs {
		if !strings.Contains(text, "plugins/shikii") && !strings.Contains(text, "OpenCode 2.x") {
			t.Fatalf("%s no longer describes the OpenCode plugin; check this test by hand", name)
		}
		if !strings.Contains(text, "`OPENCODE_CONFIG_DIR`") || !strings.Contains(text, "`shikii doctor`") {
			t.Errorf("%s says where the OpenCode plugin goes, but not that an OpenCode started with OPENCODE_CONFIG_DIR does not read it (and that shikii doctor says so)", name)
		}
	}
}
