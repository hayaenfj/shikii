package notebook

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/hayaenfj/shikii/internal/store"
)

var errReadChanged = errors.New("knowledge changed while reading; prepare again")

type readCheck struct {
	hash  string
	err   error
	check func() (string, error)
}

func sameReadResult(a string, ae error, b string, be error) bool {
	if (ae == nil) != (be == nil) || a != b {
		return false
	}
	return ae == nil || ae.Error() == be.Error()
}

type readSnapshot struct {
	root   string
	checks map[string]readCheck
	raw    map[string][]byte
	bytes  int
	pages  []byte
	failed bool
}

// PrepareReadSnapshot establishes the fixed article layout before freezing
// reads. MemoryUpgrade may initialize its checkpoint during preparation; its
// own empty-directory creation must not look like concurrent article changes.
func (b Book) PrepareReadSnapshot() (Book, error) {
	if _, err := b.directory(true, "pages"); err != nil {
		return b, err
	}
	return b.ReadSnapshot(), nil
}

// ReadSnapshot is for one sequential preparation only. It shares file reads,
// then ValidateRead must succeed inside the context's commit boundary. Writers
// and independent operations use an ordinary Book, never this snapshot.
func (b Book) ReadSnapshot() Book {
	b = b.ReadView().scopedRead()
	root, _ := filepath.EvalSymlinks(b.Root)
	b.snapshot = &readSnapshot{root: root, checks: map[string]readCheck{}, raw: map[string][]byte{}}
	namePath := filepath.Join(b.Root, "records", "store-name")
	_, err := b.sharedCheck("namespace:"+namePath, func() (string, error) {
		f, err := openSourceFile(namePath)
		if os.IsNotExist(err) {
			return "missing", nil
		}
		if err != nil {
			return "", err
		}
		defer f.Close()
		raw, err := io.ReadAll(io.LimitReader(f, 129))
		if err != nil || len(raw) > 128 {
			return "", errReadChanged
		}
		return digest(raw), nil
	})
	if err != nil {
		b.snapshot.failed = true
	}
	return b
}

func (b Book) sharedCheck(key string, check func() (string, error)) (string, error) {
	if b.snapshot == nil {
		return check()
	}
	if old, ok := b.snapshot.checks[key]; ok {
		return old.hash, old.err
	}
	hash, err := check()
	b.snapshot.checks[key] = readCheck{hash, err, check}
	return hash, err
}

func (b Book) sharedRead(path string, read func(string) ([]byte, error)) ([]byte, error) {
	if b.snapshot == nil {
		return read(path)
	}
	if raw, ok := b.snapshot.raw[path]; ok {
		return raw, nil
	}
	raw, err := read(path)
	hash := digest(raw)
	if old, ok := b.snapshot.checks[path]; ok && !sameReadResult(old.hash, old.err, hash, err) {
		b.snapshot.failed = true
		return nil, errReadChanged
	}
	b.snapshot.checks[path] = readCheck{hash, err, func() (string, error) {
		data, err := read(path)
		return digest(data), err
	}}
	if err != nil {
		return raw, err
	}
	if b.snapshot.bytes+len(raw) <= 8<<20 {
		b.snapshot.raw[path] = raw
		b.snapshot.bytes += len(raw)
	}
	return raw, nil
}

func (b Book) sharedDigest(path string) (string, error) {
	return b.sharedCheck(path, func() (string, error) { return sourceRecordDigest(path) })
}

func (b Book) sharedPremise(path string) (string, string) {
	value, _ := b.sharedCheck("premise:"+path, func() (string, error) {
		hash, state := b.premiseHash(path)
		return hash + " " + state, nil
	})
	hash, state, _ := strings.Cut(value, " ")
	return hash, state
}

func pageManifest(entries []os.DirEntry) string {
	names := []string{}
	for _, entry := range entries {
		id := strings.TrimSuffix(entry.Name(), ".md")
		if idPattern.MatchString(id) && filepath.Ext(entry.Name()) == ".md" {
			names = append(names, entry.Name()+":"+entry.Type().String())
		}
	}
	sort.Strings(names)
	return digest([]byte(strings.Join(names, "\n")))
}

func (b Book) snapshotDirectory(dir string, entries []os.DirEntry) {
	if b.snapshot == nil {
		return
	}
	key := "directory:" + dir
	if old, ok := b.snapshot.checks[key]; ok && old.hash != pageManifest(entries) {
		b.snapshot.failed = true
		return
	}
	b.snapshot.checks[key] = readCheck{pageManifest(entries), nil, func() (string, error) {
		fresh := Book{Root: b.Root, Scope: b.Scope, name: b.name}
		now, err := fresh.directory(false, "pages")
		if err != nil || now != dir {
			return "", errReadChanged
		}
		entries, err := os.ReadDir(now)
		return pageManifest(entries), err
	}}
}

func (b Book) snapshotPages() ([]Page, []Issue, bool) {
	if b.snapshot == nil || b.snapshot.pages == nil {
		return nil, nil, false
	}
	var value struct {
		Pages  []Page
		Issues []Issue
	}
	if json.Unmarshal(b.snapshot.pages, &value) != nil {
		b.snapshot.failed = true
		return nil, nil, false
	}
	return value.Pages, value.Issues, true
}

func (b Book) rememberSnapshotPages(pages []Page, issues []Issue) {
	if b.snapshot == nil {
		return
	}
	data, _ := json.Marshal(struct {
		Pages  []Page
		Issues []Issue
	}{pages, issues})
	if len(data) <= 8<<20 {
		b.snapshot.pages = data
	}
}

// Every captured byte hash, source reader, directory inventory and watched
// premise is checked again. Unchanged mtime and size never substitute for data.
func (b Book) ValidateRead() error {
	if b.snapshot == nil {
		return nil
	}
	root, err := filepath.EvalSymlinks(b.Root)
	if err != nil || root != b.snapshot.root || store.StoreName(b.Root, b.Scope) != b.name || b.snapshot.failed {
		return errReadChanged
	}
	keys := make([]string, 0, len(b.snapshot.checks))
	for key := range b.snapshot.checks {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		old := b.snapshot.checks[key]
		current, err := old.check()
		if !sameReadResult(old.hash, old.err, current, err) {
			return errReadChanged
		}
	}
	return nil
}
