package notebook

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadSnapshotSharesReadsAndRevalidates(t *testing.T) {
	b, p, _ := readViewFixture(t)
	view := b.ReadSnapshot()
	if _, e := view.Query("lesson:" + p.ID); e != nil {
		t.Fatal(e)
	}
	path := filepath.Join(b.Scope, "routing.txt")
	reads := 0
	reader := func(path string) ([]byte, error) { reads++; return os.ReadFile(path) }
	for range 3 {
		raw, e := view.sharedRead(path, reader)
		if e != nil || string(raw) != "tenant-one" {
			t.Fatal(string(raw), e)
		}
	}
	if reads != 1 {
		t.Fatal("one preparation must share the original read", reads)
	}
	if e := view.ValidateRead(); e != nil {
		t.Fatal(e)
	}
	if reads != 2 {
		t.Fatal("commit validation must actually reopen the file", reads)
	}
	raw, _ := os.ReadFile(path)
	info, _ := os.Stat(path)
	changed := bytes.Replace(raw, []byte("one"), []byte("two"), 1)
	if e := os.WriteFile(path, changed, 0600); e != nil {
		t.Fatal(e)
	}
	if e := os.Chtimes(path, info.ModTime(), info.ModTime()); e != nil {
		t.Fatal(e)
	}
	if e := view.ValidateRead(); e == nil {
		t.Fatal("same-size, same-time changes must reject the prepared result")
	}
	t.Run("unavailable-read", func(t *testing.T) {
		b, _, _ := readViewFixture(t)
		view := b.ReadSnapshot()
		path := filepath.Join(b.Scope, "missing.txt")
		if _, e := view.sharedRead(path, os.ReadFile); !os.IsNotExist(e) {
			t.Fatal(e)
		}
		if e := view.ValidateRead(); e != nil {
			t.Fatal("an unchanged missing file must remain readable as missing", e)
		}
		if e := os.WriteFile(path, []byte("created"), 0600); e != nil {
			t.Fatal(e)
		}
		if e := view.ValidateRead(); e == nil {
			t.Fatal("recovering an unavailable file changes the prepared evidence")
		}
	})
}

func TestReadSnapshotRejectsChangedKnowledgeAtCommit(t *testing.T) {
	for _, kind := range []string{"article", "source", "premise", "namespace", "namespace-link", "addition", "link", "history-link", "missing-pages", "missing-sources", "missing-source"} {
		t.Run(kind, func(t *testing.T) {
			b, p, source := readViewFixture(t)
			pages, _ := b.directory(false, "pages")
			sources, _ := b.directory(false, "sources")
			missingPath := ""
			switch kind {
			case "missing-pages":
				missingPath = pages
			case "missing-sources":
				missingPath = sources
			case "missing-source":
				missingPath = filepath.Join(sources, source+".json")
			}
			backup := filepath.Join(t.TempDir(), "unavailable")
			if missingPath != "" {
				if e := os.Rename(missingPath, backup); e != nil {
					t.Fatal(e)
				}
			}
			view := b.ReadSnapshot()
			if _, e := view.Query(""); e != nil {
				t.Fatal(e)
			}
			if _, ok, e := view.CurrentPage(p.ID); e != nil || ok != (missingPath == "") {
				t.Fatal(ok, e)
			}
			if kind == "history-link" {
				if _, e := view.Query("lesson:" + p.ID + "@" + p.Revision); e != nil {
					t.Fatal(e)
				}
			}
			if e := view.ValidateRead(); e != nil {
				t.Fatal(e)
			}
			path := filepath.Join(pages, p.ID+".md")
			switch kind {
			case "article", "source":
				old, next := "The owner choice stays unresolved.", "The owner choice stays determined."
				if kind == "source" {
					path = filepath.Join(sources, source+".json")
					old, next = "Source fixture", "Source changed"
				}
				raw, e := os.ReadFile(path)
				if e != nil {
					t.Fatal(e)
				}
				info, _ := os.Stat(path)
				changed := bytes.Replace(raw, []byte(old), []byte(next), 1)
				if bytes.Equal(raw, changed) || len(raw) != len(changed) {
					t.Fatal("fixture must change bytes while retaining size")
				}
				if e = os.WriteFile(path, changed, 0600); e != nil {
					t.Fatal(e)
				}
				if e = os.Chtimes(path, info.ModTime(), info.ModTime()); e != nil {
					t.Fatal(e)
				}
			case "premise":
				if e := os.WriteFile(filepath.Join(b.Scope, "routing.txt"), []byte("tenant-two"), 0600); e != nil {
					t.Fatal(e)
				}
			case "namespace":
				name := filepath.Join(b.Root, "records", "store-name")
				raw, _ := os.ReadFile(name)
				other := strings.Repeat("a", 64)
				if strings.TrimSpace(string(raw)) == other {
					other = strings.Repeat("b", 64)
				}
				if e := os.WriteFile(name, []byte(other+"\n"), 0600); e != nil {
					t.Fatal(e)
				}
			case "namespace-link":
				name := filepath.Join(b.Root, "records", "store-name")
				raw, e := os.ReadFile(name)
				if e != nil {
					t.Fatal(e)
				}
				target := filepath.Join(t.TempDir(), "name")
				if e = os.WriteFile(target, raw, 0600); e != nil {
					t.Fatal(e)
				}
				if e = os.Remove(name); e != nil {
					t.Fatal(e)
				}
				if e = os.Symlink(target, name); e != nil {
					t.Fatal(e)
				}
			case "addition":
				if _, e := b.Ingest(draft(source, "New current rule")); e != nil {
					t.Fatal(e)
				}
			case "link":
				target := filepath.Join(t.TempDir(), "page.md")
				if e := os.Rename(path, target); e != nil {
					t.Fatal(e)
				}
				if e := os.Symlink(target, path); e != nil {
					t.Fatal(e)
				}
			case "history-link":
				history := filepath.Join(pages, "revisions")
				target := filepath.Join(t.TempDir(), "revisions")
				if e := os.Rename(history, target); e != nil {
					t.Fatal(e)
				}
				if e := os.Symlink(target, history); e != nil {
					t.Fatal(e)
				}
			case "missing-pages", "missing-sources", "missing-source":
				if e := os.Rename(backup, missingPath); e != nil {
					t.Fatal(e)
				}
			}
			if e := view.ValidateRead(); e == nil {
				t.Fatal("changed knowledge must be rejected before the context is saved", kind)
			}
		})
	}
}
