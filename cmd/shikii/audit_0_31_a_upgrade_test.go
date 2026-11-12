package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/notebook"
)

// audit A: the upgrade batch at the smallest bound and with long lessons: does it keep its references, or go whole?
func TestAuditAUpgradeBatchAtTightBounds(t *testing.T) {
	for _, c := range []struct {
		name   string
		bound  int
		lesson int // bytes of each page's lesson
	}{
		{"8192 bytes, short lessons", 8192, 40},
		{"8192 bytes, lessons of 600 bytes", 8192, 600},
		{"9000 bytes, lessons of 1200 bytes", 9000, 1200},
		{"8192 bytes, lessons of 1200 bytes", 8192, 1200},
		{"8192 bytes, lessons of 900 bytes", 8192, 900},
		{"12000 bytes, lessons of 1200 bytes", 12000, 1200},
	} {
		t.Run(c.name, func(t *testing.T) {
			repo, root, _ := planningFixture(t)
			b := notebook.Book{Root: root, Scope: feedbackScope(repo)}
			s, err := b.CaptureObserved(notebook.Observation{Kind: "test", Title: "Original rule", Content: "Keep existing evidence and current preferences.", Reference: "synthetic", Version: "1", ObservedAt: time.Now()})
			if err != nil {
				t.Fatal(err)
			}
			for _, name := range []string{"invoices", "exports"} {
				body := strings.Repeat("The long background of the "+name+" rule. ", 140)
				lesson := "Keep the evidence of " + name + ". " + strings.Repeat("Check it. ", c.lesson/10)
				if _, err := b.Ingest(notebook.Draft{Sources: []string{s.ID}, Title: "Upgrade rule for " + name, Lesson: lesson, AppliesTo: name, Kind: "rule", Scene: "when handling " + name, Body: &body}); err != nil {
					t.Fatal(err)
				}
			}
			runHook(t, "user-prompt-submit", map[string]any{"session_id": "upgrade", "cwd": repo, "prompt": "Check the routing implementation."})
			v, err := workCall(t, b, "prepare", map[string]any{"session_id": "upgrade", "turn_id": loadSession(root, "upgrade").TurnID, "query": "memory-upgrade", "max_bytes": c.bound})
			if err != nil {
				t.Errorf("the query memory-upgrade is refused: %v", err)
				return
			}
			raw, _ := json.Marshal(v)
			u, _ := v["memory_upgrade"].(map[string]any)
			pages, _ := u["pages"].([]any)
			t.Logf("%d bytes, %d pages, omitted=%v", len(raw), len(pages), v["output"].(map[string]any)["omitted"])
			if len(pages) != 2 {
				t.Errorf("in answer to the query memory-upgrade the batch is left out whole (%d pages): the references, the maintenance references and the queries are gone: %v", len(pages), fmt.Sprint(v["output"].(map[string]any)["omitted"]))
			}
		})
	}
}
