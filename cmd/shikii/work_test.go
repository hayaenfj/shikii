package main

import (
	"encoding/json"
	"errors"
	"net/http/httptest"
	"os"
	"time"

	"path/filepath"
	"strings"
	"testing"

	"github.com/hayaenfj/shikii/internal/store"

	"github.com/hayaenfj/shikii/internal/notebook"
)

func workCall(t *testing.T, b notebook.Book, operation string, fields map[string]any) (map[string]any, error) {
	t.Helper()
	v, err := improvementCall(t, b, map[string]any{"operation": operation, "work": fields})
	if err != nil {
		return nil, err
	}
	return loopObject(t, v), nil
}

func TestWorkReceiptSavesPlanAndEvidenceTogether(t *testing.T) {
	repo, root, _ := planningFixture(t)
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": planningTestRequest})
	turn := loadSession(root, "planning").TurnID
	b := notebook.Book{Root: root, Scope: feedbackScope(repo)}
	v, err := workCall(t, b, "prepare", map[string]any{"session_id": "planning", "turn_id": turn, "query": "CSV export filters"})
	if err != nil {
		t.Fatal("prepare must retain the recording references", err)
	}
	receipt, ok := v["receipt"].(string)
	if !ok || receipt == "" {
		t.Fatal(v)
	}
	payload := map[string]any{"receipt": receipt, "agreed": map[string]any{"summary": "Implement the requested CSV export.", "quotes": []any{map[string]any{"ref": "current", "text": planningTestRequest}}}, "observations": []any{map[string]any{"kind": "test", "title": "CSV result", "content": "The synthetic CSV keeps the existing filters."}}, "notes": []any{map[string]any{"title": "CSV export keeps filters", "lesson": "Preserve the existing filters in CSV exports.", "applies_to": "CSV export", "body": "The checked synthetic result kept the configured filters.", "kind": "howto", "scene": "exporting CSV", "sources": []string{"new1"}}}}
	saved, err := workCall(t, b, "record", payload)
	if err != nil || saved["complete"] != true {
		t.Fatal(saved, err)
	}
	if loadSession(root, "planning").Planning.Result == nil {
		t.Fatal("the existing planning ritual must see the saved result")
	}
	pages, err := b.Live()
	if err != nil || len(pages) != 1 {
		t.Fatal(pages, err)
	}
	first := pages[0]
	view, err := b.ReadSources([]string{first.Sources[0].ID})
	if err != nil {
		t.Fatal(err)
	}
	observations := view["observations"].([]notebook.Observation)
	if len(observations) != 1 || observations[0].ObservedAt.IsZero() || observations[0].Version == "" {
		t.Fatal("the recorder must supply time and recording version", view)
	}
	again, err := workCall(t, b, "record", payload)
	if err != nil || again["complete"] != true {
		t.Fatal(again, err)
	}
	pages, err = b.Live()
	if err != nil || len(pages) != 1 || pages[0].Revision != first.Revision {
		t.Fatal("a resend must preserve the first save", pages, err)
	}
}

func TestWorkReceiptRejectsTamperingAndAnotherProject(t *testing.T) {
	b, _ := loopBook(t)
	v, err := workCall(t, b, "prepare", map[string]any{"request": "Inspect invoice routing", "query": "invoice routing"})
	if err != nil {
		t.Fatal(err)
	}
	if v["planning_required"] != false {
		t.Fatal("an agent-supplied task must not ask for human planning agreement", v)
	}
	receipt := v["receipt"].(string)
	other, _ := loopBook(t)
	if _, err = workCall(t, other, "record", map[string]any{"receipt": receipt, "reviewed": []string{"source1"}}); err == nil {
		t.Fatal("a receipt belongs to its prepared project")
	}
	if _, err = workCall(t, b, "record", map[string]any{"receipt": receipt + "x", "reviewed": []string{"source1"}}); err == nil {
		t.Fatal("a changed receipt must be rejected")
	}
	if _, err = workCall(t, b, "record", map[string]any{"receipt": receipt, "agreed": map[string]any{"summary": "Agent supplied context is not a human agreement.", "quotes": []any{map[string]any{"ref": "current", "text": "Inspect invoice routing"}}}}); err == nil {
		t.Fatal("standalone context must not fabricate a human agreement")
	}
}

func TestWorkReceiptRejectsAnOldTurn(t *testing.T) {
	repo, root, _ := planningFixture(t)
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": planningTestRequest})
	b := notebook.Book{Root: root, Scope: feedbackScope(repo)}
	v, err := workCall(t, b, "prepare", map[string]any{"session_id": "planning", "turn_id": loadSession(root, "planning").TurnID, "query": "CSV filters"})
	if err != nil {
		t.Fatal(err)
	}
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": "Explain the invoice ownership policy"})
	if _, err = workCall(t, b, "record", map[string]any{"receipt": v["receipt"], "observations": []any{map[string]any{"kind": "agent", "title": "Old work", "content": "Do not attach this to the new request."}}}); err == nil {
		t.Fatal("the old receipt must not attach records to the new task")
	}
}

func TestWorkPartialSaveDoesNotRecaptureEvidence(t *testing.T) {
	b, _ := loopBook(t)
	v, err := workCall(t, b, "prepare", map[string]any{"request": "Check invoice routing", "query": "invoice routing"})
	if err != nil {
		t.Fatal(err)
	}
	payload := map[string]any{"receipt": v["receipt"], "observations": []any{map[string]any{"kind": "test", "title": "First checked result", "content": "The first result was checked."}, map[string]any{"kind": "invalid", "title": "Second result", "content": "The second result was checked."}}}
	first, err := workCall(t, b, "record", payload)
	if err != nil || first["complete"] != false {
		t.Fatal(first, err)
	}
	before, err := filepath.Glob(filepath.Join(b.Root, "records", "wiki-sources", "*", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	payload["observations"].([]any)[1].(map[string]any)["kind"] = "test"
	again, err := workCall(t, b, "record", payload)
	if err != nil || again["complete"] != true {
		t.Fatal(again, err)
	}
	after, err := filepath.Glob(filepath.Join(b.Root, "records", "wiki-sources", "*", "*.json"))
	if err != nil || len(after) != len(before)+1 {
		t.Fatal("only the previously failed observation may be added", len(before), len(after), err)
	}
	raw, _ := json.Marshal(again)
	if !strings.Contains(string(raw), "observed1") {
		t.Fatal("saved observation references must be reusable", string(raw))
	}
}

func TestWorkReflectionUsesPreparedEvidenceAndKeepsTruthBoundary(t *testing.T) {
	st, scope, _ := reviewFixture(t)
	id, _ := reviewSeed(t, st, scope, 1)
	b := notebook.Book{Root: st.Dir, Scope: scope}
	v, err := workCall(t, b, "prepare", map[string]any{"query": "README language"})
	if err != nil {
		t.Fatal(err)
	}
	pending := v["pending"].([]any)
	if len(pending) != 1 {
		t.Fatal(v)
	}
	source := pending[0].(map[string]any)
	target := source["reviews"].([]any)[0].(map[string]any)
	if _, ok := target["original_inputs"]; !ok {
		t.Fatal("prepared review must retain original inputs and text status", target)
	}
	result, err := workCall(t, b, "record", map[string]any{"receipt": v["receipt"], "reviews": []any{map[string]any{"ref": target["ref"], "answer": "no", "reason": "The source reply says the requested translation is incomplete."}}, "reviewed": []string{source["ref"].(string)}})
	if err != nil || result["complete"] != true {
		t.Fatal(result, err)
	}
	q, err := reflectQuery(st.Dir, scope)
	if err != nil || len(q.Sources) != 0 {
		t.Fatal(q, err, id)
	}
	_, labels, err := st.Counts("done-as-asked")
	if err != nil || personTruths(labels) != 0 {
		t.Fatal("prepared scores are never human truths", labels, err)
	}
}

func TestWorkRetryAfterPublishedSourceAndArticle(t *testing.T) {
	for _, kind := range []string{"observation", "note"} {
		t.Run(kind, func(t *testing.T) {
			b, _ := loopBook(t)
			v, err := workCall(t, b, "prepare", map[string]any{"query": "CSV filters"})
			if err != nil {
				t.Fatal(err)
			}
			payload := map[string]any{"receipt": v["receipt"], "observations": []any{map[string]any{"kind": "test", "title": "CSV check", "content": "The CSV keeps filters."}}, "notes": []any{map[string]any{"title": "CSV filters", "lesson": "Keep the filters.", "applies_to": "CSV export", "kind": "howto", "scene": "exporting", "sources": []string{"new1"}}}}
			old := workAfterWrite
			defer func() { workAfterWrite = old }()
			failed := false
			workAfterWrite = func(k string) error {
				if k == kind && !failed {
					failed = true
					return errors.New("synthetic checkpoint failure")
				}
				return nil
			}
			first, err := workCall(t, b, "record", payload)
			if err != nil || first["complete"] != false || !failed {
				t.Fatal(first, err)
			}
			before, _ := filepath.Glob(filepath.Join(b.Root, "records", "wiki-sources", "*", "*.json"))
			workAfterWrite = old
			again, err := workCall(t, b, "record", payload)
			if err != nil || again["complete"] != true {
				t.Fatal(again, err)
			}
			after, _ := filepath.Glob(filepath.Join(b.Root, "records", "wiki-sources", "*", "*.json"))
			if len(before) != len(after) {
				t.Fatal("retry duplicated published evidence", before, after)
			}
			pages, err := b.Live()
			if err != nil || len(pages) != 1 {
				t.Fatal(pages, err)
			}
			history, _ := filepath.Glob(filepath.Join(b.Root, "wiki", "lessons", "*", "revisions", "*.md"))
			if len(history) != 1 {
				t.Fatal("retry duplicated article revision", history)
			}
		})
	}
}

func TestWorkPreparedPatchKeepsOtherTextAndRejectsStaleRevision(t *testing.T) {
	b, s := loopBook(t)
	p := loopPage(t, b, s, map[string]any{"title": "CSV filters", "applies_to": "CSV filters", "body": "Preserve tenant filters. Keep totals unchanged."})
	v, err := workCall(t, b, "prepare", map[string]any{"query": "CSV filters"})
	if err != nil {
		t.Fatal(err)
	}
	knowledge := v["knowledge"].([]any)
	if len(knowledge) != 1 {
		t.Fatal(v)
	}
	ref := knowledge[0].(map[string]any)["ref"]
	payload := map[string]any{"receipt": v["receipt"], "notes": []any{map[string]any{"target": ref, "sources": []string{"evidence1"}, "edits": []any{map[string]any{"field": "body", "before": "Preserve tenant filters.", "after": "Preserve tenant and date filters."}}}}}
	// loopPage consumed the initial observation, so use new checked evidence.
	payload["observations"] = []any{map[string]any{"kind": "test", "title": "Filter check", "content": "Tenant and date filters were checked."}}
	payload["notes"].([]any)[0].(map[string]any)["sources"] = []string{"new1"}
	saved, err := workCall(t, b, "record", payload)
	if err != nil || saved["complete"] != true {
		t.Fatal(saved, err)
	}
	pages, _ := b.Live()
	if len(pages) != 1 || pages[0].Body != "Preserve tenant and date filters. Keep totals unchanged." {
		t.Fatal(pages)
	}
	payload["notes"].([]any)[0].(map[string]any)["edits"] = []any{map[string]any{"field": "lesson", "before": p.Lesson, "after": "Different stale update."}}
	stale, err := workCall(t, b, "record", payload)
	if err != nil || stale["complete"] != false {
		t.Fatal("prepared old revision must not overwrite a later edit", stale, err)
	}
}

func TestWorkNewPreparationAndExpiredReceiptAreRejected(t *testing.T) {
	repo, root, _ := planningFixture(t)
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": planningTestRequest})
	b := notebook.Book{Root: root, Scope: feedbackScope(repo)}
	args := map[string]any{"session_id": "planning", "turn_id": loadSession(root, "planning").TurnID, "query": "CSV filters"}
	old, err := workCall(t, b, "prepare", args)
	if err != nil {
		t.Fatal(err)
	}
	fresh, err := workCall(t, b, "prepare", args)
	if err != nil {
		t.Fatal(err)
	}
	payload := map[string]any{"receipt": old["receipt"], "observations": []any{map[string]any{"title": "Old context", "content": "Old context must be refused."}}}
	if _, err = workCall(t, b, "record", payload); err == nil {
		t.Fatal("a new preparation invalidates the old receipt")
	}
	s := loadSession(root, "planning")
	s.Planning.Work.At = time.Now().Add(-hookSessionTTL - time.Minute)
	data, _ := json.Marshal(s)
	if err = os.WriteFile(sessionPath(root, "planning"), data, 0600); err != nil {
		t.Fatal(err)
	}
	payload["receipt"] = fresh["receipt"]
	if _, err = workCall(t, b, "record", payload); err == nil {
		t.Fatal("expired prepared context must be refused")
	}
}

func TestWorkTransportAndSchema(t *testing.T) {
	repo := gitRepo(t)
	root := hookEnv(t, nil, repo)
	t.Chdir(repo)
	old := stdin
	defer func() { stdin = old }()
	stdin = strings.NewReader(`{"query":"CSV filters","request":"Check CSV filters"}`)
	raw := capture(t, func() error { return cmdWork([]string{"prepare"}) })
	var v map[string]any
	if err := json.Unmarshal([]byte(raw), &v); err != nil || v["receipt"] == nil {
		t.Fatal(raw, err)
	}
	h := newServer(serveOpts{storeDir: root, projectScope: feedbackScope(repo)})
	payload, _ := json.Marshal(map[string]any{"operation": "record", "work": map[string]any{"receipt": v["receipt"], "observations": []any{map[string]any{"kind": "test", "title": "Synthetic result", "content": "Filters passed."}}}})
	code, body, err := callTool(h, "shikii_wiki", payload)
	if err != nil || code != 200 || !strings.Contains(strings.ReplaceAll(string(body), " ", ""), `"complete":true`) {
		t.Fatal(code, string(body), err)
	}
	r := httptest.NewRequest("POST", "/v1/wiki/notebook", strings.NewReader(string(payload)))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 || !strings.Contains(strings.ReplaceAll(w.Body.String(), " ", ""), `"complete":true`) {
		t.Fatal(w.Code, w.Body.String())
	}
	for _, tool := range mcpTools {
		if tool["name"] != "shikii_wiki" {
			continue
		}
		props := tool["inputSchema"].(map[string]any)["properties"].(map[string]any)
		if props["work"] == nil {
			t.Fatal("MCP must advertise the prepared recording object")
		}
	}
	if !strings.Contains(planningNotice("s", "0123456789abcdef"), "work prepare") {
		t.Fatal("hook must recommend the short preparation flow")
	}
}

func TestWorkRejectsUnpreparedReferencesAndPreservesPartialResult(t *testing.T) {
	b, _ := loopBook(t)
	v, err := workCall(t, b, "prepare", map[string]any{"query": "CSV filters"})
	if err != nil {
		t.Fatal(err)
	}
	result, err := workCall(t, b, "record", map[string]any{"receipt": v["receipt"], "observations": []any{map[string]any{"title": "Checked result", "content": "Synthetic evidence."}}, "notes": []any{map[string]any{"title": "CSV filters", "lesson": "Keep filters.", "kind": "howto", "scene": "exporting", "applies_to": "CSV export", "sources": []string{store.NewTraceID()}}}})
	if err != nil || result["complete"] != false || len(result["saved"].([]any)) != 1 {
		t.Fatal(result, err)
	}
	pages, err := b.Live()
	if err != nil || len(pages) != 0 {
		t.Fatal("unknown aliases must not publish a note", pages, err)
	}
	repo, root, _ := planningFixture(t)
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": planningTestRequest})
	hooked := notebook.Book{Root: root, Scope: feedbackScope(repo)}
	prepared, err := workCall(t, hooked, "prepare", map[string]any{"session_id": "planning", "turn_id": loadSession(root, "planning").TurnID, "query": "CSV filters"})
	if err != nil {
		t.Fatal(err)
	}
	partial, err := workCall(t, hooked, "record", map[string]any{"receipt": prepared["receipt"], "observations": []any{map[string]any{"title": "Checked observation", "content": "Keep this successful capture in the reply."}}, "agreed": map[string]any{"summary": "Unknown quotation reference.", "quotes": []any{map[string]any{"ref": "invented", "text": planningTestRequest}}}})
	if err != nil || partial["complete"] != false || len(partial["saved"].([]any)) != 1 {
		t.Fatal("plan errors must retain explicit partial results", partial, err)
	}

}

func TestWorkAmbiguousPlacementReturnsContentChoiceAndReplaysOriginalSave(t *testing.T) {
	repo, root, _ := planningFixture(t)
	b := notebook.Book{Root: root, Scope: feedbackScope(repo)}
	o, err := b.CaptureObserved(notebook.Observation{Kind: "test", Title: "Existing route", Content: "Checked existing invoice routing.", Reference: "synthetic", Version: "1", ObservedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	source := o.ID
	loopPage(t, b, source, map[string]any{"title": "Invoice routing", "lesson": "Check totals before invoice routing.", "applies_to": "invoice routing", "body": "Keep tenant ownership and check totals."})
	v, err := workCall(t, b, "prepare", map[string]any{"query": "invoice routing"})
	if err != nil {
		t.Fatal(err)
	}
	payload := map[string]any{"receipt": v["receipt"], "observations": []any{map[string]any{"title": "New route check", "content": "Synthetic routing verified."}}, "notes": []any{map[string]any{"title": "Invoice routing procedures", "lesson": "Check totals and route invoices.", "applies_to": "invoice routing", "kind": "howto", "scene": "routing invoices", "sources": []string{"new1"}}}}
	first, err := workCall(t, b, "record", payload)
	if err != nil || first["complete"] != false {
		t.Fatal(first, err)
	}
	pending, ok := first["pending"].([]any)
	if !ok {
		t.Fatal(first)
	}
	if len(pending) != 1 {
		t.Fatal(first)
	}
	choice := pending[0].(map[string]any)
	if len(choice["candidates"].([]any)) == 0 {
		t.Fatal("ambiguous placement needs candidate content", choice)
	}
	pages, _ := b.Live()
	if len(pages) != 1 {
		t.Fatal("unresolved placement must not write a second article", pages)
	}
	resolve := map[string]any{"receipt": v["receipt"], "notes": []any{map[string]any{"ref": choice["ref"], "target": "new"}}}
	saved, err := workCall(t, b, "record", resolve)
	if err != nil || saved["complete"] != true {
		t.Fatal(saved, err)
	}
	replay, err := workCall(t, b, "record", payload)
	if err != nil || replay["complete"] != true {
		t.Fatal("original resend must recover the resolved save", replay, err)
	}
	pages, _ = b.Live()
	if len(pages) != 2 {
		t.Fatal(pages)
	}
	fresh, err := workCall(t, b, "prepare", map[string]any{"query": "invoice routing"})
	if err != nil {
		t.Fatal(err)
	}
	payload["receipt"] = fresh["receipt"]
	payload["notes"].([]any)[0].(map[string]any)["title"] = "Invoice routing"
	ambiguous, err := workCall(t, b, "record", payload)
	if err != nil || ambiguous["complete"] != false {
		t.Fatal(ambiguous, err)
	}
	noteRef := ambiguous["pending"].([]any)[0].(map[string]any)["ref"]
	duplicate, err := workCall(t, b, "record", map[string]any{"receipt": fresh["receipt"], "notes": []any{map[string]any{"ref": noteRef, "target": "new"}}})
	if err != nil || duplicate["complete"] != false {
		t.Fatal("creating a new note must never overwrite an existing title", duplicate, err)
	}
	after, _ := b.Live()
	if len(after) != len(pages) {
		t.Fatal(after)
	}
	for i, p := range pages {
		if after[i].ID != p.ID || after[i].Revision != p.Revision {
			t.Fatal("the existing article was overwritten", pages, after)
		}
	}

}

func TestWorkMemoryRequiresDecisionEvidenceAndKeepsOriginalAssessment(t *testing.T) {
	st, scope, _, _, _ := memoryEffectFixture(t)
	memoryEffectPrompt(t, scope, "work-memory")
	memoryEffectComplete(t, st, scope, "work-memory")
	b := notebook.Book{Root: st.Dir, Scope: scope}
	v, err := workCall(t, b, "prepare", map[string]any{"query": "scope confirmation"})
	if err != nil {
		t.Fatal(err)
	}
	var ref string
	for _, raw := range v["pending"].([]any) {
		for _, m := range raw.(map[string]any)["memory"].([]any) {
			ref = m.(map[string]any)["ref"].(string)
		}
	}
	if ref == "" {
		t.Fatal("prepared pending work must include delivered memory", v)
	}
	payload := map[string]any{"receipt": v["receipt"], "memory": []any{map[string]any{"ref": ref, "result": "helpful", "decision": "", "evidence": ""}}}
	reviews := []any{}
	reviewed := []string{}
	for _, raw := range v["pending"].([]any) {
		source := raw.(map[string]any)
		if source["head"] == true {
			continue
		}
		reviewed = append(reviewed, source["ref"].(string))
		for _, r := range source["reviews"].([]any) {
			reviews = append(reviews, map[string]any{"ref": r.(map[string]any)["ref"], "answer": "unknown", "reason": "No checked outcome for this synthetic task."})
		}
	}
	payload["reviews"], payload["reviewed"] = reviews, reviewed
	bad, err := workCall(t, b, "record", payload)
	if err != nil || bad["complete"] != false {
		t.Fatal("reading is not useful decision evidence", bad, err)
	}
	payload["memory"].([]any)[0] = map[string]any{"ref": ref, "result": "unknown", "decision": "", "evidence": "There is no recorded decision showing an effect."}
	saved, err := workCall(t, b, "record", payload)
	if err != nil || saved["complete"] != true {
		t.Fatal(saved, err)
	}
	payload["memory"].([]any)[0] = map[string]any{"ref": ref, "result": "helpful", "decision": "Invented later effect", "evidence": "Different later result"}
	rewritten, err := workCall(t, b, "record", payload)
	if err != nil || rewritten["complete"] != false {
		t.Fatal("first assessment must remain unchanged", rewritten, err)
	}
}

func TestWorkEscapedInputKeepsFullEvidenceAndRejectsUnknownFields(t *testing.T) {
	repo := gitRepo(t)
	root := hookEnv(t, nil, repo)
	t.Chdir(repo)
	b := notebook.Book{Root: root, Scope: feedbackScope(repo)}
	v, err := workCall(t, b, "prepare", map[string]any{"query": "CSV filters"})
	if err != nil {
		t.Fatal(err)
	}
	old := stdin
	defer func() { stdin = old }()
	evidence := strings.Repeat("\u0061", 64000)
	input := `{"receipt":"` + v["receipt"].(string) + `","observations":[{"kind":"test","title":"Escaped evidence","content":"` + evidence + `"}]}`
	stdin = strings.NewReader(input)
	raw := capture(t, func() error { return cmdWork([]string{"record"}) })
	if !strings.Contains(raw, `"complete":true`) {
		t.Fatal(raw)
	}
	payload := map[string]any{"receipt": v["receipt"], "truth": "yes", "observations": []any{map[string]any{"title": "No truth field", "content": "Refuse unknown fields."}}}
	data, _ := json.Marshal(payload)
	stdin = strings.NewReader(string(data))
	if err := cmdWork([]string{"record"}); err == nil {
		t.Fatal("unknown truth or storage fields must not be silently ignored")
	}
}
