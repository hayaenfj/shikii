package handoff

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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
	// 原資料の reference に書いた形(.shikii/handoff/<パス>)もそのまま受ける(監査 0.8.1-3)
	if got, err := Clean(".shikii/handoff/audit/report-B.md"); err != nil || got != "audit/report-B.md" {
		t.Errorf("a path written as a source reference must be accepted: %q %v", got, err)
	}
	if _, err := Clean(".shikii/handoff/../x.md"); err == nil {
		t.Error("the prefix must not open a way out")
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

// 監査 0.8.1-1: 置き換えの一時名が固定だと、前もって置かれた一時名のリンクを辿って置き場の中の別のファイルを書き換え、
// 同時の書き込みが混ざり、長い名前は一時名が上限を超えて書けない。
func TestReplaceUsesAFreshTemporaryName(t *testing.T) {
	b, dir := book(t)
	if err := os.WriteFile(filepath.Join(dir, "important.md"), []byte("大事"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("important.md", filepath.Join(dir, ".write-a.md")); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Write("a.md", "上書き", false); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(filepath.Join(dir, "important.md")); string(data) != "大事" {
		t.Fatalf("another file was rewritten through a planted temporary link: %q", data)
	}
	long := strings.Repeat("長", 83) + ".md" // 252 バイト
	if _, err := b.Write(long, "x", false); err != nil {
		t.Fatalf("a long name accepted by Clean must be writable: %v", err)
	}
	big, small := strings.Repeat("大", 100000), "小"
	for i := 0; i < 50; i++ {
		done := make(chan error, 2)
		go func() { _, err := b.Write("race.md", big, false); done <- err }()
		go func() { _, err := b.Write("race.md", small, false); done <- err }()
		for j := 0; j < 2; j++ {
			if err := <-done; err != nil {
				t.Fatalf("round %d: a concurrent replace failed: %v", i, err)
			}
		}
		if f, _ := b.Read("race.md"); f.Content != big && f.Content != small {
			t.Fatalf("round %d: the contents were mixed (%d bytes)", i, len(f.Content))
		}
	}
}

// 監査 0.8.1-1: 確かめた後に差し替わったら配らない。大きさは開いた物のもの。
func TestOpenRefusesAFileSwappedAfterTheCheck(t *testing.T) {
	b, dir := book(t)
	if _, err := b.Write("swap.md", strings.Repeat("a", 30), false); err != nil {
		t.Fatal(err)
	}
	beforeOpen = func() {
		_ = os.WriteFile(filepath.Join(dir, "other.md"), []byte(strings.Repeat("b", 300)), 0o600)
		_ = os.Rename(filepath.Join(dir, "other.md"), filepath.Join(dir, "swap.md"))
	}
	defer func() { beforeOpen = nil }()
	f, e, err := b.Open("swap.md")
	if err == nil {
		defer f.Close()
		st, _ := f.Stat()
		t.Fatalf("a swapped file must not be served: entry size %d, opened %d", e.Size, st.Size())
	}
}

// 監査 0.8.1-1: ハードリンク・途中のフォルダのリンク・置き場そのもののリンク・制御文字の名前は扱わない。
func TestHardLinksFolderLinksAndControlCharactersAreRefused(t *testing.T) {
	b, dir := book(t)
	outside := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(outside, []byte("外の秘密"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(outside, filepath.Join(dir, "hard.txt")); err != nil {
		t.Fatal(err)
	}
	if f, err := b.Read("hard.txt"); err == nil || strings.Contains(f.Content, "秘密") {
		t.Fatalf("a hard link to a file outside must not be read: %+v %v", f, err)
	}
	if _, err := b.Write("hard.txt", "追記", true); err == nil {
		t.Fatal("a hard link must not be appended to")
	}
	if data, _ := os.ReadFile(outside); string(data) != "外の秘密" {
		t.Fatalf("the file outside was changed: %q", data)
	}
	if _, err := b.Write("audit/r.md", "中", false); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("audit", filepath.Join(dir, "indir")); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Read("indir/r.md"); err == nil {
		t.Fatal("a folder link inside the folder must not be followed when reading")
	}
	if _, err := b.Write("indir/new.md", "x", false); err == nil {
		t.Fatal("a folder link inside the folder must not be followed when writing")
	}
	for _, bad := range []string{"a\nb.md", "a\tb.md", "evil‮gnp.md", "a‏b.md"} {
		if _, err := Clean(bad); err == nil {
			t.Errorf("control and bidi characters must be refused: %q", bad)
		}
	}
	store := filepath.Join(t.TempDir(), ".shikii")
	if err := os.MkdirAll(store, 0o700); err != nil {
		t.Fatal(err)
	}
	elsewhere := t.TempDir()
	if err := os.Symlink(elsewhere, filepath.Join(store, Dir)); err != nil {
		t.Fatal(err)
	}
	if _, err := (Book{Store: store}).Write("x.md", "x", false); err == nil {
		t.Fatal("the folder itself must not be a link")
	}
	if ents, _ := os.ReadDir(elsewhere); len(ents) != 0 {
		t.Fatalf("a file was written through the folder link: %v", ents)
	}
}

// 監査 0.8.1-1 の 14: 書き換えの途中で落ちて残った一時ファイル(.write-*)は、次にそのフォルダへ書く時に、
// 1 日より古いものだけ片付ける(書き込み中の他の一時ファイルや、ふつうのファイルには触らない)。
func TestWriteRemovesStaleTemporaryFiles(t *testing.T) {
	b, dir := book(t)
	if err := os.MkdirAll(filepath.Join(dir, "job"), 0o700); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-48 * time.Hour)
	stale := filepath.Join(dir, "job", ".write-00aa")
	fresh := filepath.Join(dir, "job", ".write-00bb")
	keep := filepath.Join(dir, "job", ".notes")
	other := filepath.Join(dir, ".write-00cc") // 別のフォルダは触らない
	for _, p := range []string{stale, fresh, keep, other} {
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for _, p := range []string{stale, keep, other} {
		os.Chtimes(p, old, old)
	}
	if _, err := b.Write("job/a.md", "本文", false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(stale); !os.IsNotExist(err) {
		t.Error("1 日より古い一時ファイルを片付ける")
	}
	for _, p := range []string{fresh, keep, other} {
		if _, err := os.Lstat(p); err != nil {
			t.Errorf("%s は残す: %v", p, err)
		}
	}
}
