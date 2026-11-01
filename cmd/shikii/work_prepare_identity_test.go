package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/hayaenfj/shikii/internal/notebook"
)

func auditRound2SettleMaintenance(t *testing.T, b notebook.Book) {
	t.Helper()
	tasks, err := b.Maintenance()
	if err != nil {
		t.Fatal(err)
	}
	for _, task := range tasks {
		if _, err = b.Maintain(notebook.MaintenanceReview{PageID: task.PageID, Revision: task.Revision, Key: task.Key, Reason: "Synthetic audit checked the source and unchanged content."}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestAuditRound2HookPreparationOmittedBodyRecovery(t *testing.T) {
	repo, root, _ := planningFixture(t)
	b := notebook.Book{Root: root, Scope: feedbackScope(repo)}
	marker := "AUDIT_R2_END_CONDITION: never publish the confidential export."
	page := reasoningPage(t, b, "Quartz CSV workflow", "howto", strings.Repeat("x", 8001)+marker)
	auditRound2SettleMaintenance(t, b)
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": planningTestRequest})
	turn := loadSession(root, "planning").TurnID
	direct, err := planningCall(t, "query", map[string]any{"session_id": "planning", "turn_id": turn, "query": "Quartz CSV"})
	if err != nil {
		t.Fatal(err)
	}
	directBytes, _ := json.Marshal(direct)
	if !strings.Contains(string(directBytes), page.ID) || !strings.Contains(string(directBytes), page.Revision) {
		t.Fatal("Synthetic control lacks exact article identity")
	}
	got, err := workCall(t, b, "prepare", map[string]any{"session_id": "planning", "turn_id": turn, "query": "Quartz CSV"})
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(got)
	if len(got["knowledge"].([]any)) != 1 {
		t.Fatal("Synthetic article was not retrieved", string(data))
	}
	if strings.Contains(string(data), marker) {
		t.Fatal("the automatic body budget must remain bounded")
	}
	if !strings.Contains(string(data), page.ID) || !strings.Contains(string(data), page.Revision) {
		t.Fatalf("Omitted body has no exact recovery identity: has_body=false has_id=%v has_revision=%v knowledge=%s", strings.Contains(string(data), page.ID), strings.Contains(string(data), page.Revision), mustJSONAuditRound2(got["knowledge"]))
	}
	entry := got["knowledge"].([]any)[0].(map[string]any)
	if entry["id"] != page.ID || entry["revision"] != page.Revision || entry["body_included"] != false {
		t.Fatal("an omitted article must have exact identity and explicit body status", entry)
	}
	query, ok := entry["query"].(map[string]any)
	if !ok || query["operation"] != "query" || query["query"] != "lesson:"+page.ID+"@"+page.Revision {
		t.Fatal("an omitted body needs an executable exact-revision query", entry)
	}
	value, err := improvementCall(t, b, query)
	if err != nil {
		t.Fatal(err)
	}
	recovered := loopObject(t, value)["lessons"].([]any)[0].(map[string]any)
	if recovered["id"] != page.ID || recovered["revision"] != page.Revision || !strings.Contains(recovered["body"].(string), marker) {
		t.Fatal("the supplied query must recover the omitted condition from the same revision", recovered)
	}

	t.Run("standalone control", workStandalonePreparationHasBodyOrRecovery)
}

func mustJSONAuditRound2(v any) string { b, _ := json.Marshal(v); return string(b) }

func workStandalonePreparationHasBodyOrRecovery(t *testing.T) {
	b, source := loopBook(t)
	marker := "AUDIT_R2_END_CONDITION"
	page := loopPage(t, b, source, map[string]any{"title": "Quartz CSV workflow", "lesson": "Quartz CSV workflow", "applies_to": "Quartz CSV", "body": strings.Repeat("x", 8001) + marker})
	auditRound2SettleMaintenance(t, b)
	got, err := workCall(t, b, "prepare", map[string]any{"query": "Quartz CSV"})
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(got)
	if !strings.Contains(string(data), marker) && (!strings.Contains(string(data), page.ID) || !strings.Contains(string(data), page.Revision)) {
		t.Fatal("Standalone body or recovery identity lost")
	}
}
