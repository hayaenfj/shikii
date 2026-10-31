package notebook

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func auditPatchFixture(t *testing.T) (Book, Page, Patch) {
	t.Helper()
	b := Book{Root: t.TempDir(), Scope: "/audit/project"}
	d := draft(seed(t, b, 9001), "Audit article")
	body := "Keep this. Old target."
	d.Body = &body
	saved, err := b.Ingest(d)
	if err != nil {
		t.Fatal(err)
	}
	p := *saved.Page
	return b, p, Patch{ID: p.ID, Revision: p.Revision, Sources: d.Sources, ChangeReason: "Synthetic checked change.", Edits: []TextEdit{{Field: "body", Before: "Old target.", After: "New target."}}}
}

func TestKnowledgePatchReportsSavedRevisionAfterDerivedFailure(t *testing.T) {
	for _, blocked := range []string{"index.md", "log.md"} {
		t.Run(blocked, func(t *testing.T) {
			b, old, patch := auditPatchFixture(t)
			dir, err := b.directory(false, "pages")
			if err != nil {
				t.Fatal(err)
			}
			target := filepath.Join(dir, blocked)
			saved, err := os.ReadFile(target)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(target); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(target, 0700); err != nil {
				t.Fatal(err)
			}
			defer func() { _ = os.Remove(target); _ = os.WriteFile(target, saved, 0600) }()
			sourceID := seed(t, b, 9002)
			patch.Sources = append(patch.Sources, sourceID)
			sourceDir, dirErr := b.directory(false, "sources")
			if dirErr != nil {
				t.Fatal(dirErr)
			}
			sourcePath := filepath.Join(sourceDir, sourceID+".json")
			_, priorSourceErr := os.Stat(sourcePath)
			result, err := b.Patch(patch)
			if err != nil || result.Page == nil || len(result.Warnings) == 0 {
				t.Fatal("saved revision and recovery warning are required", result, err)
			}
			current, readErr := b.Query("lesson:" + old.ID)
			if readErr != nil || len(current.Lessons) != 1 {
				t.Fatal("query", readErr)
			}
			now := current.Lessons[0]
			oldHistory, oldErr := b.readRevision("lesson:" + old.ID + "@" + old.Revision)
			newHistory, newErr := b.readRevision("lesson:" + old.ID + "@" + now.Revision)
			t.Logf("saved_with_warning=true; returned_page=%t; current_changed=%t; old_history_valid=%t; new_history_valid=%t; source_issues=%d", result.Page != nil, now.Revision != old.Revision, oldErr == nil && oldHistory.Body == old.Body, newErr == nil && newHistory.Body == now.Body, len(b.sourceIssues(now)))
			_, sourceErr := os.Stat(sourcePath)
			_, reviewedErr := os.Stat(filepath.Join(sourceDir, sourceID+".reviewed"))
			indexData, indexErr := os.ReadFile(filepath.Join(dir, "index.md"))
			indexHasNew := indexErr == nil && strings.Contains(string(indexData), " / 2 sources / ")
			logData, logErr := os.ReadFile(filepath.Join(dir, "log.md"))
			logHasNew := logErr == nil && strings.Contains(string(logData), now.Revision)
			t.Logf("new_source_before=%t; new_source_after=%t; new_source_reviewed=%t; index_contains_new=%t; log_contains_new=%t", priorSourceErr == nil, sourceErr == nil, reviewedErr == nil, indexHasNew, logHasNew)
			_, retryErr := b.Patch(patch)
			t.Logf("same_patch_retry_conflict=%t", errors.Is(retryErr, ErrArticleConflict))
			if now.Revision == old.Revision || result.Page.Revision != now.Revision || !errors.Is(retryErr, ErrArticleConflict) {
				t.Fatal("saved state differs from the reply")
			}
			if err := os.Remove(target); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(target, saved, 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := b.Lint(); err != nil {
				t.Fatal(err)
			}
			if _, err := b.Ingest(Draft{Sources: patch.Sources}); err != nil {
				t.Fatal("source review recovery", err)
			}
			if _, err := os.Stat(filepath.Join(sourceDir, sourceID+".reviewed")); err != nil {
				t.Fatal("source stayed pending", err)
			}
		})
	}
}

func TestKnowledgeConcurrentPatchRejectsStaleRevision(t *testing.T) {
	b, old, patch := auditPatchFixture(t)
	start := make(chan struct{})
	errorsOut := make(chan error, 2)
	for range 2 {
		go func() { <-start; _, err := b.Patch(patch); errorsOut <- err }()
	}
	close(start)
	successes, conflicts := 0, 0
	for range 2 {
		err := <-errorsOut
		if err == nil {
			successes++
		} else if errors.Is(err, ErrArticleConflict) {
			conflicts++
		} else {
			t.Fatal(err)
		}
	}
	current, err := b.Query("lesson:" + old.ID)
	if err != nil || successes != 1 || conflicts != 1 || current.Lessons[0].Body != "Keep this. New target." {
		t.Fatal(successes, conflicts, err)
	}
}

func TestKnowledgeDeliveryRefusesParentLinkDuringLockWait(t *testing.T) {
	b := Book{Root: t.TempDir(), Scope: "/audit/project"}
	dir, err := b.directory(true, "deliveries")
	if err != nil {
		t.Fatal(err)
	}
	unlock, err := lock(dir)
	if err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	path := filepath.Join(outside, ".deliveries.json")
	original := []byte("[]")
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	waiting, resume := make(chan struct{}), make(chan struct{})
	var once sync.Once
	lockBusy = func() { once.Do(func() { close(waiting); <-resume }) }
	defer func() { lockBusy = nil }()
	done := make(chan error, 1)
	go func() {
		done <- b.SaveDeliveryCase(DeliveryCase{Mode: "request", Request: "Synthetic context", Complete: true})
	}()
	<-waiting
	moved := dir + "-saved"
	if err := os.Rename(dir, moved); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, dir); err != nil {
		t.Fatal(err)
	}
	unlock()
	close(resume)
	err = <-done
	raw, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatal(readErr)
	}
	t.Logf("save_error=%t; outside_changed=%t", err != nil, string(raw) != string(original))
	if err == nil || string(raw) != string(original) {
		t.Fatal("delivery save followed the substituted parent link")
	}
}

func TestKnowledgeDeliveryEncodedCaseBoundIncludesID(t *testing.T) {
	b := Book{Root: t.TempDir(), Scope: "/audit/project"}
	c := DeliveryCase{Format: 1, At: time.Date(2026, 10, 31, 12, 0, 0, 999999999, time.UTC), Mode: "request", Complete: true}
	c.Request = "Synthetic context"
	c.Entries = make([]DeliveryEntry, 128)
	for i := range c.Entries {
		c.Entries[i] = DeliveryEntry{ID: fmt.Sprintf("%016x", i+1), Revision: "0000000000000001", Retrieval: "candidate", Reason: "candidate", Discovery: "x"}
	}
	raw, _ := json.Marshal(c)
	remaining := (512 << 10) - len(raw) - 5
	for i := range c.Entries {
		add := min(remaining, 4003)
		c.Entries[i].Discovery += strings.Repeat("x", add)
		remaining -= add
	}
	if remaining != 0 {
		t.Fatal("cannot prepare bounded discovery contexts")
	}

	if err := b.SaveDeliveryCase(c); err != nil {
		t.Fatal(err)
	}
	dir, err := b.directory(false, "deliveries")
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, ".deliveries.json"))
	if err != nil {
		t.Fatal(err)
	}
	var records []json.RawMessage
	if err := json.Unmarshal(data, &records); err != nil {
		t.Fatal(err)
	}
	t.Logf("encoded_case_bytes=%d; advertised_limit=%d; extra_bytes=%d", len(records[0]), 512<<10, len(records[0])-(512<<10))
	if len(records[0]) > 512<<10 {
		t.Fatal("encoded case exceeds its stated bound")
	}
}

func TestKnowledgePatchRejectsOverlappingMatches(t *testing.T) {
	b := Book{Root: t.TempDir(), Scope: "/audit/project"}
	d := draft(seed(t, b, 9001), "Overlap article")
	body := "ababa"
	d.Body = &body
	saved, err := b.Ingest(d)
	if err != nil {
		t.Fatal(err)
	}
	p := *saved.Page
	patch := Patch{ID: p.ID, Revision: p.Revision, Sources: d.Sources, ChangeReason: "Synthetic overlap.", Edits: []TextEdit{{Field: "body", Before: "aba", After: "x"}}}
	got, err := b.Patch(patch)
	if err == nil {
		t.Logf("matched_offsets=0,2; body_after=%s", got.Page.Body)
		t.Fatal("overlapping matches are ambiguous")
	}
}
