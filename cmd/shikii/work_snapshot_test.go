package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorkReadSnapshotRejectsChangesBeforeSaving(t *testing.T) {
	b, source := loopBook(t)
	p := loopPage(t, b, source, map[string]any{"title": "Snapshot routing"})
	first, e := workCall(t, b, "prepare", map[string]any{"request": "Keep the owner choice unresolved.", "query": "Snapshot routing"})
	if e != nil {
		t.Fatal(e)
	}
	dir := filepath.Join(b.Root, "wiki", "lessons")
	entries, e := os.ReadDir(dir)
	if e != nil {
		t.Fatal(e)
	}
	path := ""
	for _, entry := range entries {
		if entry.IsDir() {
			candidate := filepath.Join(dir, entry.Name(), p.ID+".md")
			if _, e := os.Stat(candidate); e == nil {
				path = candidate
			}
		}
	}
	if path == "" {
		t.Fatal("fixture article path missing")
	}
	raw, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	changed := false
	_, e = prepareWork(b.Root, b.Scope, workRequest{Receipt: first["receipt"].(string), Query: "Snapshot routing", responseSize: func(v any) int {
		if !changed {
			changed = true
			if err := os.WriteFile(path, append(append([]byte{}, raw...), []byte("changed")...), 0600); err != nil {
				t.Fatal(err)
			}
		}
		return workContextSize(v)
	}})
	if e == nil || !strings.Contains(e.Error(), "changed while reading") {
		t.Fatal("a late change must fail before registering a delivery", e)
	}
	if e = os.WriteFile(path, raw, 0600); e != nil {
		t.Fatal(e)
	}
	next, e := workCall(t, b, "prepare", map[string]any{"receipt": first["receipt"], "query": "Snapshot routing"})
	if e != nil || next["delivery"] != first["delivery"].(float64)+1 || next["task"] != first["task"] {
		t.Fatal("a failed read cannot advance or replace its receipt", next, e)
	}
}
