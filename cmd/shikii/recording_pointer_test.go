package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"unicode"
)

// Measured on 2026-11-15 from the Claude Code session logs on the author's machine: agents read the whole of
// skills/shikii/references/recording.md (667 lines, about 70 KB) ten times, each read putting about 70 KB into the conversation.
// The one string the hook sent that named the file ended how_to_record with "See the installed skill's recording.md for the full
// contract." Every string in shikii's own code that names recording.md now names a heading that exists in it and says not to read
// the whole file. A new string that names the file in another way fails here.
func TestStringsThatNameRecordingMdPointToOneSection(t *testing.T) {
	headings := recordingHeadings(t)
	if problem := recordingPointerProblem("See the installed skill's recording.md for the full contract.", headings); problem == "" {
		t.Fatal("the check accepts a string that sends an agent to the whole file")
	}
	if problem := recordingPointerProblem("Search recording.md for `## No such heading` and read only that section, not the whole file.", headings); problem == "" {
		t.Fatal("the check accepts a heading that recording.md does not have")
	}
	found := 0
	for _, dir := range []string{".", "../../internal"} {
		err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
			if err != nil {
				return err
			}
			ast.Inspect(file, func(n ast.Node) bool {
				lit, ok := n.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING || !strings.Contains(lit.Value, "recording.md") {
					return true
				}
				text, err := strconv.Unquote(lit.Value)
				if err != nil {
					t.Errorf("%s: %v", path, err)
					return true
				}
				found++
				if problem := recordingPointerProblem(text, headings); problem != "" {
					t.Errorf("%s: %s: %q", path, problem, text)
				}
				return true
			})
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if found == 0 {
		t.Fatal("no string in shikii's code names recording.md; how_to_record used to, so the walk is looking in the wrong place")
	}
}

func recordingHeadings(t *testing.T) map[string]bool {
	headings := map[string]bool{}
	for _, line := range strings.Split(string(mustRead(t, "../../skills/shikii/references/recording.md")), "\n") {
		if strings.HasPrefix(line, "#") {
			headings[strings.TrimSpace(line)] = true
		}
	}
	return headings
}

var recordingHeadingInText = regexp.MustCompile("[`\"](#{1,3} [^`\"]+)[`\"]")

func recordingPointerProblem(text string, headings map[string]bool) string {
	named := recordingHeadingInText.FindAllStringSubmatch(text, -1)
	if len(named) == 0 {
		return "names recording.md without a heading to search for"
	}
	for _, m := range named {
		if !headings[m[1]] {
			return "names " + m[1] + ", which is not a heading of recording.md"
		}
	}
	if !strings.Contains(text, "not the whole file") {
		return "names recording.md without saying not to read the whole file"
	}
	return ""
}

// The same check of 2026-11-15 ran OpenCode 2.3.0 on the fixed binary: before its first prepare, it loaded the skill and read
// recording.md through the skill's links (53 KB returned), so the pointer in how_to_record never reached it. SKILL.md linked the
// file without a section in two places and three references did the same. Every link into recording.md from the skill now names
// a heading that exists in it, and SKILL.md says to read a reference by its section.
func TestSkillLinksToRecordingMdNameOneSection(t *testing.T) {
	slugs := map[string]bool{}
	for heading := range recordingHeadings(t) {
		slugs[markdownSlug(strings.TrimLeft(heading, "# "))] = true
	}
	if problem := recordingLinkProblem("See [recording.md](references/recording.md).", slugs); problem == "" {
		t.Fatal("the check accepts a link to the whole file")
	}
	if problem := recordingLinkProblem("See [the steps](recording.md#no-such-section).", slugs); problem == "" {
		t.Fatal("the check accepts a section that recording.md does not have")
	}
	found := 0
	err := filepath.WalkDir("../../skills/shikii", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".md") {
			return err
		}
		for i, line := range strings.Split(string(mustRead(t, path)), "\n") {
			if !recordingLink.MatchString(line) {
				continue
			}
			found++
			if problem := recordingLinkProblem(line, slugs); problem != "" {
				t.Errorf("%s:%d: %s", path, i+1, problem)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if found == 0 {
		t.Fatal("no link into recording.md was found; the walk is looking in the wrong place")
	}
	if skill := string(mustRead(t, "../../skills/shikii/SKILL.md")); !strings.Contains(skill, "read only the section you need, not the whole file") {
		t.Error("SKILL.md does not say to read a reference by its section")
	}
}

var recordingLink = regexp.MustCompile(`\]\((?:references/)?recording\.md(#[^)]*)?\)`)

func recordingLinkProblem(line string, slugs map[string]bool) string {
	for _, m := range recordingLink.FindAllStringSubmatch(line, -1) {
		if m[1] == "" {
			return "links to recording.md without naming a section"
		}
		if !slugs[m[1][1:]] {
			return "links to " + m[1] + ", which is not a heading of recording.md"
		}
	}
	return ""
}

// markdownSlug is the anchor GitHub gives a heading: lower case, spaces become hyphens, other punctuation is dropped.
func markdownSlug(heading string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(heading) {
		switch {
		case r == ' ':
			b.WriteRune('-')
		case r == '-' || r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
		}
	}
	return b.String()
}
