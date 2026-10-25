package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode"
)

// memoryImportMark is the marker that a memory file was moved to the wiki: ".shikii/wiki" followed by a page id (16 hex digits).
// A moved file keeps this line (canonical: .shikii/wiki <id>) in its body (the skill's import-memory.md). Text that merely mentions the wiki is not a marker (audit R4).
var memoryImportMark = regexp.MustCompile(`\.shikii/wiki\s+[0-9a-f]{16}`)

// memoryReadLimit is how much is read to look for the marker. The steps put the marker near the top of the body, so a huge file is not read on every request (audit R4).
const memoryReadLimit = 64 << 10

// agentMemoryDir is where this project's agent memory lives (Claude Code's auto memory:
// ~/.claude/projects/<the path with every non-alphanumeric turned into ->/memory; a link to a shared place is fine too). Empty if none.
func agentMemoryDir(scope string) string {
	home, err := os.UserHomeDir()
	if err != nil || scope == "" {
		return ""
	}
	slug := strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			return r
		}
		return '-'
	}, scope)
	dir := filepath.Join(home, ".claude", "projects", slug, "memory")
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return ""
	}
	return dir
}

// readMemoryHead reads the head of a memory file (up to memoryReadLimit).
func readMemoryHead(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, memoryReadLimit))
	return string(b), err
}

// unimportedMemoryFiles is the files in the memory folder not yet moved to the wiki (.md files except the index MEMORY.md).
// "Moved" = the body has the marker with a page id. In a folder that was partly moved, only the rest is counted.
func unimportedMemoryFiles(dir string) []string {
	if dir == "" {
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".md") || name == "MEMORY.md" {
			continue
		}
		head, err := readMemoryHead(filepath.Join(dir, name))
		if err != nil || memoryImportMark.MatchString(head) {
			continue
		}
		out = append(out, name)
	}
	return out
}

// codexMemoryDir is Codex's memory folder (~/.codex/memories). It is shared by all projects, so only files naming this project are looked at.
func codexMemoryDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	dir := filepath.Join(home, ".codex", "memories")
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return ""
	}
	return dir
}

// projectNames is the words that name this project: the folder path itself, the folder name (4 characters or more), and the repository name of git's origin (when different).
// Short folder names (app, go) appear in other files by chance, so only the path is used for them (audit R4).
func projectNames(scope string) []string {
	base := filepath.Base(scope)
	if scope == "" || base == "." || base == "/" || base == string(filepath.Separator) {
		return nil
	}
	names := []string{scope}
	if len(base) >= 4 {
		names = append(names, base)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if remote, ok := gitRun(ctx, scope, "remote", "get-url", "origin"); ok {
		repo := strings.TrimSuffix(filepath.Base(strings.TrimSpace(remote)), ".git")
		if len(repo) >= 4 && repo != base {
			names = append(names, repo)
		}
	}
	return names
}

// namesProject reports whether the text names this project: it contains a name at a word boundary (no alphanumeric before or after). A path may match as a substring.
func namesProject(text string, names []string) bool {
	for _, n := range names {
		if strings.Contains(n, string(filepath.Separator)) {
			if strings.Contains(text, n) {
				return true
			}
			continue
		}
		for i := 0; ; {
			j := strings.Index(text[i:], n)
			if j < 0 {
				break
			}
			at := i + j
			before, after := rune(0), rune(0)
			if at > 0 {
				before = []rune(text[:at])[len([]rune(text[:at]))-1]
			}
			if rest := text[at+len(n):]; rest != "" {
				after = []rune(rest)[0]
			}
			wordChar := func(r rune) bool {
				return r != 0 && (unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '-') && r < 128
			} // haya1-old does not name haya1
			if !wordChar(before) && !wordChar(after) {
				return true
			}
			i = at + len(n)
		}
	}
	return false
}

// unimportedCodexMemoryFiles is the files in Codex's shared memory that name this project and are not yet moved to the wiki.
func unimportedCodexMemoryFiles(dir string, names []string) []string {
	if dir == "" || len(names) == 0 {
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".md") || name == "MEMORY.md" {
			continue
		}
		head, err := readMemoryHead(filepath.Join(dir, name))
		if err != nil || memoryImportMark.MatchString(head) || !namesProject(head, names) {
			continue
		}
		out = append(out, name)
	}
	return out
}

// listFiles is the file names listed in a notice (the first 5 and a count of the rest).
func listFiles(files []string) string {
	if len(files) <= 5 {
		return strings.Join(files, ", ")
	}
	return strings.Join(files[:5], ", ") + fmt.Sprintf(" and %d more", len(files)-5)
}

// memoryImportNotice is one line telling the agent, if there is memory not yet in the wiki, to move it with the skill's steps (once per session).
// It is a notice so this gets noticed without being told; deleting is the person's job. It is shown as long as unmoved files remain, even in projects that had a wiki before 0.8.0.
// It looks at Claude Code's memory for this project and at the files in Codex's shared memory that name this project.
func memoryImportNotice(scope string) string {
	var parts []string
	if dir := agentMemoryDir(scope); dir != "" {
		if files := unimportedMemoryFiles(dir); len(files) > 0 {
			parts = append(parts, fmt.Sprintf("%d in %s (%s)", len(files), homeTilde(dir), listFiles(files)))
		}
	}
	if dir := codexMemoryDir(); dir != "" {
		if files := unimportedCodexMemoryFiles(dir, projectNames(scope)); len(files) > 0 {
			parts = append(parts, fmt.Sprintf("%d in Codex's %s that name this project (%s)", len(files), homeTilde(dir), listFiles(files)))
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return fmt.Sprintf("shikii: the agent's memory has files not yet moved to the wiki: %s. When you finish the current request, follow the steps in the skill's references/import-memory.md: keep each file as a source, move it into lessons one rule at a time, and turn each file into one line that points at its wiki page (canonical: .shikii/wiki <page id>) (for files shared by all projects, only add the pointing line; deleting is the person's job).", strings.Join(parts, "; "))
}

// homeTilde writes a path under home with ~ (for display).
func homeTilde(p string) string {
	if home, err := os.UserHomeDir(); err == nil && strings.HasPrefix(p, home+string(filepath.Separator)) {
		return "~" + p[len(home):]
	}
	return p
}
