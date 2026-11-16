package main

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/notebook"
)

// The progress notice the person sees (systemMessage) while the memory upgrade runs is Japanese. The agent's context may stay English,
// so this reads only the person's notice, not the whole hook output.
func TestUpgradeProgressSystemMessageIsJapanese(t *testing.T) {
	repo, root, _ := planningFixture(t)
	b := notebook.Book{Root: root, Scope: feedbackScope(repo)}
	s, err := b.CaptureObserved(notebook.Observation{Kind: "test", Title: "Original rule", Content: "Keep existing evidence and current preferences.", Reference: "synthetic", Version: "1", ObservedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	loopPage(t, b, s.ID, nil)
	loopPage(t, b, s.ID, map[string]any{"title": "Second upgrade rule"})
	loopPage(t, b, s.ID, map[string]any{"title": "Third upgrade rule"})
	out := runHook(t, "user-prompt-submit", map[string]any{"session_id": "upgrade-notice", "cwd": repo, "prompt": "Check the routing implementation."})
	var hook struct {
		SystemMessage string `json:"systemMessage"`
	}
	if err := json.Unmarshal([]byte(out), &hook); err != nil {
		t.Fatal("the hook output must be JSON:", err, out)
	}
	if !strings.Contains(hook.SystemMessage, "\u8a18\u61b6\u3092\u66f4\u65b0\u3057\u3066\u3044\u307e\u3059") {
		t.Fatal("the progress notice the person sees must be Japanese:", hook.SystemMessage)
	}
	if strings.Contains(hook.SystemMessage, "Upgrading memory") {
		t.Fatal("the person's notice must not carry English:", hook.SystemMessage)
	}
}
