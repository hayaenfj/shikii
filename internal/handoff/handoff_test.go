package handoff

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func book(t *testing.T) (Book, string) {
	t.Helper()
	store := filepath.Join(t.TempDir(), ".shikii")
	b := Book{Store: store}
	if err := b.Ensure(); err != nil {
		t.Fatal(err)
	}
	return b, filepath.Join(store, Dir)
}

// 置き場の外・隠し名・.. を含むパスは断る(書きも読みも)。
func TestCleanRefusesPathsOutsideTheFolder(t *testing.T) {
	for _, bad := range []string{"", "/etc/passwd", "../x.md", "a/../../b.md", "a//b.md", "./a.md", ".hidden", "a/.git/config", "a\\b.md", strings.Repeat("a", 300)} {
		if _, err := Clean(bad); err == nil {
			t.Errorf("must refuse %q", bad)
		}
	}
	for _, good := range []string{"audit/report-A.md", "release-0.8.1/notes.md", "画像/前.png"} {
		if got, err := Clean(good); err != nil || got != good {
			t.Errorf("must accept %q: %q %v", good, got, err)
		}
	}
}

// 書いて、足して、読む。親のフォルダは作る。一時ファイルは残らない。
func TestWriteAppendAndRead(t *testing.T) {
	b, dir := book(t)
	if _, err := b.Write("audit/report.md", "# 監査\n", false); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Write("audit/report.md", "- 指摘 1\n", true); err != nil {
		t.Fatal(err)
	}
	f, err := b.Read("audit/report.md")
	if err != nil || f.Content != "# 監査\n- 指摘 1\n" || f.Kind != "text" {
		t.Fatalf("read back: %+v %v", f, err)
	}
	if _, err := b.Write("audit/report.md", "置き換え\n", false); err != nil {
		t.Fatal(err)
	}
	if f, _ := b.Read("audit/report.md"); f.Content != "置き換え\n" {
		t.Fatalf("write must replace: %q", f.Content)
	}
	ents, _ := os.ReadDir(filepath.Join(dir, "audit"))
	if len(ents) != 1 {
		t.Fatalf("no temporary file may remain: %v", ents)
	}
	if _, err := b.Write("../outside.md", "x", false); err == nil {
		t.Fatal("must not write outside the folder")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(dir), "outside.md")); err == nil {
		t.Fatal("a file was written outside the folder")
	}
	if _, err := b.Write("big.md", strings.Repeat("a", writeMax+1), false); err == nil {
		t.Fatal("must refuse more than the write limit")
	}
	if _, err := b.Write("bin.md", "\xff\xfe", false); err == nil {
		t.Fatal("must refuse text that is not UTF-8")
	}
}

// 一覧は全部のファイル(件数で切らない)、リンクと隠し名は出さない。リンクの先は読まない。
func TestListSkipsLinksAndHiddenAndReadRefusesLinks(t *testing.T) {
	b, dir := book(t)
	outside := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(outside, []byte("秘密"), 0o600); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 120; i++ {
		if _, err := b.Write(filepath.ToSlash(filepath.Join("many", strings.Repeat("x", 1)+string(rune('a'+i%26))+string(rune('a'+i/26))+".md")), "x", false); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "shot.png"), []byte("\x89PNG"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".hidden.md"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "link.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Dir(outside), filepath.Join(dir, "linkdir")); err != nil {
		t.Fatal(err)
	}
	list, err := b.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 121 {
		t.Fatalf("all 120 notes and the image, nothing else: %d", len(list))
	}
	kinds := map[string]string{}
	for _, e := range list {
		kinds[e.Path] = e.Kind
		if strings.Contains(e.Path, "link") || strings.Contains(e.Path, "hidden") || strings.Contains(e.Path, "secret") {
			t.Fatalf("links and hidden files must not be listed: %s", e.Path)
		}
	}
	if kinds["shot.png"] != "image" {
		t.Fatalf("png is an image: %v", kinds["shot.png"])
	}
	if f, err := b.Read("link.txt"); err == nil || strings.Contains(f.Content, "秘密") {
		t.Fatalf("a link must not be read: %+v %v", f, err)
	}
	if f, err := b.Read("linkdir/secret.txt"); err == nil || strings.Contains(f.Content, "秘密") {
		t.Fatalf("a linked folder must not be read: %+v %v", f, err)
	}
	if _, err := b.Write("link.txt", "上書き", false); err == nil {
		t.Fatal("must not write through a link")
	}
	if data, _ := os.ReadFile(outside); string(data) != "秘密" {
		t.Fatal("the file behind the link was changed")
	}
	// 置き場の中を指すリンクも辿らない(外へ向くリンクは os.Root が断るので、決まりはこちらで確かめる)
	if err := os.Symlink("shot.png", filepath.Join(dir, "inner-link.png")); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Read("inner-link.png"); err == nil {
		t.Fatal("a link inside the folder must not be followed either")
	}
	if f, err := b.Read("shot.png"); err != nil || f.Content != "" || f.Kind != "image" {
		t.Fatalf("an image is not returned as text: %+v %v", f, err)
	}
}

// 置き場が無いプロジェクトでも一覧は空で、誤りにしない。
func TestListWithoutFolderIsEmpty(t *testing.T) {
	list, err := (Book{Store: filepath.Join(t.TempDir(), ".shikii")}).List()
	if err != nil || len(list) != 0 {
		t.Fatalf("%v %v", list, err)
	}
}
