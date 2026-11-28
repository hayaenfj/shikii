package handoff

import (
	"errors"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// walkWorkers is how many folders of the place are read at once. Reading a folder waits on the disk, and a project whose handoff/ holds
// the output of tools has tens of thousands of folders: on 2026-11-28 one took 15 to 21 seconds to read in turn (970,000 files in
// 45,000 folders) and 5 to 6 with 8 readers. More readers do not read faster than that.
const walkWorkers = 8

// infoOf asks a directory entry for its file information. Tests swap it to count the files whose information a listing asked for:
// with the place's files in the hundreds of thousands, each one asked for is time (a hook once took 15 seconds on 900,000 files).
var infoOf = func(d fs.DirEntry) (fs.FileInfo, error) { return d.Info() }

// walk is one reading of the folders of a place, breadth first: the folders nearest the top first, because the records a person writes
// to read later are there, and the files a tool wrote in thousands are deep.
type walk struct {
	base     string
	deadline time.Time // a zero time is none
	visit    func(p string, d fs.DirEntry) error

	mu    sync.Mutex
	cond  *sync.Cond
	queue []string // the folders found, by path relative to the place ("" is the place); queue[head:] are not read yet
	head  int
	busy  int   // folders being read
	err   error // the first error of a reading or of visit; the walk stops
	late  bool  // the deadline passed before a folder was read: some folders are left unread
}

// eachFile calls visit for every file of the place that List lists (links and special files are not listed or followed, a hidden name is
// not handled and a hidden folder is not entered), in no order, from up to walkWorkers goroutines at once: visit must be safe for
// that. It stops at the first error visit or a reading returns. complete is false when deadline (a zero time is none) passed before every
// folder was read: the files of the folders not read were not visited, and the folders nearest the top were read first.
//
// The folders are read by their paths, not through the os.Root of the place: a file opened in a Root has the type of every entry of its
// folder looked up one by one (Go's os.File.ReadDir cannot take it from the folder then), and that was most of the 14 seconds a place of
// 900,000 files took. Only this listing is by path; Open and Read, which return what a file holds, go through the Root as before. Each
// folder is opened without following a link, so a name swapped for a link between the reading of its parent and the opening of it is
// refused, and an entry that is a link is neither entered nor listed.
func (b Book) eachFile(deadline time.Time, visit func(p string, d fs.DirEntry) error) (complete bool, err error) {
	r, err := b.open()
	if errors.Is(err, fs.ErrNotExist) {
		return true, nil // no place, nothing in it
	}
	if err != nil {
		return false, err
	}
	r.Close() // only to refuse a place that is a link, as every other call does
	w := &walk{base: b.dir(), deadline: deadline, visit: visit, queue: []string{""}}
	w.cond = sync.NewCond(&w.mu)
	var wg sync.WaitGroup
	for i := 0; i < walkWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w.run()
		}()
	}
	wg.Wait()
	return !w.late && w.err == nil, w.err
}

// run reads folders from the queue until it is empty and none is being read, an error has come, or the deadline has passed.
func (w *walk) run() {
	for {
		w.mu.Lock()
		for w.head == len(w.queue) && w.busy > 0 && w.err == nil && !w.late {
			w.cond.Wait()
		}
		if w.err != nil || w.late || w.head == len(w.queue) {
			w.mu.Unlock()
			w.cond.Broadcast()
			return
		}
		rel := w.queue[w.head]
		w.head++
		w.busy++
		w.mu.Unlock()

		past := !w.deadline.IsZero() && time.Now().After(w.deadline)
		var folders []string
		var err error
		if !past {
			folders, err = w.read(rel)
		}

		w.mu.Lock()
		w.busy--
		switch {
		case past:
			w.late = true
		case err != nil:
			if w.err == nil {
				w.err = err
			}
		default:
			w.queue = append(w.queue, folders...)
		}
		w.mu.Unlock()
		w.cond.Broadcast()
	}
}

// read reads one folder: it visits its files and returns the folders in it. Only the last name of the path is refused when it is a link:
// a folder above it that is replaced with a link while the place is read is followed, which gives names and times only (a file's text is
// read through os.Root, book.Read) and takes the same user's own write to the place.
func (w *walk) read(rel string) ([]string, error) {
	f, err := os.OpenFile(filepath.Join(w.base, filepath.FromSlash(rel)), os.O_RDONLY|noFollow|openDirectory, 0)
	if err != nil {
		return nil, err
	}
	entries, err := f.ReadDir(-1)
	f.Close()
	if err != nil {
		return nil, err
	}
	var folders []string
	for _, d := range entries {
		if strings.HasPrefix(d.Name(), ".") {
			continue
		}
		p := path.Join(rel, d.Name())
		switch {
		case d.Type()&fs.ModeSymlink != 0:
		case d.IsDir():
			folders = append(folders, p)
		case d.Type().IsRegular():
			if err := w.visit(p, d); err != nil {
				return nil, err
			}
		}
	}
	return folders, nil
}

// before is the order of a listing: the newest update first, and the path to break a tie.
func before(a, b Entry) bool {
	if !a.Modified.Equal(b.Modified) {
		return a.Modified.After(b.Modified)
	}
	return a.Path < b.Path
}

// entryOf is the list's entry of a file the walk found.
func entryOf(p string, d fs.DirEntry) (Entry, error) {
	info, err := infoOf(d)
	if err != nil {
		return Entry{}, err
	}
	return Entry{Path: p, Size: info.Size(), Modified: info.ModTime().UTC(), Kind: KindOf(p)}, nil
}

// List returns every file in the place, newest update first (no limit on the count). Empty if the place does not exist.
func (b Book) List() ([]Entry, error) {
	out := []Entry{}
	var mu sync.Mutex
	_, err := b.eachFile(time.Time{}, func(p string, d fs.DirEntry) error {
		e, err := entryOf(p, d)
		if err != nil {
			return err
		}
		mu.Lock()
		out = append(out, e)
		mu.Unlock()
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.SliceStable(out, func(i, j int) bool { return before(out[i], out[j]) })
	return out, nil
}

// Newest returns the first n files List would return among those whose path want accepts (what want answers must depend on the path
// alone), newest update first, without listing the place: the folders are read, but the information of a file is asked for only when
// want accepts its path, so the files of other kinds, in the hundreds of thousands in a place a tool wrote into, cost the reading of
// their folders and nothing more. Empty if the place does not exist.
//
// complete is false when deadline (a zero time is none) passed before every folder was read. The folders nearest the top are read first,
// so what is returned is then the newest of the files found there, which may not be the newest of the place: the caller says so. It
// is the whole answer, the same as the first n of List, whenever complete is true.
func (b Book) Newest(n int, want func(p string) bool, deadline time.Time) (found []Entry, complete bool, err error) {
	found = []Entry{}
	if n <= 0 {
		return found, true, nil
	}
	var mu sync.Mutex
	complete, err = b.eachFile(deadline, func(p string, d fs.DirEntry) error {
		if !want(p) {
			return nil
		}
		e, err := entryOf(p, d)
		if err != nil {
			return err
		}
		mu.Lock()
		defer mu.Unlock()
		at := len(found)
		for at > 0 && before(e, found[at-1]) {
			at--
		}
		if at >= n {
			return nil
		}
		found = append(found, Entry{})
		copy(found[at+1:], found[at:])
		found[at] = e
		if len(found) > n {
			found = found[:n]
		}
		return nil
	})
	if err != nil {
		return nil, false, err
	}
	return found, complete, nil
}
