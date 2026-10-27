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

// Paths outside the place, with hidden names, or containing .. are refused (both writing and reading).
func TestCleanRefusesPathsOutsideTheFolder(t *testing.T) {
	for _, bad := range []string{"", "/etc/passwd", "../x.md", "a/../../b.md", "a//b.md", "./a.md", ".hidden", "a/.git/config", "a\\b.md", strings.Repeat("a", 300)} {
		if _, err := Clean(bad); err == nil {
			t.Errorf("must refuse %q", bad)
		}
	}
	// The form written in a source's reference (.shikii/handoff/<path>) is accepted as is too (audit 0.8.1-3)
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

// Write, append, read. Parent folders are created. No temp files are left.
func TestWriteAppendAndRead(t *testing.T) {
	b, dir := book(t)
	if _, err := b.Write("audit/report.md", "# Audit\n", false); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Write("audit/report.md", "- Finding 1\n", true); err != nil {
		t.Fatal(err)
	}
	f, err := b.Read("audit/report.md")
	if err != nil || f.Content != "# Audit\n- Finding 1\n" || f.Kind != "text" {
		t.Fatalf("read back: %+v %v", f, err)
	}
	if _, err := b.Write("audit/report.md", "replaced\n", false); err != nil {
		t.Fatal(err)
	}
	if f, _ := b.Read("audit/report.md"); f.Content != "replaced\n" {
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

// The list holds every file (no limit on the count); links and hidden names are not listed. Link targets are not read.
func TestListSkipsLinksAndHiddenAndReadRefusesLinks(t *testing.T) {
	b, dir := book(t)
	outside := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(outside, []byte("secret"), 0o600); err != nil {
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
	if f, err := b.Read("link.txt"); err == nil || strings.Contains(f.Content, "secret") {
		t.Fatalf("a link must not be read: %+v %v", f, err)
	}
	if f, err := b.Read("linkdir/secret.txt"); err == nil || strings.Contains(f.Content, "secret") {
		t.Fatalf("a linked folder must not be read: %+v %v", f, err)
	}
	if _, err := b.Write("link.txt", "overwrite", false); err == nil {
		t.Fatal("must not write through a link")
	}
	if data, _ := os.ReadFile(outside); string(data) != "secret" {
		t.Fatal("the file behind the link was changed")
	}
	// Links pointing inside the place are not followed either (os.Root refuses links pointing outside, so this rule is checked here)
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

// Even in a project without the place, the list is empty and not an error.
func TestListWithoutFolderIsEmpty(t *testing.T) {
	list, err := (Book{Store: filepath.Join(t.TempDir(), ".shikii")}).List()
	if err != nil || len(list) != 0 {
		t.Fatalf("%v %v", list, err)
	}
}

// Audit 0.8.1-1: with a fixed temp name for replacement, a temp-name link planted in advance would be followed to rewrite another file in the place,
// concurrent writes would mix, and with long names the temp name would exceed the limit and could not be written.
func TestReplaceUsesAFreshTemporaryName(t *testing.T) {
	b, dir := book(t)
	if err := os.WriteFile(filepath.Join(dir, "important.md"), []byte("important"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("important.md", filepath.Join(dir, ".write-a.md")); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Write("a.md", "overwrite", false); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(filepath.Join(dir, "important.md")); string(data) != "important" {
		t.Fatalf("another file was rewritten through a planted temporary link: %q", data)
	}
	long := strings.Repeat("長", 83) + ".md" // 252 bytes
	if _, err := b.Write(long, "x", false); err != nil {
		t.Fatalf("a long name accepted by Clean must be writable: %v", err)
	}
}

// Audit 0.8.1-1: two replaces of one item at once do not mix or fail. Checked in one run, without waiting on a race: while one write has
// filled its temp file and is about to rename it into place, the other write runs from start to end.
func TestReplaceWhileAnotherReplaceIsAboutToRename(t *testing.T) {
	b, dir := book(t)
	big, small := strings.Repeat("大", 100000), "小"
	t.Cleanup(func() { beforeReplace = func() {} })
	var inner error
	met := 0
	beforeReplace = func() {
		beforeReplace = func() {}
		met++
		_, inner = b.Write("race.md", small, false)
	}
	if _, err := b.Write("race.md", big, false); err != nil {
		t.Fatalf("the outer replace failed: %v", err)
	}
	if met != 1 || inner != nil {
		t.Fatalf("the other replace ran right before the rename and succeeded: met %d, %v", met, inner)
	}
	if f, _ := b.Read("race.md"); f.Content != big {
		t.Fatalf("the last rename wins with its whole contents: got %d bytes", len(f.Content))
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if isTempName(e.Name()) {
			t.Fatalf("a temp file was left: %s", e.Name())
		}
	}
}

// Audit 0.8.1-1: if the file is swapped after the check, it is not served. The size is that of the opened item.
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

// Audit 0.8.1-1: hard links, links in intermediate folders, a link for the place itself and names with control characters are not handled.
func TestHardLinksFolderLinksAndControlCharactersAreRefused(t *testing.T) {
	b, dir := book(t)
	outside := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(outside, []byte("outside secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(outside, filepath.Join(dir, "hard.txt")); err != nil {
		t.Fatal(err)
	}
	if f, err := b.Read("hard.txt"); err == nil || strings.Contains(f.Content, "secret") {
		t.Fatalf("a hard link to a file outside must not be read: %+v %v", f, err)
	}
	if _, err := b.Write("hard.txt", "append", true); err == nil {
		t.Fatal("a hard link must not be appended to")
	}
	if data, _ := os.ReadFile(outside); string(data) != "outside secret" {
		t.Fatalf("the file outside was changed: %q", data)
	}
	if _, err := b.Write("audit/r.md", "inside", false); err != nil {
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

// Audit 0.8.1-1 item 14: temp files (.write-*) left by a crash during a rewrite are cleaned up the next time that folder is written,
// only those older than one day (other temp files being written and regular files are not touched).
func TestWriteRemovesStaleTemporaryFiles(t *testing.T) {
	b, dir := book(t)
	if err := os.MkdirAll(filepath.Join(dir, "job"), 0o700); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-48 * time.Hour)
	stale := filepath.Join(dir, "job", ".write-00aa11bb22cc33dd")
	fresh := filepath.Join(dir, "job", ".write-00bb11cc22dd33ee")
	keep := filepath.Join(dir, "job", ".notes")
	human := filepath.Join(dir, "job", ".write-up.md")     // hidden files not in the temp-name form (.write- and 16 hex digits) belong to a person
	other := filepath.Join(dir, ".write-00cc11dd22ee33ff") // other folders are not touched
	for _, p := range []string{stale, fresh, keep, other, human} {
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for _, p := range []string{stale, keep, other, human} {
		os.Chtimes(p, old, old)
	}
	if _, err := b.Write("job/a.md", "body", false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(stale); !os.IsNotExist(err) {
		t.Error("temp files older than one day must be cleaned up")
	}
	for _, p := range []string{fresh, keep, other, human} {
		if _, err := os.Lstat(p); err != nil {
			t.Errorf("%s must be kept: %v", p, err)
		}
	}
}

// Audit 0.8.2-1 item 4: links and folders with temp-file names are not deleted, and neither the link targets nor anything outside the place a link points to is touched.
func TestStaleTemporaryCleanupLeavesLinksFoldersAndOutside(t *testing.T) {
	b, dir := book(t)
	old := time.Now().Add(-48 * time.Hour)
	if err := os.MkdirAll(filepath.Join(dir, "job"), 0o700); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	victim := filepath.Join(outside, ".write-0011223344556677")
	target := filepath.Join(outside, "target.md")
	for _, p := range []string{victim, target} {
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		os.Chtimes(p, old, old)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "out")); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "job", ".write-aaaaaaaaaaaaaaaa")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	folder := filepath.Join(dir, "job", ".write-bbbbbbbbbbbbbbbb")
	if err := os.Mkdir(folder, 0o700); err != nil {
		t.Fatal(err)
	}
	os.Chtimes(folder, old, old)
	r, err := b.open()
	if err != nil {
		t.Fatal(err)
	}
	removeStaleTemps(r, "out") // does not go outside even when given a folder link pointing outside the place
	r.Close()
	if _, err := b.Write("job/a.md", "x", false); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{victim, target, link, folder} {
		if _, err := os.Lstat(p); err != nil {
			t.Errorf("%s must be kept: %v", p, err)
		}
	}
}
