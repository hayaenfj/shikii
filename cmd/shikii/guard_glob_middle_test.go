package main

import "testing"

// What the guard patterns match. A "**" segment stands for any number of folders, none included: in the middle
// ("docs/**/*.md"), at the start ("**/*.md") and, as before, at the end ("docs/**": everything below). "*" still stays inside
// one path segment, and a pattern without a slash still matches the name alone at any depth.
func TestMatchPathReadsADoubleStarSegmentAsAnyNumberOfFolders(t *testing.T) {
	for _, c := range []struct {
		pattern, path string
		want          bool
	}{
		// "**" in the middle: no folder, one folder, several folders
		{"docs/**/*.md", "docs/b.md", true},
		{"docs/**/*.md", "docs/a/b.md", true},
		{"docs/**/*.md", "docs/a/c/b.md", true},
		{"src/**/*.go", "src/main.go", true},
		{"src/**/*.go", "src/cmd/main.go", true},
		{"src/**/*.go", "src/cmd/tool/main.go", true},
		{"src/**/testdata/**/*.json", "src/testdata/a.json", true},
		{"src/**/testdata/**/*.json", "src/pkg/x/testdata/cases/deep/a.json", true},
		// "**" at the start: the top level and any depth
		{"**/*.md", "README.md", true},
		{"**/*.md", "docs/a.md", true},
		{"**/*.md", "docs/a/b.md", true},
		{"**/CHANGELOG.md", "CHANGELOG.md", true},
		{"**/CHANGELOG.md", "docs/CHANGELOG.md", true},
		{"**/CHANGELOG.md", "docs/a/b/CHANGELOG.md", true},
		// a trailing "dir/**" is everything below, as before; "**" alone is every path
		{"docs/**", "docs/a.md", true},
		{"docs/**", "docs/a/b/c.md", true},
		{"docs/**", "src/docs.md", false},
		{"docs/**", "docs", false},
		{"docs/**/internal/**", "docs/internal/a.md", true},
		{"docs/**/internal/**", "docs/x/y/internal/z/a.md", true},
		{"docs/**/internal/**", "docs/x/internal", false},
		{"**", "docs/a/b.md", true},
		// the folder before a trailing "/**" may be a glob too: each "*" is one folder
		{"docs/*/gen/**", "docs/a/gen/x/y.go", true},
		{"docs/*/gen/**", "docs/a/b/gen/x.go", false},
		{"docs/*/gen/**", "docs/a/gen", false},
		// a pattern without a slash also matches the name alone, at any depth, as before
		{"*.md", "docs/a/b.md", true},
		{"docs/*.md", "docs/a.md", true},

		// the rest of the path still has to match around the "**"
		{"docs/**/*.md", "src/a/b.md", false},
		{"docs/**/*.md", "docs/a/b.txt", false},
		{"docs/**/*.md", "xdocs/a/b.md", false},
		{"docs/**/*.md", "docs", false},
		{"**/*.md", "docs/a.txt", false},
		{"**/CHANGELOG.md", "docs/CHANGELOG_ja.md", false},
		{"src/**/testdata/**/*.json", "src/pkg/cases/a.json", false},
		// "*" does not cross a path separator, next to a "**" or without one
		{"docs/*.md", "docs/a/b.md", false},
		{"docs/*/*.md", "docs/a/c/b.md", false},
		{"src/*/main.go", "src/a/b/main.go", false},
		{"docs/**/x*.md", "docs/a/xb/c.md", false},
		{"docs/**/a*/c.md", "docs/ab/x/c.md", false},
		{"docs/*/**/c.md", "docs/c.md", false},
		{"docs/*/**/c.md", "docs/a/c.md", true},
		// "**" inside a segment is not a folder wildcard: it is a "*" within that one segment, as before
		{"docs/**.md", "docs/a.md", true},
		{"docs/**.md", "docs/a/b.md", false},
	} {
		if got := matchPath(c.pattern, c.path); got != c.want {
			t.Errorf("matchPath(%q, %q) = %v, want %v", c.pattern, c.path, got, c.want)
		}
	}
}

// What that means for the guards: a pair written with "**" in the middle sees the files at every depth, and an except with
// "**" in the middle leaves out the files at every depth; a file outside the pattern is still checked.
func TestGuardsWithADoubleStarInTheMiddleSeeEveryDepth(t *testing.T) {
	g := guard{ID: "go-log", Kind: guardKindPairedFiles, Title: "a source change needs a changelog line", Pairs: [][]string{{"src/**/*.go", "CHANGELOG.md"}}}
	if err := g.validate(); err != nil {
		t.Fatalf("the pattern is accepted: %v", err)
	}
	negative, _ := guardNegative(g)
	if r := evalGuard(g, negative); r.Pass {
		t.Fatal("the negative example of enable stops, so the guard can be enabled")
	}
	if r := evalGuard(g, guardPositive(g)); !r.Pass {
		t.Fatalf("the positive example of enable passes: %s", r.Reason)
	}
	for path, stops := range map[string]bool{
		"src/main.go":          true,
		"src/cmd/main.go":      true,
		"src/cmd/tool/main.go": true,
		"src/cmd/tool/main.rs": false,
		"lib/src/main.go":      false,
	} {
		if r := evalGuard(g, guardInput{Changed: []string{path}, Known: true}); r.Pass == stops {
			t.Errorf("a turn that changed only %s: stopped=%v, want %v", path, !r.Pass, stops)
		}
	}
	words := guard{ID: "no-word", Kind: guardKindForbiddenWords, Title: "do not use the word", Words: []string{"FORBIDDENWORD"}, Except: []string{"docs/**/*.md"}}
	for path, stops := range map[string]bool{
		"docs/b.md":       false,
		"docs/a/b.md":     false,
		"docs/a/c/b.md":   false,
		"docs/a/c/b.go":   true,
		"src/docs/a/b.md": true,
	} {
		if r := evalGuard(words, guardInput{Changed: []string{path}, Known: true, Added: map[string][]string{path: {"FORBIDDENWORD here"}}}); r.Pass == stops {
			t.Errorf("the word added to %s: stopped=%v, want %v", path, !r.Pass, stops)
		}
	}
}
