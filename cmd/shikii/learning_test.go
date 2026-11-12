package main

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/notebook"
)

func learningValue(t *testing.T, value any) map[string]any {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func learningConnections(a, b notebook.Page) []any {
	return []any{
		map[string]any{"id": a.ID, "revision": a.Revision, "quote": a.Body, "conditions": "Before replacing a shared artifact", "reason": "Preserves earlier work", "relation": "supports"},
		map[string]any{"id": b.ID, "revision": b.Revision, "quote": b.Body, "conditions": "When another writer may act", "reason": "Different ownership needs rechecking", "relation": "counterexample"},
	}
}

func TestLearningConnectionsRetainDistinctExperiences(t *testing.T) {
	b, s := loopBook(t)
	a := loopPage(t, b, s, map[string]any{"title": "Binary replacement experience", "body": "Preserve the previous artifact before replacement."})
	c := loopPage(t, b, s, map[string]any{"title": "Wiki replacement experience", "body": "Check the current revision before a shared edit."})
	p := loopPage(t, b, s, map[string]any{"title": "Shared replacement insight", "learning": map[string]any{"connections": learningConnections(a, c)}})
	l, ok := learningValue(t, p)["learning"].(map[string]any)
	if !ok || len(l["connections"].([]any)) != 2 {
		t.Fatal("a synthesized insight must retain both pinned experiences and their conditions", p)
	}
	v, err := improvementCall(t, b, map[string]any{"operation": "consult", "request": "Shared replacement insight", "proposal": "Reuse the insight"})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(v)
	if !strings.Contains(string(raw), `"connections"`) || !strings.Contains(string(raw), c.ID) {
		t.Fatal("consult must deliver the connected evidence", string(raw))
	}
}

func TestLearningProbeRetainsCheckAndObservedResult(t *testing.T) {
	b, s := loopBook(t)
	probe := map[string]any{"question": "Does splitting improve retrieval?", "method": "Compare the same task before and after a split.", "reconsider_when": "A necessary exception is lost.", "results": []any{map[string]any{"conditions": "Synthetic two-article fixture", "conclusion": "Both exceptions were retained.", "assessment": "supports", "sources": []string{s}}}}
	p := loopPage(t, b, s, map[string]any{"title": "Retrieval hypothesis", "learning": map[string]any{"probe": probe}})
	l, ok := learningValue(t, p)["learning"].(map[string]any)
	if !ok || l["probe"] == nil {
		t.Fatal("a hypothesis needs its check, reconsideration condition and source-backed reported result", p)
	}
	view, err := b.Query("lesson:" + p.ID + "@" + p.Revision)
	if err != nil || learningValue(t, view.Lessons[0])["learning"] == nil {
		t.Fatal("the exact revision must preserve the investigation", view, err)
	}
}

func TestLearningPracticeReachesReuseAndWorkUpdates(t *testing.T) {
	b, s := loopBook(t)
	practice := map[string]any{"steps": []any{map[string]any{"condition": "Before a shared edit", "action": "Read the current revision", "avoid": "Replacing another writer's work", "check": "The source revision still matches", "sources": []string{s}}}}
	p := loopPage(t, b, s, map[string]any{"title": "Quartz editing practice", "applies_to": "Quartz editing", "kind": "howto", "learning": map[string]any{"practice": practice}})
	v, err := improvementCall(t, b, map[string]any{"operation": "reuse", "request": "Quartz editing", "conditions": "Another agent also edits the wiki"})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(v)
	steps := learningValue(t, v)["steps"].([]any)
	if len(steps) != 1 || steps[0].(map[string]any)["practice"] == nil || !strings.Contains(string(raw), "Replacing another writer's work") {
		t.Fatal("reuse must deliver the versioned project practice", string(raw))
	}
	prepared, err := workCall(t, b, "prepare", map[string]any{"query": "lesson:" + p.ID, "request": "Improve Quartz editing"})
	if err != nil {
		t.Fatal(err)
	}
	ref := prepared["knowledge"].([]any)[0].(map[string]any)["ref"]
	updated, err := workCall(t, b, "record", map[string]any{"receipt": prepared["receipt"], "notes": []any{map[string]any{"target": ref, "sources": []string{s}, "change_reason": "Retain the reported practice with its new condition.", "learning": map[string]any{"practice": practice}}}})
	// Prepared source aliases, rather than arbitrary raw source ids, are required.
	if err == nil && updated["complete"] == true {
		t.Fatal("work must not accept an unprepared raw source id")
	}
	step := practice["steps"].([]any)[0].(map[string]any)
	step["sources"] = []string{"new1"}
	step["check"] = "Recheck the saved revision immediately before writing"
	updated, err = workCall(t, b, "record", map[string]any{"receipt": prepared["receipt"], "observations": []any{map[string]any{"kind": "test", "title": "Second agent's practice check", "content": "The synthetic stale revision was refused before writing."}}, "notes": []any{map[string]any{"target": ref, "sources": []string{"new1"}, "change_reason": "Add the checked second-writer condition.", "learning": map[string]any{"practice": practice}}}})
	if err != nil || updated["complete"] != true {
		t.Fatal("work record must revise the practice using prepared evidence", updated, err)
	}
	other := notebook.Book{Root: b.Root, Scope: b.Scope}
	v, err = improvementCall(t, other, map[string]any{"operation": "reuse", "request": "Quartz editing", "conditions": "A later agent editing the same wiki"})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ = json.Marshal(v)
	if !strings.Contains(string(raw), step["check"].(string)) {
		t.Fatal("a later reader must receive the revised practice", string(raw))
	}
}

func TestLearningTransportSchemasAndOrganizationMerge(t *testing.T) {
	repo := gitRepo(t)
	root := hookEnv(t, nil, repo)
	b := notebook.Book{Root: root, Scope: feedbackScope(repo)}
	observed, err := b.CaptureObserved(notebook.Observation{Kind: "test", Title: "Transport evidence", Content: "Both source conditions were checked with synthetic input.", Reference: "synthetic", Version: "1", ObservedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	s := observed.ID
	a := loopPage(t, b, s, map[string]any{"title": "Quartz checks", "body": "Check current ownership before replacement."})
	c := loopPage(t, b, s, map[string]any{"title": "Quartz evidence", "body": "Retain the original quoted source when reorganizing."})
	input := map[string]any{"operation": "organize", "organization": map[string]any{"reason": "Combine two verified conditions.", "sources": []any{map[string]any{"id": a.ID, "revision": a.Revision, "lesson": "See the shared Quartz practice."}, map[string]any{"id": c.ID, "revision": c.Revision, "lesson": "See the shared Quartz practice."}}, "targets": []any{map[string]any{"title": "Shared Quartz practice", "lesson": "Check current ownership and retain original evidence.", "applies_to": "Quartz replacement", "kind": "howto", "scene": "Quartz replacement", "body": a.Body + "\n\n" + c.Body}}, "moves": []any{map[string]any{"from": a.ID, "quote": a.Body, "to": "Shared Quartz practice"}, map[string]any{"from": c.ID, "quote": c.Body, "to": "Shared Quartz practice"}}}}
	h := newServer(serveOpts{storeDir: root, projectScope: b.Scope})
	encoded, _ := json.Marshal(input)
	got := notebookCommand(t, "organize", string(encoded))
	if len(got["patches"].([]any)) != 2 {
		t.Fatal("CLI must return both guarded source edits", got)
	}
	code, raw, err := callTool(h, "shikii_wiki", encoded)
	if err != nil || code != 200 || !strings.Contains(string(raw), `"requires"`) {
		t.Fatal("MCP must expose the same guarded plan", code, string(raw), err)
	}
	r := httptest.NewRequest("POST", "/v1/wiki/notebook", strings.NewReader(string(encoded)))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	var httpPlan map[string]any
	decodeErr := json.Unmarshal(w.Body.Bytes(), &httpPlan)
	if w.Code != 200 || decodeErr != nil || httpPlan["creates"].([]any)[0].(map[string]any)["draft"].(map[string]any)["create_only"] != true {
		t.Fatal("HTTP must preserve create-only publication", w.Code, w.Body.String())
	}
	for _, tool := range mcpTools {
		if tool["name"] != "shikii_wiki" {
			continue
		}
		properties := tool["inputSchema"].(map[string]any)["properties"].(map[string]any)
		for _, field := range []string{"learning", "organization", "create_only"} {
			if properties[field] == nil {
				t.Fatal("MCP must advertise usable learning fields", field)
			}
		}
	}
	notes := workSchema["properties"].(map[string]any)["notes"].(map[string]any)["items"].(map[string]any)["properties"].(map[string]any)
	if notes["learning"] == nil {
		t.Fatal("work notes must advertise learning")
	}
	patch := patchSchema["properties"].(map[string]any)
	if patch["requires"] == nil || patch["learning"] == nil || patch["related"] == nil {
		t.Fatal("the returned patches must be callable through MCP", patch)
	}
	t.Run("escaped publication", func(t *testing.T) {
		quote := strings.Repeat("<", 11000)
		p := loopPage(t, b, s, map[string]any{"title": "Long quoted source", "body": quote})
		payload := map[string]any{"operation": "organize", "organization": map[string]any{"reason": "Preserve a large quoted source.", "sources": []any{map[string]any{"id": p.ID, "revision": p.Revision, "lesson": "Follow the related quoted source."}}, "targets": []any{map[string]any{"title": "Long quoted destination", "lesson": "Retain the original source text.", "applies_to": "Reviewing source text", "kind": "history", "scene": "Reviewing sources", "body": quote}}, "moves": []any{map[string]any{"from": p.ID, "quote": quote, "to": "Long quoted destination"}}}}
		value, err := improvementCall(t, b, payload)
		if err != nil {
			t.Fatal(err)
		}
		plan := learningValue(t, value)
		create, _ := json.Marshal(plan["creates"].([]any)[0].(map[string]any)["draft"])
		if len(create) <= 64<<10 {
			t.Fatal("the control must exceed the old encoded JSON limit")
		}
		saved := notebookCommand(t, "ingest", string(create))
		if saved["page"] == nil {
			t.Fatal("the returned organization create must fit the CLI", saved)
		}
		edit, _ := json.Marshal(plan["patches"].([]any)[0])
		if result := notebookCommand(t, "patch", string(edit)); result["page"] == nil {
			t.Fatal("the source patch must also fit the CLI", result)
		}
	})
}

func TestLearningEvidenceAndConditionBoundaries(t *testing.T) {
	b, s := loopBook(t)
	a := loopPage(t, b, s, map[string]any{"title": "First experience", "body": "Keep the existing source."})
	c := loopPage(t, b, s, map[string]any{"title": "Second experience", "body": "Check the active owner."})
	for _, mode := range []string{"missing-quote", "old-revision", "one-experience", "foreign-article", "missing-reason", "wrong-relation", "foreign-result", "truth-assessment", "practice-kind"} {
		t.Run(mode, func(t *testing.T) {
			connections := learningConnections(a, c)
			learning := map[string]any{"connections": connections}
			switch mode {
			case "missing-quote":
				connections[0].(map[string]any)["quote"] = "A quotation absent from the source."
			case "old-revision":
				connections[0].(map[string]any)["revision"] = "ffffffffffffffff"
			case "one-experience":
				learning["connections"] = connections[:1]
			case "foreign-article":
				connections[0].(map[string]any)["id"] = "ffffffffffffffff"
			case "missing-reason":
				connections[0].(map[string]any)["reason"] = ""
			case "wrong-relation":
				connections[0].(map[string]any)["relation"] = "proven"
			case "foreign-result", "truth-assessment":
				assessment, source := "supports", "ffffffffffffffff"
				if mode == "truth-assessment" {
					assessment, source = "true", s
				}
				learning = map[string]any{"probe": map[string]any{"question": "Does the check help?", "method": "Compare a synthetic edit", "reconsider_when": "The old content is lost", "results": []any{map[string]any{"conditions": "Synthetic input", "conclusion": "Retained", "assessment": assessment, "sources": []string{source}}}}}
			case "practice-kind":
				learning = map[string]any{"practice": map[string]any{"steps": []any{map[string]any{"condition": "Shared edit", "action": "Read", "avoid": "Overwrite", "check": "Compare", "sources": []string{s}}}}}
			}
			_, err := improvementCall(t, b, map[string]any{"operation": "ingest", "sources": []string{s}, "title": "Rejected " + mode, "lesson": "A provisional insight", "applies_to": "Shared work", "kind": "fact", "scene": "Shared work", "learning": learning})
			if err == nil {
				t.Fatal("unsupported learning must be refused", mode)
			}
		})
	}
	p := loopPage(t, b, s, map[string]any{"title": "Pinned synthesis", "learning": map[string]any{"connections": learningConnections(a, c)}})
	loopPage(t, b, s, map[string]any{"id": a.ID, "title": a.Title, "body": "The current owner changed."})
	view, err := improvementCall(t, b, map[string]any{"operation": "consult", "request": "Pinned synthesis", "proposal": "Apply the earlier experience"})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(view)
	if !strings.Contains(string(raw), "connected experience is changed") {
		t.Fatal("a changed connected premise must reach the next reader", string(raw))
	}
	var patch notebook.Patch
	encoded, _ := json.Marshal(map[string]any{"id": p.ID, "revision": p.Revision, "sources": []string{s}, "change_reason": "Revise prose without losing the historical connection.", "edits": []any{map[string]any{"field": "lesson", "before": p.Lesson, "after": "Retain the conditional earlier insight."}}})
	if err := json.Unmarshal(encoded, &patch); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Patch(patch); err != nil {
		t.Fatal("an unrelated edit must preserve historical evidence", err)
	}
}

func TestLearningOrganizeGuardsDestinationAndPreservesRoles(t *testing.T) {
	b, s := loopBook(t)
	quote := "Earlier verification retained both original sources."
	p := loopPage(t, b, s, map[string]any{"title": "Combined knowledge", "body": "Current verification uses an exact revision.\n\n" + quote, "passages": []any{map[string]any{"role": "counterexample", "quote": quote, "conditions": "Earlier context", "reason": "A different condition must stay visible"}}})
	payload := map[string]any{"operation": "organize", "organization": map[string]any{"reason": "Preserve the counterexample in its own article.", "sources": []any{map[string]any{"id": p.ID, "revision": p.Revision, "lesson": "Current verification and related counterexamples."}}, "targets": []any{map[string]any{"title": "Separated counterexample", "lesson": "An earlier counterexample", "applies_to": "Comparing earlier work", "body": quote, "kind": "fact", "scene": "Comparing work"}}, "moves": []any{map[string]any{"from": p.ID, "quote": quote, "to": "Separated counterexample"}}}}
	v, err := improvementCall(t, b, payload)
	if err != nil {
		t.Fatal(err)
	}
	plan := learningValue(t, v)
	create := plan["creates"].([]any)[0].(map[string]any)
	if create["page"].(map[string]any)["passages"].([]any)[0].(map[string]any)["role"] != "counterexample" {
		t.Fatal("organization must keep the interpreted role", plan)
	}
	var draft notebook.Draft
	raw, _ := json.Marshal(create["draft"])
	if err := json.Unmarshal(raw, &draft); err != nil {
		t.Fatal(err)
	}
	created, err := b.Ingest(draft)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.Ingest(draft); err == nil {
		t.Fatal("a repeated create must not overwrite a concurrent article")
	}
	loopPage(t, b, s, map[string]any{"id": created.Page.ID, "title": created.Page.Title, "body": "A later agent changed the destination.", "passages": []any{}})
	var patch notebook.Patch
	raw, _ = json.Marshal(plan["patches"].([]any)[0])
	if err := json.Unmarshal(raw, &patch); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Patch(patch); err == nil {
		t.Fatal("a changed destination must prevent source text removal")
	}
	original, _ := b.Query("lesson:" + p.ID)
	if original.Lessons[0].Revision != p.Revision {
		t.Fatal("a refused move must preserve the source revision", original)
	}
	org := payload["organization"].(map[string]any)
	org["moves"].([]any)[0].(map[string]any)["quote"] = "retained both"
	org["targets"].([]any)[0].(map[string]any)["title"] = "Partial role"
	org["moves"].([]any)[0].(map[string]any)["to"] = "Partial role"
	if _, err := improvementCall(t, b, payload); err == nil {
		t.Fatal("moving only part of a classified quote must be refused")
	}
	indented := "    Indented source text must keep its leading spaces."
	spaceSource := loopPage(t, b, s, map[string]any{"title": "Indented original", "body": "Current context.\n\n" + indented})
	spacePlan := map[string]any{"operation": "organize", "organization": map[string]any{"reason": "Check the actual normalized destination.", "sources": []any{map[string]any{"id": spaceSource.ID, "revision": spaceSource.Revision, "lesson": "Read the related indented source."}}, "targets": []any{map[string]any{"title": "Indented destination", "lesson": "Preserve indentation.", "applies_to": "Reading indented text", "kind": "history", "scene": "Reading sources", "body": indented}}, "moves": []any{map[string]any{"from": spaceSource.ID, "quote": indented, "to": "Indented destination"}}}}
	if _, err := improvementCall(t, b, spacePlan); err == nil {
		t.Fatal("normalization must not alter the exact moved quotation")
	}
}

func TestLearningOrganizePreviewsBeforeMovingKnowledge(t *testing.T) {
	b, s := loopBook(t)
	quote := "Historical check: preserve both source quotations."
	p := loopPage(t, b, s, map[string]any{"title": "Quartz combined procedure", "body": "Current check: verify the current revision.\n\n" + quote, "passages": []any{map[string]any{"role": "history", "quote": quote, "conditions": "Earlier workflow", "reason": "Retained as the earlier experience"}}})
	input := map[string]any{"operation": "organize", "organization": map[string]any{"reason": "Separate the earlier experience from the current procedure.", "sources": []any{map[string]any{"id": p.ID, "revision": p.Revision, "lesson": "Use the current check and follow related articles for earlier experience."}}, "targets": []any{map[string]any{"title": "Quartz earlier experience", "lesson": "Earlier source preservation checks.", "applies_to": "Reviewing earlier Quartz work", "kind": "history", "scene": "Reviewing earlier work", "body": quote}}, "moves": []any{map[string]any{"from": p.ID, "quote": quote, "to": "Quartz earlier experience"}}}}
	v, err := improvementCall(t, b, input)
	if err != nil {
		t.Fatal("organize must preview a source-preserving multi-article plan", err)
	}
	plan := learningValue(t, v)
	if len(plan["creates"].([]any)) != 1 || len(plan["patches"].([]any)) != 1 {
		t.Fatal("the plan needs destination creation before source editing", plan)
	}
	before, _ := b.Query("lesson:" + p.ID)
	if before.Lessons[0].Revision != p.Revision {
		t.Fatal("planning must not publish the organization")
	}
	var patch notebook.Patch
	raw, _ := json.Marshal(plan["patches"].([]any)[0])
	if err := json.Unmarshal(raw, &patch); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Patch(patch); err == nil {
		t.Fatal("moving text before the destination is published must be refused")
	}
	for _, c := range plan["creates"].([]any) {
		var create notebook.Draft
		raw, _ := json.Marshal(c.(map[string]any)["draft"])
		if err := json.Unmarshal(raw, &create); err != nil {
			t.Fatal(err)
		}
		if _, err := b.Ingest(create); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := b.Patch(patch); err != nil {
		t.Fatal("the guarded move must work after the destination is saved", err)
	}
	after, _ := b.Query("lesson:" + p.ID)
	if strings.Contains(after.Lessons[0].Body, quote) || len(after.Lessons[0].Related) != 1 {
		t.Fatal("the source must point to its new destination", after)
	}
}
