package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/notebook"
	"github.com/hayaenfj/shikii/internal/spec"
)

func TestRound2PlanSessionEncodedBoundary(t *testing.T) {
	repo, root, _ := planningFixture(t)
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": planningTestRequest})
	b := notebook.Book{Root: root, Scope: feedbackScope(repo)}
	prep, err := workCall(t, b, "prepare", map[string]any{"session_id": "planning", "turn_id": loadSession(root, "planning").TurnID, "query": "CSV filters"})
	if err != nil {
		t.Fatal(err)
	}
	token, err := workDecode(prep["receipt"].(string))
	if err != nil {
		t.Fatal(err)
	}
	dir, err := openWorkSessions(root)
	if err != nil {
		t.Fatal(err)
	}
	defer dir.Close()
	name := filepath.Base(sessionPath(root, "planning"))
	s, err := readPlanningSession(dir, name)
	if err != nil {
		t.Fatal(err)
	}
	// A bounded collection of pending note bodies forms the near-limit stored context.
	for i := 0; i < 64; i++ {
		body := strings.Repeat("x", 16000)
		ref := string(rune('a' + i))
		s.Planning.Work.Pending[ref] = notebook.Draft{Title: "Synthetic pending", Lesson: "Keep existing context.", AppliesTo: "audit boundary", Kind: "howto", Scene: "checking", Body: &body}
	}
	body := ""
	s.Planning.Work.Pending["padding"] = notebook.Draft{Title: "Synthetic padding", Lesson: "Keep existing context.", AppliesTo: "audit boundary", Kind: "howto", Scene: "checking", Body: &body}
	before, _ := json.MarshalIndent(s, "", "  ")
	delta := (1 << 20) - 32 - len(before)
	body += strings.Repeat("x", delta)
	s.Planning.Work.Pending["padding"] = notebook.Draft{Title: "Synthetic padding", Lesson: "Keep existing context.", AppliesTo: "audit boundary", Kind: "howto", Scene: "checking", Body: &body}
	if err = writeWorkSession(dir, name, s); err != nil {
		t.Fatal(err)
	}
	before, _ = os.ReadFile(sessionPath(root, "planning"))
	if len(before) >= 1<<20 {
		t.Fatal("fixture exceeded boundary", len(before))
	}
	if _, _, err = loadWorkState(dir, b.Scope, token); err != nil {
		t.Fatal(err)
	}
	result, err := workCall(t, b, "record", map[string]any{"receipt": prep["receipt"], "agreed": map[string]any{"unknown": true}, "open": map[string]any{"unknown": true}})
	after, _ := os.ReadFile(sessionPath(root, "planning"))
	_, reread := readPlanningSession(dir, name)
	t.Logf("before_bytes=%d after_bytes=%d input_error=%t complete=%v reread_rejected=%t", len(before), len(after), err != nil, result["complete"], reread != nil)
	if len(after) > 1<<20 || reread != nil {
		t.Fatal("\u8a08\u753b\u306e\u4fdd\u5b58\u304c\u30bb\u30c3\u30b7\u30e7\u30f3\u4e0a\u9650\u3092\u8d85\u3048\u3001\u518d\u8aad\u307f\u8fbc\u307f\u3092\u62d2\u5426\u3057\u305f")
	}
}

func TestRound2PublicPlanSessionBoundary(t *testing.T) {
	repo, root, _ := planningFixture(t)
	b := notebook.Book{Root: root, Scope: feedbackScope(repo)}
	evidence, err := b.Capture(notebook.Observation{Kind: "test", Title: "Synthetic baseline", Content: "Only local synthetic context.", Reference: "synthetic", Version: "1", ObservedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	loopPage(t, b, evidence.ID, map[string]any{"title": "CSV baseline", "lesson": "Keep existing CSV filters.", "applies_to": "CSV filters"})
	// A separate source remains pending so prepare supplies evidence1.
	_, err = b.Capture(notebook.Observation{Kind: "test", Title: "Synthetic pending evidence", Content: "Only local pending context.", Reference: "synthetic", Version: "1", ObservedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": planningTestRequest})
	prep, err := workCall(t, b, "prepare", map[string]any{"session_id": "planning", "turn_id": loadSession(root, "planning").TurnID, "query": "CSV filters"})
	if err != nil {
		t.Fatal(err)
	}
	readSize := func() int {
		raw, e := os.ReadFile(sessionPath(root, "planning"))
		if e != nil {
			t.Fatal(e)
		}
		return len(raw)
	}
	previous := readSize()
	overhead := 0
	count := 0
	for i := 0; i < 64; i++ {
		body := strings.Repeat("<", 16000)
		needed := (1 << 20) - 32 - previous - overhead
		last := i > 1 && needed <= 96000
		if last {
			if needed < 0 || needed > 96000 {
				t.Fatal("invalid final body budget", needed)
			}
			body = strings.Repeat("<", needed/6) + strings.Repeat("x", needed%6)
		}
		value, e := workCall(t, b, "record", map[string]any{"receipt": prep["receipt"], "notes": []any{map[string]any{"title": fmt.Sprintf("CSV boundary %02d", i), "lesson": "Keep existing CSV filters.", "applies_to": "CSV filters", "kind": "howto", "scene": "checking CSV", "body": body, "sources": []string{"evidence1"}}}})
		if e != nil || value["complete"] != false || len(value["pending"].([]any)) != 1 {
			t.Fatal(i, value, e)
		}
		current := readSize()
		overhead = current - previous - len(body)*6
		previous = current
		count++
		if last {
			break
		}
	}
	before := readSize()
	if before < (1<<20)-64 || before >= 1<<20 {
		t.Fatal("unexpected public boundary size", before)
	}
	result, err := workCall(t, b, "record", map[string]any{"receipt": prep["receipt"], "agreed": map[string]any{"unknown": true}, "open": map[string]any{"unknown": true}})
	after := readSize()
	dir, e := openWorkSessions(root)
	if e != nil {
		t.Fatal(e)
	}
	defer dir.Close()
	_, reread := readPlanningSession(dir, filepath.Base(sessionPath(root, "planning")))
	t.Logf("public_pending=%d before_bytes=%d after_bytes=%d input_error=%t complete=%v reread_rejected=%t", count, before, after, err != nil, result["complete"], reread != nil)
	if after > 1<<20 || reread != nil {
		t.Fatal("\u516c\u958b\u5165\u529b\u306e\u307f\u3067\u8aad\u3081\u306a\u3044\u30bb\u30c3\u30b7\u30e7\u30f3\u3092\u4fdd\u5b58\u3057\u305f")
	}
	if after != before || result["complete"] != false {
		t.Fatal("a capacity failure must preserve the prior record and remain explicitly incomplete", before, after, result)
	}
	recovered, recoverErr := workCall(t, b, "prepare", map[string]any{"session_id": "planning", "turn_id": loadSession(root, "planning").TurnID, "query": "CSV filters"})
	if recoverErr != nil || recovered["receipt"] == prep["receipt"] {
		t.Fatal("the intact session must support a fresh preparation after capacity failure", recovered, recoverErr)
	}
	state := loopObject(t, recovered["reasoning"])
	archive, _ := state["pending_source"].(string)
	if state["wiki_pending"] != true || archive == "" {
		t.Fatal("capacity recovery must preserve the obligation and original drafts", recovered)
	}
	sources, sourceErr := b.ReadSources([]string{archive})
	raw, _ := json.Marshal(sources)
	if sourceErr != nil || !strings.Contains(string(raw), fmt.Sprintf("CSV boundary %02d", count-1)) {
		t.Fatal("the archived pending draft is unavailable", sourceErr)
	}

}

func TestHookSessionWritersPreserveReadableRecordAtCapacity(t *testing.T) {
	for _, writer := range []string{"hook", "work", "query", "plan"} {
		t.Run(writer, func(t *testing.T) {
			repo, root, _ := planningFixture(t)
			runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": planningTestRequest})
			dir, err := openWorkSessions(root)
			if err != nil {
				t.Fatal(err)
			}
			defer dir.Close()
			name := filepath.Base(sessionPath(root, "planning"))
			before, err := os.ReadFile(sessionPath(root, "planning"))
			if err != nil {
				t.Fatal(err)
			}
			s, err := readPlanningSession(dir, name)
			if err != nil {
				t.Fatal(err)
			}
			oversized := strings.Repeat("<", 200000)
			request := planningRequest{SessionID: "planning", TurnID: s.TurnID, Agreed: &planningFact{Unknown: true}, Open: &planningFact{Unknown: true}}
			scope := feedbackScope(repo)
			switch writer {
			case "hook":
				s.Request = oversized
				err = saveSession(root, "planning", s)
			case "work":
				s.Request = oversized
				err = writeWorkSession(dir, name, s)
			case "query":
				err = savePlanningQuery(dir, name, scope, request, map[string]string{"current_user": oversized}, nil)
			case "plan":
				sp, e := spec.Load(hookSpecPath(root, "needs-plan"))
				if e != nil {
					t.Fatal(e)
				}
				err = savePlanningResult(dir, name, root, scope, request, sp, nil, nil, &planningReceipt{Shadow: true, Reason: oversized}, "synthetic-size-test", nil)
			}
			if err == nil {
				t.Error("an encoded session above capacity must fail before replacement")
			}
			after, readErr := os.ReadFile(sessionPath(root, "planning"))
			if readErr != nil || string(before) != string(after) {
				t.Fatal("an oversized write replaced the last readable session", readErr)
			}
			if _, readErr = readPlanningSession(dir, name); readErr != nil {
				t.Fatal("the prior session must remain readable", readErr)
			}
		})
	}
}
