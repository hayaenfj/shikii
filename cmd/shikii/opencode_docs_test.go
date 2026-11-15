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

// xdgSentencesWithoutAbsolute lists the sentences of text that name $XDG_CONFIG_HOME without saying it counts only as an absolute path
// ("absolute", or the Japanese for an absolute path). shikii follows XDG_CONFIG_HOME only when it is absolute (opencodeConfigDir; a
// relative one is ignored, as TestHookInstallPutsOpenCodePluginUnderXDGConfigHome checks), so "when it is set" says more than it does.
func xdgSentencesWithoutAbsolute(text string) []string {
	var bad []string
	for rest := text; ; {
		i := strings.Index(rest, "XDG_CONFIG_HOME")
		if i < 0 {
			return bad
		}
		start := 0
		for _, end := range []string{". ", "\u3002", "\n"} {
			if j := strings.LastIndex(rest[:i], end); j >= 0 && j+len(end) > start {
				start = j + len(end)
			}
		}
		stop := len(rest)
		for _, end := range []string{". ", "\u3002", "\n"} {
			if j := strings.Index(rest[i:], end); j >= 0 && i+j+len(end) < stop {
				stop = i + j + len(end)
			}
		}
		if sentence := rest[start:stop]; !strings.Contains(sentence, "absolute") && !strings.Contains(sentence, "\u7d76\u5bfe\u30d1\u30b9") {
			bad = append(bad, sentence)
		}
		rest = rest[stop:]
	}
}

// The documents say where the plugin goes as opencodeConfigDir decides it: under $XDG_CONFIG_HOME only when that is an absolute path.
func TestDocsSayXDGConfigHomeCountsOnlyWhenAbsolute(t *testing.T) {
	// the check itself: "when that is set" is caught, the absolute condition passes
	if bad := xdgSentencesWithoutAbsolute("It goes in `~/.config/x` (under `$XDG_CONFIG_HOME/x` when that is set). Next."); len(bad) != 1 {
		t.Fatalf("a sentence without the absolute condition must be caught: %q", bad)
	}
	if bad := xdgSentencesWithoutAbsolute("It goes in `~/.config/x` (under `$XDG_CONFIG_HOME/x` when that is an absolute path). Next."); len(bad) != 0 {
		t.Fatalf("a sentence with the absolute condition passes: %q", bad)
	}
	for _, f := range []string{"README.md", "docs/readme/README_ja.md", "integrations/claude-code.md", "integrations/claude-code.ja.md", "skills/shikii/SKILL.md"} {
		text := readDocT(t, f)
		if !strings.Contains(text, "XDG_CONFIG_HOME") {
			t.Errorf("%s no longer names $XDG_CONFIG_HOME; check this test by hand", f)
		}
		for _, sentence := range xdgSentencesWithoutAbsolute(text) {
			t.Errorf("%s: %q names $XDG_CONFIG_HOME without saying shikii follows it only as an absolute path", f, sentence)
		}
	}
}

// opencodeLimitSentences lists the sentences, in the paragraphs of text that name OpenCode, that say text is not shown to the person or
// that there is no hook before a command, without making shikii's plugin their subject. OpenCode 2.3.0 has both for plugins
// (session.synthetic shows text, ctx.tool.hook("execute.before") runs before a command); shikii's plugin uses neither. So a limit is
// said only of shikii's plugin (an allow-list of subjects), never of OpenCode or of plugins in general.
func opencodeLimitSentences(text string) []string {
	topics := []string{"show text", "show you", "shows you", "before a command", "\u898b\u305b", "\u30b3\u30de\u30f3\u30c9\u306e\u524d"}
	subjects := []string{"shikii's plugin", "The plugin ", "the plugin ", "shikii \u306e\u30d7\u30e9\u30b0\u30a4\u30f3", "shikii\u306e\u30d7\u30e9\u30b0\u30a4\u30f3"}
	var bad []string
	for _, paragraph := range strings.Split(text, "\n") {
		if !strings.Contains(paragraph, "OpenCode") {
			continue
		}
		for _, sentence := range splitSentencesT(paragraph) {
			topic, subject := false, false
			for _, w := range topics {
				topic = topic || strings.Contains(sentence, w)
			}
			for _, w := range subjects {
				subject = subject || strings.Contains(sentence, w)
			}
			if topic && !subject {
				bad = append(bad, sentence)
			}
		}
	}
	return bad
}

// splitSentencesT cuts a paragraph after ". " and after the Japanese full stop.
func splitSentencesT(paragraph string) []string {
	var out []string
	for _, part := range strings.SplitAfter(paragraph, "\u3002") {
		for _, sentence := range strings.SplitAfter(part, ". ") {
			if strings.TrimSpace(sentence) != "" {
				out = append(out, sentence)
			}
		}
	}
	return out
}

func TestDocsSayOnlyShikiisPluginLacksShownTextAndACommandHook(t *testing.T) {
	// the check itself: a limit said of OpenCode is caught, the same limit said of shikii's plugin passes
	if bad := opencodeLimitSentences("OpenCode 2.x: it works. OpenCode gives plugins no place to show text to you. Next."); len(bad) != 1 {
		t.Fatalf("a limit said of OpenCode must be caught: %q", bad)
	}
	if bad := opencodeLimitSentences("OpenCode 2.x: it works. shikii's plugin does not show text to you or add a hook before a command. Next."); len(bad) != 0 {
		t.Fatalf("a limit said of shikii's plugin passes: %q", bad)
	}
	docs := map[string]string{}
	for _, f := range []string{"README.md", "docs/readme/README_ja.md", "integrations/claude-code.md", "integrations/claude-code.ja.md", "skills/shikii/SKILL.md"} {
		docs[f] = readDocT(t, f)
	}
	docs["CHANGELOG.md 0.34.0"] = changelogSectionT(t, readDocT(t, "CHANGELOG.md"), "0.34.0")
	docs["CHANGELOG_ja.md 0.34.0"] = changelogSectionT(t, readDocT(t, "CHANGELOG_ja.md"), "0.34.0")
	for name, text := range docs {
		for _, sentence := range opencodeLimitSentences(text) {
			t.Errorf("%s: %q says OpenCode (or any plugin) cannot show text or has no hook before a command; OpenCode 2.3.0 has both, so say it of shikii's plugin", name, sentence)
		}
	}
}
