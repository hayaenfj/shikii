package main

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/notebook"
)

func TestHookPreparationNeedsRecordedResultBeforeStop(t *testing.T) {
	repo, root, _ := planningFixture(t)
	const session = "hook-record-stop"
	preparedHookReply(t, runUnreviewedHook(t, "user-prompt-submit", map[string]any{"session_id": session, "cwd": repo, "prompt": "Explain the existing synthetic archive approval conditions."}))
	before := loadSession(root, session)
	if before.Planning.Work == nil || !before.Planning.Work.HookPrepared || before.Reasoning == nil || before.Reasoning.PreparedAt.IsZero() || before.Reasoning.Finish != nil {
		t.Fatal("the real hook must prepare the task without inventing an ending review")
	}
	out := runUnreviewedHook(t, "stop", map[string]any{"session_id": session, "cwd": repo, "last_assistant_message": "\u65e2\u5b58\u306e\u6761\u4ef6\u3092\u8aac\u660e\u3057\u307e\u3057\u305f\u3002"})
	reason, blocked := stopReason(t, out)
	if !blocked || !strings.Contains(reason, "reasoning-before-end") || loadSession(root, session).Judged {
		t.Fatal("preparation alone must not pass Stop", out)
	}
	if strings.Contains(reason, "receipt") || !strings.Contains(reason, "work record") || !strings.Contains(reason, "the MCP tool shikii_wiki, operation record") {
		t.Fatal("the hook Stop must request its IDs and result without copying a receipt", reason)
	}
	start := strings.Index(reason, `{"session_id":`)
	if start < 0 {
		t.Fatal("the Stop recovery must supply the request IDs", reason)
	}
	var record map[string]any
	if err := json.NewDecoder(strings.NewReader(reason[start:])).Decode(&record); err != nil || len(record) != 2 || record["session_id"] != session || record["turn_id"] != before.TurnID {
		t.Fatal("the Stop must supply exactly the current request IDs", record, err)
	}
	record["result"] = map[string]any{"changed": "Explained the existing synthetic archive approval conditions."}
	input, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	t.Chdir(repo)
	var saved string
	withStdin(t, string(input), func() { saved = capture(t, func() error { return cmdWork([]string{"record"}) }) })
	var result map[string]any
	if err := json.Unmarshal([]byte(saved), &result); err != nil || result["complete"] != true || loopObject(t, result["storage"])["status"] != "saved" {
		t.Fatal("the real result must finish the hook preparation", result, err)
	}
	if loadSession(root, session).Reasoning.Finish == nil {
		t.Fatal("record must persist an ending review for the delivered preparation")
	}
	out = runUnreviewedHook(t, "stop", map[string]any{"session_id": session, "cwd": repo, "last_assistant_message": "\u65e2\u5b58\u306e\u6761\u4ef6\u3092\u8aac\u660e\u3057\u3001\u7d50\u679c\u3092\u4fdd\u5b58\u3057\u307e\u3057\u305f\u3002", "stop_hook_active": true})
	if _, blocked := stopReason(t, out); blocked || !loadSession(root, session).Judged {
		t.Fatal("Stop must accept the real saved result without a synthetic prerequisite", out)
	}
}

func TestStopKeepsReceiptForManualOrReplacedPreparation(t *testing.T) {
	for _, initial := range []string{"manual", "hook_replaced"} {
		t.Run(initial, func(t *testing.T) {
			repo, root, _ := planningFixture(t)
			const session = "explicit-record-stop"
			prompt := map[string]any{"session_id": session, "cwd": repo, "prompt": "Explain the existing synthetic archive approval conditions."}
			if initial == "manual" {
				runManualHook(t, "user-prompt-submit", prompt)
			} else {
				preparedHookReply(t, runUnreviewedHook(t, "user-prompt-submit", prompt))
			}
			b := notebook.Book{Root: root, Scope: feedbackScope(repo)}
			prepared, err := workCall(t, b, "prepare", map[string]any{"session_id": session, "turn_id": loadSession(root, session).TurnID, "query": "archive approval"})
			if err != nil || loadSession(root, session).Planning.Work.HookPrepared {
				t.Fatal("the fixture must retain an explicit preparation", err)
			}
			reason, blocked := stopReason(t, runUnreviewedHook(t, "stop", map[string]any{"session_id": session, "cwd": repo, "last_assistant_message": "\u65e2\u5b58\u306e\u6761\u4ef6\u3092\u8aac\u660e\u3057\u307e\u3057\u305f\u3002"}))
			if !blocked || !strings.Contains(reason, "Send the receipt of your work prepare") || !strings.Contains(reason, "work record") {
				t.Fatal("an explicit preparation must keep the receipt recovery instructions", reason)
			}
			result, err := workCall(t, b, "record", map[string]any{"receipt": prepared["receipt"], "result": map[string]any{"changed": "Explained the explicitly prepared archive conditions."}})
			if err != nil || result["complete"] != true {
				t.Fatal("the explicit receipt in the existing recovery flow must remain usable", result, err)
			}
		})
	}
}

func TestHookResultCannotBypassPendingReflection(t *testing.T) {
	repo, root, _ := planningFixture(t)
	b := notebook.Book{Root: root, Scope: feedbackScope(repo)}
	var sources []string
	for _, name := range []string{"A", "B"} {
		source, err := b.CaptureObserved(notebook.Observation{Kind: "test", Title: "Synthetic earlier check " + name, Content: "Synthetic case " + name + " completed without a new reusable rule.", Reference: "synthetic fixture", Version: "1", ObservedAt: time.Now().UTC()})
		if err != nil {
			t.Fatal(err)
		}
		sources = append(sources, source.ID)
	}
	const session = "hook-reflection-stop"
	prepared := preparedHookReply(t, runUnreviewedHook(t, "user-prompt-submit", map[string]any{"session_id": session, "cwd": repo, "prompt": "Explain the existing synthetic archive approval conditions."}))
	s := loadSession(root, session)
	if s.ReflectAskedTurn != s.TurnID || !slices.Equal(s.ReflectAsked, sources) {
		t.Fatal("the real hook must retain both required reflection sources", s.ReflectAsked)
	}
	var reviewed []string
	for _, raw := range prepared["observations"].([]any) {
		entry := loopObject(t, raw)
		material := loopObject(t, entry["source"])
		observations := material["observations"].([]any)
		if len(observations) != 1 || !slices.Contains(sources, loopObject(t, observations[0])["id"].(string)) {
			t.Fatal("the prepared reflection must supply each original source")
		}
		reviewed = append(reviewed, entry["ref"].(string))
	}
	if len(reviewed) != len(sources) {
		t.Fatal("the hook must deliver both reflection sources before review")
	}
	result, err := workCall(t, b, "record", map[string]any{"session_id": session, "turn_id": s.TurnID, "result": map[string]any{"changed": "Explained the existing synthetic archive approval conditions."}})
	if err != nil || result["complete"] != true || loopObject(t, result["storage"])["status"] != "saved" {
		t.Fatal("the current result must be saved independently of earlier reflection", result, err)
	}
	if left, err := reflectLeft(root, b.Scope, sources); err != nil || len(left) != len(sources) {
		t.Fatal("saving a result must not consume the required reflection", left, err)
	}
	out := runUnreviewedHook(t, "stop", map[string]any{"session_id": session, "cwd": repo, "last_assistant_message": "\u65e2\u5b58\u306e\u6761\u4ef6\u3092\u8aac\u660e\u3057\u3001\u7d50\u679c\u3092\u4fdd\u5b58\u3057\u307e\u3057\u305f\u3002"})
	if reason, blocked := stopReason(t, out); !blocked || !strings.Contains(reason, "reflection has not been saved") || strings.Contains(reason, "reasoning-before-end") || loadSession(root, session).Judged {
		t.Fatal("a saved result must not bypass the pending reflection", out)
	}
	result, err = workCall(t, b, "record", map[string]any{"receipt": prepared["receipt"], "reviewed": reviewed})
	if err != nil || result["complete"] != true {
		t.Fatal("reviewing the supplied sources must save the missing reflection", result, err)
	}
	out = runUnreviewedHook(t, "stop", map[string]any{"session_id": session, "cwd": repo, "last_assistant_message": "\u78ba\u8a8d\u6e08\u307f\u306e\u8cc7\u6599\u3092\u6574\u7406\u3057\u3001\u7d50\u679c\u3092\u4fdd\u5b58\u3057\u307e\u3057\u305f\u3002", "stop_hook_active": true})
	if _, blocked := stopReason(t, out); blocked || !loadSession(root, session).Judged {
		t.Fatal("Stop must accept the result after the real reflection is saved", out)
	}
}
