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
