package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/hayaenfj/shikii/internal/notebook"
	"github.com/hayaenfj/shikii/internal/store"
)

func TestHookPreparesAndOneRecordFinishes(t *testing.T) {
	repo, root, _ := planningFixture(t)
	b := notebook.Book{Root: root, Scope: feedbackScope(repo)}
	page := interimAddArticle(t, b, "Archive approval", "Production restore requires owner approval. Restore was not executed.")
	out := runUnreviewedHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": "Explain archive approval for production restore."})
	s := loadSession(root, "planning")
	if s.Planning.Work == nil || s.Reasoning == nil || s.Reasoning.PreparedAt.IsZero() {
		t.Fatal("the hook must save a real preparation before asking for the ending record")
	}
	context := feedbackContext(t, out)
	const prefix = "shikii prepared work:\n"
	i := strings.Index(context, prefix)
	if i < 0 || strings.Contains(context, "shikii: prepare this task once") {
		t.Fatal("deliver the preparation instead of asking the agent to prepare again", context)
	}
	var prepared map[string]any
	if err := json.Unmarshal([]byte(context[i+len(prefix):]), &prepared); err != nil {
		t.Fatal("the complete prepared result must reach the agent", err)
	}
	if !strings.Contains(context, page.ID) || !strings.Contains(context, "Owner") && !strings.Contains(context, "owner approval") || prepared["receipt"] == nil {
		t.Fatal("the saved preparation must actually supply its evidence and receipt", context)
	}
	if records, err := (store.Store{Dir: root}).Records("needs-plan", 0); err != nil || len(records) != 0 {
		t.Fatal("preparing context must not invent a planning judgment", records, err)
	}
	r, err := workCall(t, b, "record", map[string]any{"session_id": "planning", "turn_id": s.TurnID, "result": map[string]any{"changed": "Explained the approval requirement and retained the unexecuted restore condition."}})
	if err != nil || r["complete"] != true || r["storage"].(map[string]any)["status"] != "saved" {
		t.Fatal("the hook IDs must finish its preparation without copying a long receipt", r, err)
	}
}

func TestHookRecordIDsCannotFollowReplacedPreparation(t *testing.T) {
	repo, root, _ := planningFixture(t)
	b := notebook.Book{Root: root, Scope: feedbackScope(repo)}
	runUnreviewedHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": "Explain archive approval."})
	s := loadSession(root, "planning")
	if s.Planning.Work == nil {
		t.Fatal("missing hook preparation")
	}
	req := map[string]any{"session_id": "planning", "turn_id": s.TurnID, "query": "Different source material"}
	p, err := workCall(t, b, "prepare", req)
	if err != nil {
		t.Fatal(err)
	}
	delete(req, "query")
	req["result"] = map[string]any{"changed": "Explained the checked material."}
	if _, err := workCall(t, b, "record", req); err == nil {
		t.Fatal("old local refs must not bind to a replacement preparation")
	}
	req["receipt"] = p["receipt"]
	if r, err := workCall(t, b, "record", req); err != nil || r["complete"] != true {
		t.Fatal("the explicit current receipt remains usable", r, err)
	}
}

func TestHookRecordIDsRejectAnotherTurnAndProject(t *testing.T) {
	repo, root, _ := planningFixture(t)
	b := notebook.Book{Root: root, Scope: feedbackScope(repo)}
	runUnreviewedHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": "Explain archive approval."})
	old := loadSession(root, "planning")
	if old.Planning.Work == nil {
		t.Fatal("missing hook preparation")
	}
	runUnreviewedHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": "Explain the current recovery conditions."})
	request := map[string]any{"session_id": "planning", "turn_id": old.TurnID, "result": map[string]any{"changed": "Explained the old material."}}
	if _, err := workCall(t, b, "record", request); err == nil {
		t.Fatal("a new request must refuse the older IDs")
	}
	current := loadSession(root, "planning")
	request["turn_id"] = current.TurnID
	other := notebook.Book{Root: root, Scope: b.Scope + "-other"}
	if _, err := workCall(t, other, "record", request); err == nil {
		t.Fatal("the same IDs are not a preparation in another project")
	}
	if loadSession(root, "planning").Planning.Work.Result != nil {
		t.Fatal("refused records must leave the current result unsaved")
	}
}

func TestHookPreparationCannotReplaceConcurrentExplicitPreparation(t *testing.T) {
	repo, root, _ := planningFixture(t)
	b := notebook.Book{Root: root, Scope: feedbackScope(repo)}
	runUnreviewedHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": "Explain archive approval."})
	turn := loadSession(root, "planning").TurnID
	p, err := workCall(t, b, "prepare", map[string]any{"session_id": "planning", "turn_id": turn, "query": "Explicit selected topic"})
	if err != nil {
		t.Fatal(err)
	}
	before := loadSession(root, "planning").Planning.Work.Nonce
	// The hook's unlocked check happened before the explicit preparation saved.
	_, err = prepareWork(root, b.Scope, workRequest{SessionID: "planning", TurnID: turn, Query: "Explain archive approval.", fromHook: true})
	if err == nil || loadSession(root, "planning").Planning.Work.Nonce != before {
		t.Fatal("a late hook preparation must not replace a receipt already given to its caller", err)
	}
	if r, err := workCall(t, b, "record", map[string]any{"receipt": p["receipt"], "result": map[string]any{"changed": "Explained the explicitly prepared topic."}}); err != nil || r["complete"] != true {
		t.Fatal("retain the concurrent explicit receipt", r, err)
	}
}

func TestHookPreparationBudgetFailureKeepsManualRecovery(t *testing.T) {
	repo, root, _ := planningFixture(t)
	runUnreviewedHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": planningTestRequest})
	turn := loadSession(root, "planning").TurnID
	if err := updateSession(root, "planning", func(s *hookSession) { s.Planning.Work = nil }); err != nil {
		t.Fatal(err)
	}
	in := hookInput{SessionID: "planning", CWD: repo, Prompt: planningTestRequest, workTurn: turn}
	out := hookOut{Context: planningNotice("planning", turn) + strings.Repeat("Retain this existing context. ", 1500)}
	before := out.Context
	prepareHookWork(root, &in, &out)
	if out.Context != before || loadSession(root, "planning").Planning.Work != nil {
		t.Fatal("an undeliverable preparation must keep the manual recovery and save no replacement")
	}
}
