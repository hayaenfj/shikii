package notebook

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func readViewFixture(t *testing.T) (Book, Page, string) {
	t.Helper()
	dir := t.TempDir()
	b := Book{Root: filepath.Join(dir, ".shikii"), Scope: dir}
	o, err := b.Capture(Observation{Kind: "test", Title: "Read view source", Content: "Source fixture", Reference: "synthetic", Version: "1", ObservedAt: time.Unix(1, 0)})
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, "routing.txt"), []byte("tenant-one"), 0600); err != nil {
		t.Fatal(err)
	}
	d := draft(o.ID, "Read view routing")
	d.Kind = "howto"
	body := "The owner choice stays unresolved."
	d.Body = &body
	watch := []string{"routing.txt"}
	d.Watch = &watch
	d.Learning = &Learning{Practice: &KnowledgePractice{Steps: []PracticeStep{{Condition: "Synthetic routing", Action: "Check the source", Check: "Observe the owner", Avoid: "Do not publish", Sources: []string{o.ID}}}}}
	saved, err := b.Ingest(d)
	if err != nil {
		t.Fatal(err)
	}
	return b.ReadView(), *saved.Page, o.ID
}

func TestReadViewRechecksBytesSourcesAndPremises(t *testing.T) {
	b, p, source := readViewFixture(t)
	pageDir, _ := b.directory(false, "pages")
	sourceDir, _ := b.directory(false, "sources")
	for _, tc := range []struct{ name, path, old, next string }{
		{"article", filepath.Join(pageDir, p.ID+".md"), "The owner choice stays unresolved.", "The owner choice stays determined."},
		{"source", filepath.Join(sourceDir, source+".json"), "Source fixture", "Source changed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, ok, e := b.CurrentPage(p.ID); e != nil || !ok {
				t.Fatal("warm current article", ok, e)
			}
			raw, err := os.ReadFile(tc.path)
			if err != nil {
				t.Fatal(err)
			}
			info, _ := os.Stat(tc.path)
			changed := bytes.Replace(raw, []byte(tc.old), []byte(tc.next), 1)
			if bytes.Equal(changed, raw) || len(changed) != len(raw) {
				t.Fatal("fixture must change bytes without changing size")
			}
			if err = os.WriteFile(tc.path, changed, 0600); err != nil {
				t.Fatal(err)
			}
			if err = os.Chtimes(tc.path, info.ModTime(), info.ModTime()); err != nil {
				t.Fatal(err)
			}
			if _, ok, e := b.CurrentPage(p.ID); e != nil || ok {
				t.Fatal("cached parsing must not hide changed bytes", ok, e)
			}
			if err = os.WriteFile(tc.path, raw, 0600); err != nil {
				t.Fatal(err)
			}
			if _, ok, e := b.CurrentPage(p.ID); e != nil || !ok {
				t.Fatal("restored original remains readable", ok, e)
			}
		})
	}
	if err := os.WriteFile(filepath.Join(b.Scope, "routing.txt"), []byte("tenant-two"), 0600); err != nil {
		t.Fatal(err)
	}
	current, ok, err := b.CurrentPage(p.ID)
	if err != nil || !ok || len(current.Recheck) != 1 || current.Recheck[0].State != "changed" {
		t.Fatal("watched premises stay live", current.Recheck, ok, err)
	}
}

func TestReadViewDoesNotAliasReturnedLearning(t *testing.T) {
	b, p, _ := readViewFixture(t)
	for range 2 {
		got, ok, err := b.CurrentPage(p.ID)
		if err != nil || !ok {
			t.Fatal(ok, err)
		}
		got.Sources[0].Hash = "changed"
		got.Learning.Practice.Steps[0].Sources[0] = "changed"
		got.Learning.Practice.Steps[0].Action = "changed"
	}
	got, ok, err := b.CurrentPage(p.ID)
	if err != nil || !ok || got.Sources[0].Hash != p.Sources[0].Hash || got.Learning.Practice.Steps[0].Action != p.Learning.Practice.Steps[0].Action || got.Learning.Practice.Steps[0].Sources[0] != p.Learning.Practice.Steps[0].Sources[0] {
		t.Fatal("returned metadata must not modify the parsed read view", ok, err)
	}
}

func TestReadViewRefreshesRevisionAndStoreBinding(t *testing.T) {
	b, p, source := readViewFixture(t)
	if _, ok, err := b.CurrentPage(p.ID); err != nil || !ok {
		t.Fatal(ok, err)
	}
	d := draft(source, p.Title)
	d.Kind = "howto"
	d.ID = p.ID
	d.Lesson = "A new explicit owner condition."
	saved, err := b.Ingest(d)
	if err != nil {
		t.Fatal(err)
	}
	current, ok, err := b.CurrentPage(p.ID)
	if err != nil || !ok || current.Revision != saved.Page.Revision || current.Lesson != d.Lesson {
		t.Fatal("the next operation must read the new revision", ok, err)
	}
	namePath := filepath.Join(b.Root, "records", "store-name")
	raw, err := os.ReadFile(namePath)
	if err != nil {
		t.Fatal(err)
	}
	other := strings.Repeat("a", 64)
	if strings.TrimSpace(string(raw)) == other {
		other = strings.Repeat("b", 64)
	}
	if err = os.WriteFile(namePath, []byte(other+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, ok, err = b.CurrentPage(p.ID); err != nil || ok {
		t.Fatal("a read view must not retain the prior namespace across operations", ok, err)
	}
	if err = os.WriteFile(namePath, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if _, ok, err = b.CurrentPage(p.ID); err != nil || !ok {
		t.Fatal("restored namespace remains readable", ok, err)
	}
}

func TestReadViewRejectsLinkAfterWarmRead(t *testing.T) {
	b, p, _ := readViewFixture(t)
	if _, ok, e := b.CurrentPage(p.ID); e != nil || !ok {
		t.Fatal(ok, e)
	}
	dir, _ := b.directory(false, "pages")
	path := filepath.Join(dir, p.ID+".md")
	saved := filepath.Join(t.TempDir(), "article.md")
	if err := os.Rename(path, saved); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(saved, path); err != nil {
		t.Fatal(err)
	}
	if _, ok, e := b.CurrentPage(p.ID); e != nil || ok {
		t.Fatal("a cached article must still reject a symbolic link", ok, e)
	}
}
