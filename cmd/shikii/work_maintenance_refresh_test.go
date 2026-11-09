package main

import (
	"github.com/hayaenfj/shikii/internal/notebook"
	"strings"
	"testing"
	"time"
)

func TestWorkMaintenanceRefreshRequiresCurrentFullRead(t *testing.T) {
	for _, mode := range []string{"current", "historical", "omitted", "untracked"} {
		t.Run(mode, func(t *testing.T) {
			repo, root, _ := planningFixture(t)
			b := notebook.Book{Root: root, Scope: feedbackScope(repo)}
			source, e := b.Capture(notebook.Observation{Kind: "test", Title: "Refresh source", Content: "Keep the owner unresolved.", Reference: "synthetic", Version: "1", ObservedAt: time.Now()})
			if e != nil {
				t.Fatal(e)
			}
			for _, title := range []string{"Refresh first", "Refresh second", "Refresh third"} {
				loopPage(t, b, source.ID, map[string]any{"title": title})
			}
			runHook(t, "user-prompt-submit", map[string]any{"session_id": "refresh", "cwd": repo, "prompt": "Check the routing implementation."})
			initial, e := workCall(t, b, "prepare", map[string]any{"session_id": "refresh", "turn_id": loadSession(root, "refresh").TurnID, "query": "memory-upgrade"})
			if e != nil {
				t.Fatal(e)
			}
			pages := initial["memory_upgrade"].(map[string]any)["pages"].([]any)
			selected := pages[0].(map[string]any)
			article := selected["article"].(map[string]any)
			id := article["id"].(string)
			oldRevision := article["revision"].(string)
			ref := selected["maintenance_ref"].(string)
			var target string
			if mode == "untracked" {
				all, _ := b.Live()
				known := map[string]bool{}
				for _, v := range pages {
					known[v.(map[string]any)["article"].(map[string]any)["id"].(string)] = true
				}
				for _, p := range all {
					if !known[p.ID] {
						target = "lesson:" + p.ID + "@" + p.Revision
					}
				}
			}
			body := strings.Repeat("The owner remains unresolved.\n", 350)
			saved, e := b.Ingest(notebook.Draft{ID: id, Title: article["title"].(string), Lesson: "The revised owner condition is unresolved.", AppliesTo: article["applies_to"].(string), Sources: []string{source.ID}, Body: &body})
			if e != nil {
				t.Fatal(e)
			}
			targetRevision := saved.Page.Revision
			if target == "" {
				if mode == "historical" {
					targetRevision = oldRevision
				}
				target = "lesson:" + id + "@" + targetRevision
			}
			budget := 65536
			if mode == "omitted" {
				budget = 8192
			}
			read, e := workCall(t, b, "prepare", map[string]any{"receipt": initial["receipt"], "query": target, "max_bytes": budget})
			if e != nil {
				t.Fatal(e)
			}
			entry := read["knowledge"].([]any)[0].(map[string]any)
			if mode == "current" {
				reviews, ok := entry["maintenance_reviews"].([]any)
				if !ok || len(reviews) != 1 || reviews[0].(map[string]any)["ref"] != ref || reviews[0].(map[string]any)["revision"] != saved.Page.Revision {
					t.Fatal("a full current read must return the refreshed existing review", entry["maintenance_reviews"])
				}
			} else if entry["maintenance_reviews"] != nil {
				t.Fatal("historical, omitted and untracked reads must not refresh reviews", mode)
			}
			result, e := workCall(t, b, "record", map[string]any{"receipt": initial["receipt"], "maintenance": []any{map[string]any{"ref": ref, "reason": "Checked the exact current article, original source and changed condition."}}})
			if e != nil {
				t.Fatal(e)
			}
			if (result["complete"] == true) != (mode == "current") {
				t.Fatal("only an actually delivered current read may authorize the latest review key", mode, result)
			}
		})
	}
}
