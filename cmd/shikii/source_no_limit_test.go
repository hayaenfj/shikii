package main

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/hayaenfj/shikii/internal/notebook"
	"github.com/hayaenfj/shikii/internal/store"
)

// A source record has no size limit of its own: a long memory file is captured whole (not split by its sections),
// read back, and listed for sorting. Only the store's line limit (store.MaxLineBytes) still applies to the record.
func TestLongSourceIsCapturedWhole(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("HOME", filepath.Join(dir, "home"))
	capture(t, func() error { return cmdInit(nil) })
	content := strings.Repeat("A long memory file, kept verbatim. ", 6000) + "END-OF-MEMORY" // about 210 KiB
	in, _ := json.Marshal(notebook.Observation{Kind: "agent", Title: "long memory", Content: content, Reference: "memory/long.md", Version: "2026-10-26", ObservedAt: time.Now()})
	var out string
	withStdin(t, string(in), func() { out = capture(t, func() error { return cmdNotebook([]string{"capture"}) }) })
	var saved notebook.Observation
	if err := json.Unmarshal([]byte(out), &saved); err != nil || saved.ID == "" {
		t.Fatalf("the CLI must capture a long source whole: %v\n%.300s", err, out)
	}
	root, scope, err := currentProjectStore()
	if err != nil {
		t.Fatal(err)
	}
	book := notebook.Book{Root: root, Scope: scope}
	got, err := book.ReadSources([]string{saved.ID})
	obs, _ := got["observations"].([]notebook.Observation)
	if err != nil || len(obs) != 1 || obs[0].Content != content {
		t.Fatalf("the long source must be read back whole: %v", err)
	}
	pending, err := book.PendingObservations()
	if err != nil || len(pending) != 1 || !strings.HasSuffix(pending[0].Content, "END-OF-MEMORY") {
		t.Fatalf("the long source must be listed for sorting: %v", err)
	}
	huge := notebook.Observation{Kind: "agent", Title: "huge", Content: strings.Repeat("x", store.MaxLineBytes), Reference: "memory/huge.md", Version: "v", ObservedAt: time.Now()}
	if _, err := book.Capture(huge); err == nil || !strings.Contains(err.Error(), "MiB") {
		t.Fatalf("a record over the store's line limit is still refused, saying the limit: %v", err)
	}
}

// The reflection and the wiki listing show a pending source by its head only (title, reference, version, length and the first
// notebook.SourceHeadBytes of the body); the whole body is read with wiki query <source id>. A short source is shown whole.
func TestPendingSourcesAreListedByTheirHead(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("HOME", filepath.Join(dir, "home"))
	capture(t, func() error { return cmdInit(nil) })
	root, scope, err := currentProjectStore()
	if err != nil {
		t.Fatal(err)
	}
	book := notebook.Book{Root: root, Scope: scope}
	long := "x" + strings.Repeat("\u3042\u3044\u3046\u3048\u304a", 2000) + "END-OF-LONG" // 30 KB, multi-byte and off the 3-byte grid
	big, err := book.Capture(notebook.Observation{Kind: "agent", Title: "long", Content: long, Reference: "memory/long.md", Version: "v1", ObservedAt: time.Unix(1, 0)})
	if err != nil {
		t.Fatal(err)
	}
	small, err := book.Capture(notebook.Observation{Kind: "test", Title: "short", Content: "PASS", Reference: "go test", Version: "v1", ObservedAt: time.Unix(2, 0)})
	if err != nil {
		t.Fatal(err)
	}
	check := func(where string, list []notebook.ObservationHead) {
		t.Helper()
		heads := map[string]notebook.ObservationHead{}
		for _, h := range list {
			heads[h.ID] = h
		}
		h := heads[big.ID]
		if !h.ContentCut || h.ContentBytes != len(long) || len(h.Content) > notebook.SourceHeadBytes || len(h.Content) < notebook.SourceHeadBytes-3 || !strings.HasPrefix(long, h.Content) || !utf8.ValidString(h.Content) || h.Content == "" || h.Title != "long" || h.Reference != "memory/long.md" || h.Version != "v1" {
			t.Fatalf("%s: a long source is listed by its head (cut=%v bytes=%d head=%d)", where, h.ContentCut, h.ContentBytes, len(h.Content))
		}
		if s := heads[small.ID]; s.ContentCut || s.Content != "PASS" || s.ContentBytes != 4 {
			t.Fatalf("%s: a short source is listed whole: %+v", where, s)
		}
	}
	v, err := reflectQuery(root, scope)
	if err != nil {
		t.Fatal(err)
	}
	check("reflect query", v.Observations)
	w, err := book.QueryPage("", "")
	if err != nil {
		t.Fatal(err)
	}
	check("wiki query", w.Observations)
	got, err := book.ReadSources([]string{big.ID})
	if obs, _ := got["observations"].([]notebook.Observation); err != nil || len(obs) != 1 || obs[0].Content != long {
		t.Fatalf("wiki query <source id> still returns the whole body: %v", err)
	}
}
