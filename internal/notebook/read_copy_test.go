package notebook

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestPreparedReadCacheKeepsExternalChangesVisible(t *testing.T) {
	for _, kind := range []string{"article", "source"} {
		t.Run(kind, func(t *testing.T) {
			b, p, source := readViewFixture(t)
			warm, err := b.PrepareReadSnapshot()
			if err != nil {
				t.Fatal(err)
			}
			if _, ok, err := warm.CurrentPage(p.ID); err != nil || !ok {
				t.Fatal(ok, err)
			}
			area, file := "pages", p.ID+".md"
			old, next := "The owner choice stays unresolved.", "The owner choice stays determined."
			if kind == "source" {
				area, file = "sources", source+".json"
				old, next = "Source fixture", "Source changed"
			}
			dir, _ := b.directory(false, area)
			path := filepath.Join(dir, file)
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			info, _ := os.Stat(path)
			changed := bytes.Replace(raw, []byte(old), []byte(next), 1)
			if bytes.Equal(changed, raw) || len(changed) != len(raw) {
				t.Fatal("fixture must change bytes while retaining size")
			}
			if err = os.WriteFile(path, changed, 0600); err != nil {
				t.Fatal(err)
			}
			if err = os.Chtimes(path, info.ModTime(), info.ModTime()); err != nil {
				t.Fatal(err)
			}
			current, err := b.PrepareReadSnapshot()
			if err != nil {
				t.Fatal(err)
			}
			if _, ok, err := current.CurrentPage(p.ID); err != nil || ok {
				t.Fatal("another preparation must recheck file bytes and sources", ok, err)
			}
		})
	}
}

// Populate every exported reference field, so adding a new mutable Page field
// also extends the ownership check without another hand-maintained field list.
func populateReadValue(v reflect.Value, text string) {
	switch v.Kind() {
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			if v.Field(i).CanSet() {
				populateReadValue(v.Field(i), text)
			}
		}
	case reflect.Pointer:
		if v.IsNil() {
			v.Set(reflect.New(v.Type().Elem()))
		}
		populateReadValue(v.Elem(), text)
	case reflect.Slice:
		if v.IsNil() {
			v.Set(reflect.MakeSlice(v.Type(), 1, 1))
		}
		for i := 0; i < v.Len(); i++ {
			populateReadValue(v.Index(i), text)
		}
	case reflect.String:
		v.SetString(text)
	default:
		panic("extend the ownership check for " + v.Type().String())
	}
}

func TestReadSnapshotCopiesIsolateEveryMutableField(t *testing.T) {
	b, _, _ := readViewFixture(t)
	view := b.ReadSnapshot()
	var p Page
	populateReadValue(reflect.ValueOf(&p).Elem(), "original")
	issues := []Issue{{Kind: "unreadable-page", Page: "original"}}
	want, _ := json.Marshal(p)
	view.rememberSnapshotPages([]Page{p}, issues)
	populateReadValue(reflect.ValueOf(&p).Elem(), "input changed")
	issues[0].Page = "input changed"
	for range 2 {
		pages, gotIssues, ok := view.snapshotPages()
		raw, _ := json.Marshal(pages[0])
		if !ok || string(raw) != string(want) || gotIssues[0].Page != "original" {
			t.Fatal("snapshot callers must own all returned mutable fields")
		}
		populateReadValue(reflect.ValueOf(&pages[0]).Elem(), "output changed")
		gotIssues[0].Page = "output changed"
	}
}

func TestReadSnapshotCopyAllocationBudget(t *testing.T) {
	b, p, _ := readViewFixture(t)
	view := b.ReadSnapshot()
	p.Body = strings.Repeat("Original owner choice stays unresolved.\n", 300)
	pages := make([]Page, 100)
	for i := range pages {
		pages[i] = p
	}
	view.rememberSnapshotPages(pages, []Issue{})
	result := testing.Benchmark(func(bench *testing.B) {
		for range bench.N {
			got, _, ok := view.snapshotPages()
			if !ok || len(got) != len(pages) || got[0].Body != p.Body {
				bench.Fatal("all article content must survive the copy")
			}
		}
	})
	if bytes := result.AllocedBytesPerOp(); bytes > 256<<10 {
		t.Fatalf("copying 100 articles exceeds the allocation budget: %d > 256 KiB", bytes)
	}
	oversized := b.ReadSnapshot()
	tooMany := make([]Page, 1000)
	for i := range tooMany {
		tooMany[i] = p
	}
	oversized.rememberSnapshotPages(tooMany, nil)
	if _, _, ok := oversized.snapshotPages(); ok {
		t.Fatal("an oversized inventory must not be retained")
	}
}
