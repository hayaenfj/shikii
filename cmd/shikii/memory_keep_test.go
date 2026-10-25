package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 0.9.2: moving a memory into the wiki keeps the original memory files (the owner: keep the original memory files when the person asks to move them).
// The notice asks to keep each file's body and add one pointing line, never to replace the body; a file kept that way counts as moved.
func TestMemoryImportKeepsTheOriginalFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "MEMORY.md"), []byte("- [Rule](rule.md)\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	body := "---\nname: rule\ndescription: a rule\nmetadata:\n  type: feedback\n---\n\nAlways run the tests before committing.\n\n**Why:** the person said so.\n"
	if err := os.WriteFile(filepath.Join(dir, "rule.md"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := unimportedMemoryFiles(dir); len(got) != 1 {
		t.Fatalf("a file without the pointing line is not moved yet: %v", got)
	}
	// moved as the steps say: the body stays, one line right after the frontmatter points at the page
	kept := strings.Replace(body, "---\n\n", "---\n\ncanonical: .shikii/wiki 0123456789abcdef\n\n", 1)
	if err := os.WriteFile(filepath.Join(dir, "rule.md"), []byte(kept), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := unimportedMemoryFiles(dir); len(got) != 0 {
		t.Fatalf("a file that keeps its body and has the pointing line counts as moved: %v", got)
	}
	notice := memoryImportText([]string{"rule.md"})
	if !strings.Contains(notice, "keep each file's body") || strings.Contains(notice, "turn each file into one line") {
		t.Fatalf("the notice asks to keep the body and add a line, not to replace the body: %s", notice)
	}
	steps, err := os.ReadFile(filepath.Join("..", "..", "skills", "shikii", "references", "import-memory.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(steps), "Keep every memory file and its body as it is") || strings.Contains(string(steps), "Replace each project-only file's body") {
		t.Fatalf("the steps keep the original memory files")
	}
}
