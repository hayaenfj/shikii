package handoff

import (
	"fmt"
	"io/fs"
	"math/rand"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

// referenceList is what List was before the listing read the folders by path and from several goroutines: every file of the place
// through the os.Root of the place, newest update first. The listing must return what this does, on every tree.
func referenceList(t *testing.T, b Book) []Entry {
	t.Helper()
	r, err := b.open()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	out := []Entry{}
	err = fs.WalkDir(r.FS(), ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p == "." {
			return nil
		}
		if strings.HasPrefix(d.Name(), ".") {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.Type()&fs.ModeSymlink != 0 || !d.Type().IsRegular() && !d.IsDir() {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		out = append(out, Entry{Path: p, Size: info.Size(), Modified: info.ModTime().UTC(), Kind: KindOf(p)})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].Modified.Equal(out[j].Modified) {
			return out[i].Modified.After(out[j].Modified)
		}
		return out[i].Path < out[j].Path
	})
	return out
}

// shape counts what a generated tree holds of each kind that the listing treats on its own, so that a test can say the kind was there.
type shape struct{ files, hiddenFiles, hiddenFolders, linkedFiles, linkedFolders, pipes, tied, deep int }

// randomPlace fills the place of b with a tree made from seed: folders nested to 6 levels, files of every kind of name (records, data,
// images, no extension, dots in a folder's name), hidden names, links to a file and to a folder, a named pipe, and modification times
// taken from a few values so that many files tie. It returns what it made.
func randomPlace(t *testing.T, dir string, seed int64) shape {
	t.Helper()
	rng := rand.New(rand.NewSource(seed))
	var s shape
	exts := []string{".md", ".txt", "", ".json", ".log", ".png", ".go", ".bin", ".MD", ".Txt"}
	folders := []string{dir}
	for i := 0; i < 40; i++ {
		parent := folders[rng.Intn(len(folders))]
		name := fmt.Sprintf("f%d", i)
		switch rng.Intn(4) {
		case 0:
			name = fmt.Sprintf("release-0.%d.1", i) // a folder whose name looks like a file with an extension
		case 1:
			name = "." + name
			s.hiddenFolders++
		}
		sub := filepath.Join(parent, name)
		if err := os.MkdirAll(sub, 0o755); err != nil {
			t.Fatal(err)
		}
		folders = append(folders, sub)
		if depth := strings.Count(strings.TrimPrefix(sub, dir), string(filepath.Separator)); depth > s.deep {
			s.deep = depth
		}
	}
	base := time.Now().Add(-100 * time.Hour).Truncate(time.Second)
	times := map[time.Time]int{}
	for i := 0; i < 400; i++ {
		folder := folders[rng.Intn(len(folders))]
		name := fmt.Sprintf("n%d%s", i, exts[rng.Intn(len(exts))])
		if rng.Intn(15) == 0 {
			name = "." + name
			s.hiddenFiles++
		}
		p := filepath.Join(folder, name)
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		at := base.Add(time.Duration(rng.Intn(12)) * time.Hour) // twelve values for 400 files: many tie
		if err := os.Chtimes(p, at, at); err != nil {
			t.Fatal(err)
		}
		times[at]++
		s.files++
	}
	for _, n := range times {
		if n > 1 {
			s.tied += n
		}
	}
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.md"), []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 6; i++ {
		folder := folders[rng.Intn(len(folders))]
		if err := os.Symlink(filepath.Join(outside, "secret.md"), filepath.Join(folder, fmt.Sprintf("link%d.md", i))); err != nil {
			t.Fatal(err)
		}
		s.linkedFiles++
		if err := os.Symlink(outside, filepath.Join(folder, fmt.Sprintf("linkdir%d", i))); err != nil {
			t.Fatal(err)
		}
		s.linkedFolders++
	}
	if err := syscall.Mkfifo(filepath.Join(folders[rng.Intn(len(folders))], "pipe.md"), 0o644); err != nil {
		t.Fatal(err)
	}
	s.pipes++
	return s
}

func isRecord(p string) bool {
	ext := strings.ToLower(filepath.Ext(p))
	return ext == ".md" || ext == ".txt" || ext == ""
}

// On every tree the listing is what it was, and Newest is the first n of it among the files want accepts, for every n and both
// ways of asking: this is what lets the session's brief stop listing the whole place. The trees are made to hold each thing the
// listing treats on its own, and the test says each was there (a "same on every tree" over trees without links proves nothing about links).
func TestListAndNewestAreTheReferenceOnEveryTree(t *testing.T) {
	var all shape
	for seed := int64(1); seed <= 10; seed++ {
		b, dir := book(t)
		s := randomPlace(t, dir, seed)
		all.files += s.files
		all.hiddenFiles += s.hiddenFiles
		all.hiddenFolders += s.hiddenFolders
		all.linkedFiles += s.linkedFiles
		all.linkedFolders += s.linkedFolders
		all.pipes += s.pipes
		all.tied += s.tied
		if s.deep > all.deep {
			all.deep = s.deep
		}
		want := referenceList(t, b)
		got, err := b.List()
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("seed %d: List differs from the reference (%d entries, want %d)", seed, len(got), len(want))
		}
		for _, n := range []int{1, 3, 7, 1000} {
			for name, accept := range map[string]func(string) bool{"records": isRecord, "all": func(string) bool { return true }, "none": func(string) bool { return false }} {
				var first []Entry
				for _, e := range want {
					if accept(e.Path) && len(first) < n {
						first = append(first, e)
					}
				}
				if first == nil {
					first = []Entry{}
				}
				found, complete, err := b.Newest(n, accept, time.Time{})
				if err != nil || !complete || !reflect.DeepEqual(found, first) {
					t.Fatalf("seed %d, n=%d, %s: Newest = %v (complete %v, err %v), want the first %d of the reference: %v", seed, n, name, found, complete, err, len(first), first)
				}
			}
		}
	}
	if all.files < 3000 || all.hiddenFiles == 0 || all.hiddenFolders == 0 || all.linkedFiles == 0 || all.linkedFolders == 0 || all.pipes == 0 || all.tied < 1000 || all.deep < 4 {
		t.Fatalf("premise: the trees must hold files, hidden names, links, pipes, ties and depth, they hold %+v", all)
	}
}

// A place with files of other kinds in the thousands: Newest asks for the information of the files want accepts, and of no other. The
// information of a file is an access to the disk (a lookup of its entry in a folder of tens of thousands), and a hook once spent 14 of
// its 15 seconds on it.
func TestNewestAsksForTheInformationOfTheWantedFilesOnly(t *testing.T) {
	b, dir := book(t)
	for i := 0; i < 300; i++ {
		if err := os.MkdirAll(filepath.Join(dir, "tool", fmt.Sprintf("run%d", i%20)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "tool", fmt.Sprintf("run%d", i%20), fmt.Sprintf("out%d.json", i)), []byte("{}"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, p := range []string{"plan/a.md", "plan/b.txt", "notes"} {
		if _, err := b.Write(p, "x", false); err != nil {
			t.Fatal(err)
		}
	}
	asked := atomic.Int64{}
	old := infoOf
	infoOf = func(d fs.DirEntry) (fs.FileInfo, error) { asked.Add(1); return d.Info() }
	t.Cleanup(func() { infoOf = old })

	found, complete, err := b.Newest(3, isRecord, time.Time{})
	if err != nil || !complete || len(found) != 3 {
		t.Fatalf("the three records: %v %v %v", found, complete, err)
	}
	if got := asked.Load(); got != 3 {
		t.Fatalf("the information of the 3 records and of none of the 300 data files was asked for, but %d were", got)
	}
	asked.Store(0)
	if list, err := b.List(); err != nil || len(list) != 303 || asked.Load() != 303 {
		t.Fatalf("List is the whole place, each file once: %d entries, %d asked, %v", len(list), asked.Load(), err)
	}
}

// The deadline: a deadline already past reads no folder and says so; one that passes while the top is being read leaves what the top
// held (the folders nearest the top are read first) and says so; no deadline reads all and says so.
func TestNewestPastItsDeadlineReturnsWhatTheTopHeldAndSaysItIsIncomplete(t *testing.T) {
	b, dir := book(t)
	for _, p := range []string{"top.md", "plan/mid.md", "plan/deep/deepest.md"} {
		if _, err := b.Write(p, "x", false); err != nil {
			t.Fatal(err)
		}
	}
	// the deepest is the newest, so a complete answer would be it
	later := time.Now().Add(time.Hour)
	if err := os.Chtimes(filepath.Join(dir, "plan", "deep", "deepest.md"), later, later); err != nil {
		t.Fatal(err)
	}
	found, complete, err := b.Newest(3, isRecord, time.Now().Add(-time.Second))
	if err != nil || complete || len(found) != 0 {
		t.Fatalf("a deadline already past: no folder is read: %v %v %v", found, complete, err)
	}
	deadline := time.Now().Add(time.Second)
	found, complete, err = b.Newest(3, func(p string) bool {
		if p == "top.md" {
			for time.Now().Before(deadline) { // the top is being read when the deadline passes
				time.Sleep(10 * time.Millisecond)
			}
		}
		return isRecord(p)
	}, deadline)
	if err != nil || complete || len(found) != 1 || found[0].Path != "top.md" {
		t.Fatalf("past the deadline while the top was read: what the top held, and incomplete: %v %v %v", found, complete, err)
	}
	found, complete, err = b.Newest(3, isRecord, time.Time{})
	if err != nil || !complete || len(found) != 3 || found[0].Path != "plan/deep/deepest.md" {
		t.Fatalf("no deadline: all of it, the deepest first: %v %v %v", found, complete, err)
	}
	if found, complete, err = (Book{Store: filepath.Join(t.TempDir(), ".shikii")}).Newest(3, isRecord, time.Now().Add(-time.Second)); err != nil || !complete || len(found) != 0 {
		t.Fatalf("no place: nothing to read, so nothing left unread: %v %v %v", found, complete, err)
	}
}

// A folder that cannot be read is an error, as it was: the listing is not silently shorter.
func TestListReportsAFolderItCannotRead(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads every folder")
	}
	b, dir := book(t)
	if _, err := b.Write("closed/a.md", "x", false); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(dir, "closed"), 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(filepath.Join(dir, "closed"), 0o755) })
	if _, err := b.List(); err == nil {
		t.Fatal("a folder that cannot be read must make List fail, as before")
	}
	if _, _, err := b.Newest(3, isRecord, time.Time{}); err == nil {
		t.Fatal("and Newest")
	}
}
