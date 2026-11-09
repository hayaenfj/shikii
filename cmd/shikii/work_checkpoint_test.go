package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/store"
)

func TestWorkCheckpointReducesReplacementWrites(t *testing.T) {
	b, _ := loopBook(t)
	v, err := workCall(t, b, "prepare", map[string]any{"query": "unmatched", "request": "Check synthetic storage only."})
	if err != nil {
		t.Fatal(err)
	}
	old := store.SyncTemp
	replacements, syncs := 0, 0
	store.SyncTemp = func(f *os.File) error {
		name := filepath.Base(f.Name())
		if strings.HasPrefix(name, ".work-") || strings.HasSuffix(name, ".json") {
			syncs++
		}
		if strings.HasPrefix(name, ".work-") {
			replacements++
		}
		return old(f)
	}
	t.Cleanup(func() { store.SyncTemp = old })
	r, err := workCall(t, b, "record", map[string]any{"receipt": v["receipt"], "result": map[string]any{"changed": "Checked synthetic storage only."}})
	if err != nil || r["complete"] != true {
		t.Fatal(r, err)
	}
	if replacements > 7 {
		t.Fatalf("unchanged checkpoints must reuse the durable file; replacements=%d syncs=%d", replacements, syncs)
	}
	if syncs != 9 {
		t.Fatalf("checkpoint reuse must retain every durability boundary, got %d", syncs)
	}
}

func TestWorkCheckpointKeepsTTLAndCurrentReceipt(t *testing.T) {
	b, _ := loopBook(t)
	v, err := workCall(t, b, "prepare", map[string]any{"query": "unmatched", "request": "Synthetic checkpoint."})
	if err != nil {
		t.Fatal(err)
	}
	token, err := workDecode(v["receipt"].(string))
	if err != nil {
		t.Fatal(err)
	}
	dir, err := openWorkSessions(b.Root)
	if err != nil {
		t.Fatal(err)
	}
	defer dir.Close()
	w, name, err := loadWorkState(dir, b.Scope, token)
	if err != nil {
		t.Fatal(err)
	}
	cp := &workSessionCheckpoint{}
	if err = saveWorkState(dir, name, b.Scope, token.Turn, w, false, cp); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir.Name(), name)
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-hookSessionTTL - time.Hour).Truncate(time.Microsecond)
	if err = os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	now := time.Now().Truncate(time.Microsecond)
	clock := workCheckpointNow
	workCheckpointNow = func() time.Time { return now }
	t.Cleanup(func() { workCheckpointNow = clock })
	if err = saveWorkState(dir, name, b.Scope, token.Turn, w, false, cp); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(before, after) || !after.ModTime().Equal(now) {
		t.Fatal("reuse must keep the file and refresh cleanup TTL", after.ModTime(), now)
	}
	pruneSessionRecord(dir, name)
	if _, err = os.Stat(path); err != nil {
		t.Fatal("a refreshed checkpoint must survive TTL cleanup", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	w.Nonce = "different"
	if err = saveWorkState(dir, name, b.Scope, token.Turn, w, false, cp); err == nil {
		t.Fatal("reuse cannot bypass current receipt validation")
	}
	current, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(current, raw) {
		t.Fatal("a stale receipt must not write", err)
	}
}

func TestWorkCheckpointFailureRetainsBytesAndRetries(t *testing.T) {
	for _, kind := range []string{"touch", "sync", "changed-bytes"} {
		t.Run(kind, func(t *testing.T) {
			b, _ := loopBook(t)
			v, err := workCall(t, b, "prepare", map[string]any{"query": "unmatched", "request": "Synthetic checkpoint failure."})
			if err != nil {
				t.Fatal(err)
			}
			token, _ := workDecode(v["receipt"].(string))
			dir, err := openWorkSessions(b.Root)
			if err != nil {
				t.Fatal(err)
			}
			defer dir.Close()
			w, name, err := loadWorkState(dir, b.Scope, token)
			if err != nil {
				t.Fatal(err)
			}
			cp := &workSessionCheckpoint{}
			if err = saveWorkState(dir, name, b.Scope, token.Turn, w, false, cp); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(dir.Name(), name)
			oldTime := time.Now().Add(-time.Hour).Truncate(time.Microsecond)
			if err = os.Chtimes(path, oldTime, oldTime); err != nil {
				t.Fatal(err)
			}
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			oldTouch, oldSync := workCheckpointTouch, store.SyncTemp
			t.Cleanup(func() { workCheckpointTouch = oldTouch; store.SyncTemp = oldSync })
			failure := errors.New("synthetic checkpoint failure")
			switch kind {
			case "touch":
				workCheckpointTouch = func(*os.File, time.Time) error { return failure }
			case "sync":
				store.SyncTemp = func(*os.File) error { return failure }
			case "changed-bytes":
				raw = bytes.Replace(raw, []byte("Synthetic"), []byte("Different"), 1)
				if err = os.WriteFile(path, raw, 0600); err != nil {
					t.Fatal(err)
				}
			}
			// This helper is called only after saveWorkContext's locked state checks.
			if err = reuseWorkCheckpoint(dir, name, cp.last); err == nil {
				t.Fatal("expected reuse failure")
			}
			current, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(current, raw) {
				t.Fatal("failed reuse must leave all bytes intact", err)
			}
			if kind != "changed-bytes" {
				info, _ := os.Stat(path)
				if !info.ModTime().Equal(oldTime) {
					t.Fatal("failed reuse must retain the old cleanup time", info.ModTime(), oldTime)
				}
			}
			workCheckpointTouch, store.SyncTemp = oldTouch, oldSync
			if kind == "changed-bytes" {
				if err = os.WriteFile(path, cp.last, 0600); err != nil {
					t.Fatal(err)
				}
			}
			if err = saveWorkState(dir, name, b.Scope, token.Turn, w, false, cp); err != nil {
				t.Fatal("retry must recover the same state", err)
			}
		})
	}
}
