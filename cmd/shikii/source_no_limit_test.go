package main

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
