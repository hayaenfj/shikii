package main

import (
	"strings"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/notebook"
)

// The refusals of work that kept coming back in this machine's agent sessions (counted on 2026-11-11) said that something was wrong
// but not what or what to do: "prepare reads context; send content with record" (which field?), "only the context of the ongoing
// request in the same project can be handled" (which of four reasons?), "unknown prepared memory reference" (which refs exist?).
// Each now names the cause and the next call.
func TestWorkRefusalsNameTheCauseAndTheNextCall(t *testing.T) {
	repo, root, _ := planningFixture(t)
	b := notebook.Book{Root: root, Scope: feedbackScope(repo)}
	prompt := func(text string) string {
		runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": text})
		return loadSession(root, "planning").TurnID
	}
	lacks := func(name string, err error, want ...string) {
		t.Helper()
		if err == nil {
			t.Errorf("%s must be refused", name)
			return
		}
		for _, w := range want {
			if !strings.Contains(err.Error(), w) {
				t.Errorf("%s: the refusal lacks %q: %v", name, w, err)
			}
		}
	}

	// prepare names the content fields it was sent
	turn := prompt(planningTestRequest)
	_, err := workCall(t, b, "prepare", map[string]any{"session_id": "planning", "turn_id": turn, "query": "CSV export", "continues_previous": true})
	lacks("prepare with continues_previous", err, "prepare reads context; send content with record", "prepare was sent continues_previous")
	_, err = workCall(t, b, "prepare", map[string]any{"session_id": "planning", "turn_id": turn, "query": "CSV export", "result": map[string]any{"changed": "x"}, "observations": []any{map[string]any{"kind": "test", "title": "t", "content": "c"}}})
	lacks("prepare with a result and observations", err, "prepare was sent result, observations")

	// a receipt of an earlier request, after a newer request started
	first, err := workCall(t, b, "prepare", map[string]any{"session_id": "planning", "turn_id": turn, "query": "CSV export"})
	if err != nil {
		t.Fatal(err)
	}
	prompt("Also check the totals of the CSV export.")
	_, err = workCall(t, b, "record", map[string]any{"receipt": first["receipt"], "result": map[string]any{"changed": "Nothing.", "checked": "Read the export."}})
	lacks("a record with the receipt of an earlier request", err, "only the context of the ongoing request in the same project can be handled", "a newer request has started in this session", "prepare again with the ids of the latest hook notice")

	// the other reasons, on the session record itself
	frame := func(change func(*hookSession)) error {
		s := &hookSession{Scope: "/project", TurnID: "0123456789abcdef", Planning: &planningFrame{TurnID: "0123456789abcdef", At: time.Now()}}
		change(s)
		return currentPlanningFrame(s, "0123456789abcdef", "/project")
	}
	if err := frame(func(*hookSession) {}); err != nil {
		t.Errorf("the ongoing request is accepted: %v", err)
	}
	lacks("another project", frame(func(s *hookSession) { s.Scope = "/elsewhere" }), "only the context of the ongoing request in the same project can be handled", "another project folder", "run the command in that project")
	lacks("no organizing for this turn", frame(func(s *hookSession) { s.Planning = nil }), "a newer request has started in this session")
	lacks("a closed turn", frame(func(s *hookSession) { s.Judged, s.Planning.Result = true, &planningReceipt{} }), "closed at the end of its turn", "the next request's hook notice")
	lacks("an old request", frame(func(s *hookSession) { s.Planning.At = time.Now().Add(-hookSessionTTL - time.Hour) }), "older than", "prepare again")
	if err := frame(func(s *hookSession) { s.Judged = true }); err != nil {
		t.Errorf("a turn judged before its organizing was saved still takes it: %v", err)
	}

	// a memory or review reference the receipt did not prepare
	turn = loadSession(root, "planning").TurnID
	second, err := workCall(t, b, "prepare", map[string]any{"session_id": "planning", "turn_id": turn, "query": "CSV export"})
	if err != nil {
		t.Fatal(err)
	}
	saved, err := workCall(t, b, "record", map[string]any{"receipt": second["receipt"],
		"memory":  []any{map[string]any{"ref": "knowledge1", "result": "helpful", "decision": "d", "evidence": "e"}},
		"reviews": []any{map[string]any{"ref": "lesson:0123456789abcdef", "answer": "yes", "reason": "r"}}})
	if err != nil {
		t.Fatal(err)
	}
	items := map[string]string{}
	for _, raw := range saved["errors"].([]any) {
		item := raw.(map[string]any)
		items[item["kind"].(string)] = item["error"].(string)
	}
	for kind, want := range map[string][]string{
		"memory": {`unknown prepared memory reference "knowledge1"`, "this receipt prepared no memory to assess", "leave memory out"},
		"review": {`unknown prepared review reference "lesson:0123456789abcdef"`, "this receipt prepared no review", "leave reviews out"},
	} {
		for _, w := range want {
			if !strings.Contains(items[kind], w) {
				t.Errorf("the %s refusal lacks %q: %q", kind, w, items[kind])
			}
		}
	}
	// with prepared references, the refusal lists them
	w := &workState{Memory: map[string]workMemoryRef{"memory2": {}, "memory10": {}, "memory1": {}}}
	if got := unknownPreparedRef("memory", "knowledge1", sortedRefs(w.Memory), "pending[].memories[]"); !strings.Contains(got, "the refs of pending[].memories[] in the reply of prepare: memory1, memory2, memory10") {
		t.Errorf("the prepared refs are listed in their order: %q", got)
	}

	// a source reference that is this call's own observation, which was not saved
	saved, err = workCall(t, b, "record", map[string]any{"receipt": second["receipt"],
		"observations": []any{map[string]any{"kind": "check", "title": "t", "content": "c"}},
		"notes":        []any{map[string]any{"title": "CSV totals", "lesson": "Check the totals.", "applies_to": "CSV export", "kind": "rule", "scene": "when exporting CSV", "body": "b", "sources": []any{"new1"}}}})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, raw := range saved["errors"].([]any) {
		text, _ := raw.(map[string]any)["error"].(string)
		if strings.Contains(text, `unknown source reference "new1"`) {
			found = true
			for _, w := range []string{"observation 1 of this call was not saved", "resend it with the note"} {
				if !strings.Contains(text, w) {
					t.Errorf("the refusal of a note citing an unsaved observation lacks %q: %q", w, text)
				}
			}
		}
	}
	if !found {
		t.Errorf("a note citing an observation that was not saved is refused: %v", saved["errors"])
	}
	if got := unknownSourceRef("evidence9", 0, []string{"current", "source1", "result"}); !strings.Contains(got, `unknown source reference "evidence9"`) || !strings.Contains(got, "usable here: current, result, source1") {
		t.Errorf("an unknown source reference lists the usable ones: %q", got)
	}
}

// related in a note names articles the receipt has read. An agent that wrote page ids there (the ids it sees in knowledge) was
// answered "unknown related article" and nothing else (2026-11-12, while recording this very work). The id of an article the
// receipt read is accepted like its ref, and any other value is answered with the refs it could use and how to read another page.
func TestRelatedInANoteTakesTheArticlesTheReceiptRead(t *testing.T) {
	repo, root, _ := planningFixture(t)
	t.Chdir(repo)
	b := notebook.Book{Root: root, Scope: feedbackScope(repo)}
	source, err := b.Capture(notebook.Observation{Kind: "test", Title: "Synthetic check", Content: "The CSV export keeps its filters.", Reference: "synthetic", Version: "1", ObservedAt: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	body := "The export keeps the filters."
	page, err := b.Ingest(notebook.Draft{Sources: []string{source.ID}, Title: "CSV export keeps filters", Lesson: "Keep the filters when exporting CSV.", AppliesTo: "CSV export", Kind: "rule", Scene: "when exporting CSV", Body: &body})
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := workCall(t, b, "prepare", map[string]any{"query": "CSV export filters", "request": "Check the CSV export"})
	if err != nil {
		t.Fatal(err)
	}
	ref := ""
	for _, raw := range prepared["knowledge"].([]any) {
		if entry := raw.(map[string]any); entry["id"] == page.Page.ID {
			ref, _ = entry["ref"].(string)
		}
	}
	if ref == "" {
		t.Fatalf("the page is among the prepared knowledge: %v", prepared["knowledge"])
	}
	note := func(title string, related ...string) map[string]any {
		return map[string]any{"receipt": prepared["receipt"],
			"observations": []any{map[string]any{"kind": "test", "title": "Totals " + title, "content": "The totals of " + title + " match."}},
			"notes":        []any{map[string]any{"target": "new", "title": "CSV totals " + title, "lesson": "Check the totals of " + title + ".", "applies_to": "CSV export totals", "kind": "rule", "scene": "when exporting CSV", "body": "Checked " + title + ".", "sources": []any{"new1"}, "related": related}}}
	}
	refused := func(saved map[string]any) string {
		text := ""
		for _, raw := range saved["errors"].([]any) {
			if item := raw.(map[string]any); item["kind"] == "note" {
				text, _ = item["error"].(string)
			}
		}
		return text
	}
	saved, err := workCall(t, b, "record", note("with an unread page", "0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	got := refused(saved)
	for _, w := range []string{`unknown related article "0123456789abcdef"`, "related takes the articles this receipt read: " + ref, "prepare with this receipt and query lesson:0123456789abcdef", "or leave it out"} {
		if !strings.Contains(got, w) {
			t.Errorf("the refusal of an unread page in related lacks %q: %q", w, got)
		}
	}
	for _, related := range []string{ref, page.Page.ID} {
		saved, err = workCall(t, b, "record", note("related by "+related, related))
		if err != nil {
			t.Fatal(err)
		}
		if errs, _ := saved["errors"].([]any); len(errs) > 0 && strings.Contains(refused(saved), "related") {
			t.Errorf("related %q, an article the receipt read, is accepted: %v", related, errs)
		}
	}
}
