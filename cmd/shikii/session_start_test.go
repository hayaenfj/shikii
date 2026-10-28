package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/handoff"
	"github.com/hayaenfj/shikii/internal/notebook"
)

// 0.11: at SessionStart, which Claude Code and Codex also run right after a summary of a long conversation (source "compact"), the agent
// gets what the project itself says: the rules that apply to every request (scene "always") and the last section of the newest work
// record under .shikii/handoff/. A summary dropped the person's intent in this project on 2026-10-28; the work record was what brought
// it back. Nothing to tell means no output, and the hook never stops the session.
func TestSessionStartBringsTheAlwaysRulesAndTheNewestWorkRecord(t *testing.T) {
	st, scope, _, draft, _ := memoryEffectFixture(t)
	always := draft
	always.Title, always.Lesson, always.Scene, always.AppliesTo = "Visible text is Japanese", "Every visible sentence is written in Japanese.", "\u3044\u3064\u3082", "every request"
	page, err := (notebook.Book{Root: st.Dir, Scope: scope}).Ingest(always)
	if err != nil {
		t.Fatal(err)
	}
	book := handoff.Book{Store: st.Dir}
	if _, err := book.Write("plan-0.11/scope.md", "# Scope\n\n## First section\nold text\n\n## Where the work stands\nA and B are in; C is next.\n", false); err != nil {
		t.Fatal(err)
	}
	if _, err := book.Write("audit/old.md", "# Old\n\n## Done\nnothing\n", false); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(filepath.Join(st.Dir, handoff.Dir, "audit", "old.md"), time.Now().Add(-48*time.Hour), time.Now().Add(-48*time.Hour)); err != nil {
		t.Fatal(err)
	}
	out := runHook(t, "session-start", map[string]any{"session_id": "s", "cwd": scope, "hook_event_name": "SessionStart", "source": "compact"})
	var v map[string]any
	if err := json.Unmarshal([]byte(out), &v); err != nil {
		t.Fatalf("one JSON object: %v\n%s", err, out)
	}
	spec, _ := v["hookSpecificOutput"].(map[string]any)
	ctx, _ := spec["additionalContext"].(string)
	if spec["hookEventName"] != "SessionStart" || len(v) != 1 || len(spec) != 2 {
		t.Fatalf("the output only adds context to SessionStart; it never stops the session:\n%s", out)
	}
	for _, want := range []string{"Every visible sentence is written in Japanese.", "lesson:" + page.Page.ID + ":" + page.Page.Revision, "plan-0.11/scope.md", "Where the work stands", "A and B are in; C is next."} {
		if !strings.Contains(ctx, want) {
			t.Fatalf("the brief carries the always rules and the last section of the newest work record (missing %q):\n%s", want, ctx)
		}
	}
	for _, not := range []string{"old text", "audit/old.md", draft.Lesson} {
		if strings.Contains(ctx, not) {
			t.Fatalf("not the earlier sections, not older records, not ordinary lessons (%q):\n%s", not, ctx)
		}
	}
	if len(ctx) > 1800 {
		t.Fatalf("the brief stays short: %d bytes", len(ctx))
	}
}

// A project with no always rule and no work record gets nothing; outside a shikii project nothing is printed and no error is raised.
func TestSessionStartIsSilentWhenThereIsNothingToTell(t *testing.T) {
	_, scope, _, _, _ := memoryEffectFixture(t)
	if out := runHook(t, "session-start", map[string]any{"session_id": "s", "cwd": scope, "hook_event_name": "SessionStart", "source": "startup"}); out != "" {
		t.Fatalf("nothing to tell, nothing printed:\n%s", out)
	}
	if out := runHook(t, "session-start", map[string]any{"session_id": "s", "cwd": t.TempDir(), "hook_event_name": "SessionStart", "source": "startup"}); out != "" {
		t.Fatalf("outside a project, nothing:\n%s", out)
	}
}
