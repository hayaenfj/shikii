package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
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
// file without a section in two places and three references did the same. After those links named a section, the next run did
// not read recording.md, but it read wiki.md (34 KB) whole through SKILL.md's two links without a section: the same kind of link
// into another long file. So every link of the skill into a reference names a heading that exists in it, except the short
// references listed in wholeReferences, which an agent may read whole; a reference that is not listed, including a new one, needs
// a section. SKILL.md also says how to read one section.
func TestSkillLinksToLongReferencesNameOneSection(t *testing.T) {
	dir := "../../skills/shikii/references/"
	slugs := map[string]map[string]bool{}
	sizes := map[string]int{}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		body := string(mustRead(t, dir+e.Name()))
		sizes[e.Name()] = len(body)
		slugs[e.Name()] = map[string]bool{}
		for _, line := range strings.Split(body, "\n") {
			if strings.HasPrefix(line, "#") {
				slugs[e.Name()][markdownSlug(strings.TrimLeft(line, "# "))] = true
			}
		}
	}
	for name := range wholeReferences {
		if sizes[name] == 0 {
			t.Errorf("wholeReferences lists %s, which is not a reference", name)
		} else if sizes[name] > wholeReferenceMax {
			t.Errorf("%s is %d bytes, over %d: take it out of wholeReferences and name a section in its links", name, sizes[name], wholeReferenceMax)
		}
	}
	if problem := referenceLinkProblem("See [recording.md](references/recording.md).", slugs); problem == "" {
		t.Fatal("the check accepts a link to the whole of a long reference")
	}
	if problem := referenceLinkProblem("See [the steps](wiki.md#no-such-section).", slugs); problem == "" {
		t.Fatal("the check accepts a section that the reference does not have")
	}
	if problem := referenceLinkProblem("See [bands.md](references/bands.md).", slugs); problem != "" {
		t.Fatalf("the check refuses a short reference that may be read whole: %s", problem)
	}
	found := 0
	err = filepath.WalkDir("../../skills/shikii", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".md") {
			return err
		}
		for i, line := range strings.Split(string(mustRead(t, path)), "\n") {
			if !referenceLink.MatchString(line) {
				continue
			}
			found++
			if problem := referenceLinkProblem(line, slugs); problem != "" {
				t.Errorf("%s:%d: %s", path, i+1, problem)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if found == 0 {
		t.Fatal("no link into a reference was found; the walk is looking in the wrong place")
	}
	skill := string(mustRead(t, "../../skills/shikii/SKILL.md"))
	for _, w := range []string{"read only the section you need, not the whole file", "to the next heading"} {
		if !strings.Contains(skill, w) {
			t.Errorf("SKILL.md does not say how to read a reference by its section: it lacks %q", w)
		}
	}
}

// wholeReferences are the references short enough to read whole (each at most wholeReferenceMax bytes); a link into any other
// reference names a section.
var wholeReferences = map[string]bool{"bands.md": true, "spec.md": true, "content-quality.md": true, "truths.md": true, "handoff.md": true,
	"source-review.md": true, "experience.md": true, "import-memory.md": true, "tidy.md": true, "guards.md": true, "learning.md": true}

const wholeReferenceMax = 12 << 10

var referenceLink = regexp.MustCompile(`\]\((?:references/)?([a-z-]+\.md)(#[^)]*)?\)`)

func referenceLinkProblem(line string, slugs map[string]map[string]bool) string {
	for _, m := range referenceLink.FindAllStringSubmatch(line, -1) {
		name, section := m[1], m[2]
		if slugs[name] == nil {
			continue // SKILL.md itself or a file outside the references
		}
		if section == "" {
			if wholeReferences[name] {
				continue
			}
			return "links to " + name + " without naming a section"
		}
		if !slugs[name][section[1:]] {
			return "links to " + name + section + ", which is not a heading of " + name
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
