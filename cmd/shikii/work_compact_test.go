package main

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hayaenfj/shikii/internal/notebook"
)

func TestWorkCompactTwoCallsKeepEvidenceOnce(t *testing.T) {
	b, source := loopBook(t)
	v, err := workCall(t, b, "prepare", map[string]any{"request": "Explain the existing routing rule.", "query": "routing"})
	if err != nil {
		t.Fatal(err)
	}
	if v["reasoning"].(map[string]any)["started"] != true {
		t.Fatal("preparation must register the start without a second call")
	}
	receipt := v["receipt"]
	result := map[string]any{"changed": "Explained the existing rule; no new project knowledge.", "checked": "Verified the supplied routing evidence.", "evidence": []string{}, "remaining": ""}
	r, err := workCall(t, b, "record", map[string]any{"receipt": receipt, "result": result})
	if err != nil || r["complete"] != true || r["reasoning"].(map[string]any)["complete"] != true {
		t.Fatal("ordinary work must end in two calls without a wiki draft", r, err)
	}
	if r["storage"].(map[string]any)["status"] != "saved" {
		t.Fatal(r)
	}
	first := r["storage"].(map[string]any)["result_source"]
	again, err := workCall(t, b, "record", map[string]any{"receipt": receipt, "result": result})
	if err != nil || again["storage"].(map[string]any)["result_source"] != first {
		t.Fatal("an identical retry must retain the result source", again, err)
	}
	pages, err := b.Live()
	if err != nil || len(pages) != 0 {
		t.Fatal("do not invent lasting knowledge", pages, err, source)
	}
	q, err := b.PendingObservations()
	if err != nil || len(q) != 1 { // only loopBook's unrelated original evidence remains
		t.Fatal("a closed no-knowledge report must not create another mandatory reflection", q, err)
	}
}

func TestWorkCompactResultSurvivesFailedNote(t *testing.T) {
	b, _ := loopBook(t)
	v, err := workCall(t, b, "prepare", map[string]any{"request": "Check routing", "query": "routing"})
	if err != nil {
		t.Fatal(err)
	}
	r, err := workCall(t, b, "record", map[string]any{"receipt": v["receipt"], "result": map[string]any{"changed": "Routing was checked.", "checked": "Synthetic route A stayed in tenant A."}, "notes": []any{map[string]any{"title": "Routing check", "lesson": "Keep tenant ownership.", "applies_to": "routing", "sources": []string{"result"}, "kind": "howto", "scene": "routing", "target": "missing-article"}}})
	if err != nil || r["complete"] != false {
		t.Fatal(r, err)
	}
	s, ok := r["storage"].(map[string]any)
	if !ok {
		t.Fatal("missing explicit storage state", r)
	}
	if s["status"] != "saved-pending" || s["result_source"] == "" {
		t.Fatal("a failed organizer must not lose the original result", s)
	}
	if !strings.Contains(r["next_action"].(string), "pending") {
		t.Fatal("give a bounded recovery action", r)
	}
	if _, err := b.ReadSources([]string{s["result_source"].(string)}); err != nil {
		t.Fatal("saved means original evidence is readable", err)
	}
	errors := r["errors"].([]any)
	ref := errors[0].(map[string]any)["ref"]
	again, err := workCall(t, b, "record", map[string]any{"receipt": v["receipt"], "notes": []any{map[string]any{"ref": ref, "target": "new"}}})
	if err != nil || again["complete"] != true || again["storage"].(map[string]any)["result_source"] != s["result_source"] {
		t.Fatal("recover the remaining note without restating the saved result", again, err)
	}
	t.Run("organizer unavailable", func(t *testing.T) {
		repo, root, _ := planningFixture(t)
		book := notebook.Book{Root: root, Scope: feedbackScope(repo)}
		reasoningPage(t, book, "Routing ownership", "rule", "Routing requires the current tenant owner.")
		old := hookLocalURL
		hookLocalURL = "http://127.0.0.1:1"
		defer func() { hookLocalURL = old }()
		p, err := workCall(t, book, "prepare", map[string]any{"request": "Check routing ownership", "query": "routing ownership"})
		if err != nil {
			t.Fatal(err)
		}
		r, err := workCall(t, book, "record", map[string]any{"receipt": p["receipt"], "result": map[string]any{"changed": "Checked the synthetic routing boundary."}, "notes": []any{map[string]any{"title": "Routing ownership", "lesson": "Keep the tenant owner boundary when routing.", "applies_to": "routing ownership", "kind": "rule", "scene": "routing", "sources": []string{"result"}}}})
		if err != nil || r["complete"] != false || r["storage"].(map[string]any)["status"] != "saved-pending" {
			t.Fatal("a refused connection must leave the original saved and organization pending", r, err)
		}
		id := r["storage"].(map[string]any)["result_source"].(string)
		if _, err := book.ReadSources([]string{id}); err != nil {
			t.Fatal(err)
		}
		errors := r["errors"].([]any)
		required := errors[0].(map[string]any)["ref"]
		hookLocalURL = old
		resumed, e := workCall(t, book, "record", map[string]any{"receipt": p["receipt"], "notes": []any{map[string]any{"ref": required, "target": "new"}}})
		if e != nil {
			t.Fatal(e)
		}
		pending, ok := resumed["pending"].([]any)
		if !ok || len(pending) != 1 {
			t.Fatal("recover the saved draft and return its placement candidates", resumed)
		}
		item := pending[0].(map[string]any)
		candidate := item["candidates"].([]any)[0].(map[string]any)
		page := candidate["article"].([]any)[0].(map[string]any)
		hookLocalURL = old
		retry, err := workCall(t, book, "record", map[string]any{"receipt": p["receipt"], "notes": []any{map[string]any{"ref": item["ref"], "target": candidate["ref"], "edits": []any{map[string]any{"field": "lesson", "before": page["lesson"], "after": "Retain tenant ownership in the checked routing boundary."}}}}})
		if err != nil || retry["complete"] != true || retry["storage"].(map[string]any)["result_source"] != id {
			t.Fatal("resume organization only, preserving the original evidence", retry, err)
		}
	})
}

func TestWorkCompactTwoCallsSatisfyTheRealStop(t *testing.T) {
	repo, root, _ := planningFixture(t)
	runUnreviewedHook(t, "user-prompt-submit", map[string]any{"session_id": "two-calls", "cwd": repo, "prompt": planningTestRequest})
	b := notebook.Book{Root: root, Scope: feedbackScope(repo)}
	p, err := workCall(t, b, "prepare", map[string]any{"session_id": "two-calls", "turn_id": loadSession(root, "two-calls").TurnID, "query": "CSV filters"})
	if err != nil {
		t.Fatal(err)
	}
	r, err := workCall(t, b, "record", map[string]any{"receipt": p["receipt"], "result": map[string]any{"changed": "Checked the requested CSV filter behavior.", "checked": "The existing filters are retained.", "evidence": []string{"current"}}})
	if err != nil || r["complete"] != true {
		t.Fatal(r, err)
	}
	out := runUnreviewedHook(t, "stop", map[string]any{"session_id": "two-calls", "cwd": repo, "last_assistant_message": "The existing filters are retained."})
	if strings.Contains(out, `"decision":"block"`) || strings.Contains(out, `"continue":false`) || reasoningLeft(loadSession(root, "two-calls").Reasoning) != 0 {
		t.Fatal("the real hook must accept the two-call contract", out)
	}
	t.Run("HTTP", func(t *testing.T) {
		h := newServer(serveOpts{storeDir: root, projectScope: feedbackScope(repo)})
		call := func(operation string, fields map[string]any) map[string]any {
			t.Helper()
			raw, _ := json.Marshal(map[string]any{"operation": operation, "work": fields})
			request := httptest.NewRequest("POST", "/v1/wiki/notebook", strings.NewReader(string(raw)))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			h.ServeHTTP(response, request)
			var value map[string]any
			if err := json.Unmarshal(response.Body.Bytes(), &value); err != nil || response.Code != 200 {
				t.Fatal("the HTTP parser must share the same work contract", response.Code, err)
			}
			return value
		}
		p := call("prepare", map[string]any{"request": "Explain the existing filter.", "query": "filter", "max_bytes": 8192})
		r := call("record", map[string]any{"receipt": p["receipt"], "result": map[string]any{"changed": "Explained the existing filter."}})
		if r["complete"] != true || r["reasoning"].(map[string]any)["complete"] != true || r["storage"].(map[string]any)["status"] != "saved" {
			t.Fatal("the HTTP path must also close in two calls", r)
		}
	})
}
