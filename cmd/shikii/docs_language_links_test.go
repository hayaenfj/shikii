package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// A Japanese document (*.ja.md, docs/readme/README_ja.md, docs/guide.ja.md) links the Japanese version of a document when there is one,
// and the English guide does not call an English reference "(Japanese)" (audit 0.9.2 translation, low 6).
func TestJapaneseDocumentsLinkJapaneseVersions(t *testing.T) {
	repo := filepath.Join("..", "..")
	link := regexp.MustCompile(`\]\(([^)#\s]+\.md)(#[^)]*)?\)`)
	var docs []string
	_ = filepath.WalkDir(repo, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() && (d.Name() == "node_modules" || strings.HasPrefix(d.Name(), ".")) && path != repo {
			return filepath.SkipDir
		}
		if strings.HasSuffix(path, ".ja.md") || strings.HasSuffix(path, "README_ja.md") {
			docs = append(docs, path)
		}
		return nil
	})
	if len(docs) < 10 {
		t.Fatalf("found only %d Japanese documents", len(docs))
	}
	for _, doc := range docs {
		b, err := os.ReadFile(doc)
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(b), "\n") {
			for _, m := range link.FindAllStringSubmatch(line, -1) {
				target := m[1]
				if strings.Contains(target, "://") || strings.HasSuffix(target, ".ja.md") || strings.HasSuffix(target, "README_ja.md") {
					continue
				}
				ja := strings.TrimSuffix(target, ".md") + ".ja.md"
				if _, err := os.Stat(filepath.Join(filepath.Dir(doc), ja)); err == nil {
					if strings.Contains(line, "English") || strings.Contains(line, "\u82f1\u8a9e") { // the language switcher names the English version on purpose
						continue
					}
					t.Errorf("%s:%d links %s although %s exists", doc, i+1, target, ja)
				}
			}
		}
	}
	guide, err := os.ReadFile(filepath.Join(repo, "docs", "guide.md"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(guide), "references/review.md) (Japanese)") {
		t.Error("docs/guide.md calls references/review.md Japanese, but it is English")
	}
}
