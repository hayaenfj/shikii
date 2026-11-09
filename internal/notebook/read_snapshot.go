package notebook

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"slices"
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
	root          string
	checks        map[string]readCheck
	raw           map[string][]byte
	bytes         int
	pages         []Page
	issues        []Issue
	failed        bool
	named         bool
	validation    []byte
	feedback      *store.FeedbackRead
	sourceDecodes *sourceDecodeView
}

// Only validated parsing is shared across preparations in this process. File
// bytes, source validity and premises are still freshly checked per preparation.
var preparedReadPages = &readView{pages: map[string]readPage{}}

// PrepareReadSnapshot establishes the fixed article layout before freezing
// reads. MemoryUpgrade may initialize its checkpoint during preparation; its
// own empty-directory creation must not look like concurrent article changes.
func (b Book) PrepareReadSnapshot() (Book, error) {
	b = b.ReadSnapshot()
	if _, err := b.directory(true, "pages"); err != nil {
		return b, err
	}
	b.read = preparedReadPages
	return b, nil
}

// ReadSnapshot is for one sequential preparation only. It shares file reads,
// then ValidateRead must succeed inside the context's commit boundary. Writers
// and independent operations use an ordinary Book, never this snapshot.
func (b Book) ReadSnapshot() Book {
	b = b.ReadView()
	namePath := filepath.Join(b.Root, "records", "store-name")
	name, hash, err := readSnapshotName(namePath)
	if err != nil || name == "" {
		b.name = store.StoreName(b.Root, b.Scope)
		name, hash, err = readSnapshotName(namePath)
	} else {
		b.name = name
	}
	root, _ := filepath.EvalSymlinks(b.Root)
	b.snapshot = &readSnapshot{root: root, checks: map[string]readCheck{}, raw: map[string][]byte{}}
	b.snapshot.checks["namespace:"+namePath] = readCheck{hash, err, func() (string, error) {
		name, hash, err := readSnapshotName(namePath)
		if name != "" && name != b.name {
			return "", errReadChanged
		}
		return hash, err
	}}
	if err != nil {
		b.snapshot.failed = true
	}
	b.snapshot.named = err == nil && hash != "missing"
	return b
}

func readSnapshotName(path string) (string, string, error) {
	f, err := openSourceFile(path)
	if os.IsNotExist(err) {
		return "", "missing", nil
	}
	if err != nil {
		return "", "", err
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, 129))
	name := strings.TrimSpace(string(raw))
	if err != nil || len(raw) > 128 || len(name) != 64 || strings.Trim(name, "0123456789abcdef") != "" {
		return "", "", errReadChanged
	}
	return name, digest(raw), nil
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
	return b.sharedReadValidated(path, read, nil)
}

func (b Book) sharedReadValidated(path string, read func(string) ([]byte, error), verify func(string, []byte) (string, error)) ([]byte, error) {
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
		if verify != nil {
			return verify(path, b.validationScratch())
		}
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
	return copyReadPages(b.snapshot.pages), slices.Clone(b.snapshot.issues), true
}

func (b Book) rememberSnapshotPages(pages []Page, issues []Issue) {
	if b.snapshot == nil {
		return
	}
	cost := readValueSize(reflect.ValueOf(pages)) + readValueSize(reflect.ValueOf(issues))
	if cost <= readViewLimit {
		b.snapshot.pages = copyReadPages(pages)
		b.snapshot.issues = slices.Clone(issues)
	}
}

// Every captured byte hash, source reader, directory inventory and watched
// premise is checked again. Unchanged mtime and size never substitute for data.
func (b Book) ValidateRead() error {
	if b.snapshot == nil {
		return nil
	}
	root, err := filepath.EvalSymlinks(b.Root)
	if err != nil || root != b.snapshot.root || b.snapshot.failed {
		return errReadChanged
	}
	// A regular name file is already checked in full below. Only the legacy
	// missing-file fallback needs StoreName to resolve its project metadata.
	if !b.snapshot.named && store.StoreName(b.Root, b.Scope) != b.name {
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
