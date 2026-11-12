package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/notebook"
	"github.com/hayaenfj/shikii/internal/store"
)

func TestReasoningRequiredCorrectionSurvivesUnrelatedPatchAndRecovers(t *testing.T) {
	_, root, b, p := reasoningFixture(t)
	source, err := b.CaptureObserved(notebook.Observation{Kind: "test", Title: "Synthetic original policies", Content: "Two independent synthetic policies.", Reference: "synthetic", Version: "1", ObservedAt: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	add := func(title, body string) notebook.Page {
		t.Helper()
		result, err := b.Ingest(notebook.Draft{Title: title, Lesson: title, Body: &body, Kind: "howto", Scene: "synthetic policy checks", AppliesTo: title, Sources: []string{source.ID}})
		if err != nil {
			t.Fatal(err)
		}
		return *result.Page
	}
	a, c := add("Required policy A", "Original A."), add("Independent policy B", "Original B.")
	read := func(prep map[string]any, id string) string {
		t.Helper()
		v, err := workCall(t, b, "prepare", map[string]any{"receipt": prep["receipt"], "query": "lesson:" + id})
		if err != nil {
			t.Fatal(err)
		}
		for _, raw := range v["knowledge"].([]any) {
			item := loopObject(t, raw)
			if item["id"] == id {
				return item["ref"].(string)
			}
		}
		t.Fatal("missing prepared article")
		return ""
	}
	ar, br := read(p, a.ID), read(p, c.ID)
	saveReasoning(t, b, p["receipt"], reasoningInput("start", "current"), nil)
	if _, err = b.Patch(notebook.Patch{ID: a.ID, Revision: a.Revision, Sources: []string{source.ID}, ChangeReason: "A concurrent synthetic correction.", Edits: []notebook.TextEdit{{Field: "body", Before: "Original A.", After: "Concurrent A."}}}); err != nil {
		t.Fatal(err)
	}
	note := func(target, before, after string) map[string]any {
		return map[string]any{"target": target, "sources": []string{"current"}, "edits": []any{map[string]any{"field": "body", "before": before, "after": after}}}
	}
	failed, err := workCall(t, b, "record", map[string]any{"receipt": p["receipt"], "notes": []any{note(ar, "Original A.", "Corrected A.")}})
	if err != nil || failed["complete"] != false {
		t.Fatal(failed, err)
	}
	requiredRef := loopObject(t, failed["errors"].([]any)[0])["ref"]
	targets := loopObject(t, loopObject(t, failed["reasoning"])["pending_note_targets"])
	if targets[requiredRef.(string)] != a.ID {
		t.Fatal("the original required article must remain discoverable for recovery")
	}
	unrelated, err := workCall(t, b, "record", map[string]any{"receipt": p["receipt"], "notes": []any{note(br, "Original B.", "Corrected B.")}, "reasoning": reasoningInput("finish", "current")})
	if err != nil || loopObject(t, unrelated["reasoning"])["wiki_pending"] != true {
		t.Fatal("unrelated successful patch cleared a failed correction", unrelated, err)
	}
	next, err := workCall(t, b, "prepare", map[string]any{"session_id": "reasoning", "turn_id": loadSession(root, "reasoning").TurnID, "query": "Required policy A"})
	if err != nil {
		t.Fatal(err)
	}
	fixed := note(read(next, a.ID), "Concurrent A.", "Corrected A.")
	fixed["ref"] = requiredRef
	final := saveReasoning(t, b, next["receipt"], reasoningInput("finish", "current"), map[string]any{"notes": []any{fixed}})
	if loopObject(t, final["reasoning"])["complete"] != true {
		t.Fatal(final)
	}
	pages, err := b.LiveWhere(func(page notebook.Page) bool { return page.ID == a.ID })
	if err != nil || len(pages) != 1 || pages[0].Body != "Corrected A." {
		t.Fatal("the original correction was not published", err)
	}
}

func TestReasoningSessionStoresOneCanonicalProgress(t *testing.T) {
	_, root, b, p := reasoningFixture(t)
	saveReasoning(t, b, p["receipt"], reasoningInput("start", "current"), nil)
	raw, err := os.ReadFile(sessionPath(root, "reasoning"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Count(raw, []byte(`"reasoning":`)) != 1 {
		t.Fatal("session must store one canonical reasoning progress")
	}
	s := loadSession(root, "reasoning")
	if s.Planning.Work.Reasoning == nil || workHash(s.Planning.Work.Reasoning) != workHash(s.Reasoning) {
		t.Fatal("loading must restore the work copy of reasoning progress")
	}
}

func TestReasoningCaptureRecoveryKeepsSourceAcrossPreparation(t *testing.T) {
	_, root, b, p := reasoningFixture(t)
	saveReasoning(t, b, p["receipt"], reasoningInput("start", "current"), nil)
	original := workAfterWrite
	t.Cleanup(func() { workAfterWrite = original })
	workAfterWrite = func(kind string) error {
		if kind == "observation" {
			return errors.New("interrupted synthetic capture")
		}
		return nil
	}
	o := map[string]any{"kind": "test", "title": "Interrupted capture", "content": "A checked condition must remain recoverable."}
	failed, err := workCall(t, b, "record", map[string]any{"receipt": p["receipt"], "observations": []any{o}})
	workAfterWrite = original
	if err != nil || failed["complete"] != false {
		t.Fatal(failed, err)
	}
	id := loopObject(t, failed["errors"].([]any)[0])["id"]
	next, err := workCall(t, b, "prepare", map[string]any{"session_id": "reasoning", "turn_id": loadSession(root, "reasoning").TurnID, "query": "CSV"})
	if err != nil {
		t.Fatal(err)
	}
	end := reasoningInput("finish", "current")
	end["result"] = "The unresolved capture still needs recovery."
	blocked, err := workCall(t, b, "record", map[string]any{"receipt": next["receipt"], "reasoning": end})
	if err != nil || blocked["complete"] != false {
		t.Fatal("preparation erased the capture obligation", blocked, err)
	}
	recovered, err := workCall(t, b, "record", map[string]any{"receipt": next["receipt"], "observations": []any{o}})
	if err != nil || recovered["complete"] != true {
		t.Fatal(recovered, err)
	}
	item := loopObject(t, recovered["saved"].([]any)[0])
	if item["id"] != id {
		t.Fatal("recovery recaptured an already durable source", item, id)
	}
	end = reasoningInput("finish", item["ref"].(string))
	end["result"] = "The interrupted source was reread and saved with its original identity."
	final := saveReasoning(t, b, next["receipt"], end, nil)
	if loopObject(t, final["reasoning"])["complete"] != true {
		t.Fatal(final)
	}
}

func auditALifecycleFixture(t *testing.T, withModel, modern bool) (string, string, notebook.Book, map[string]any) {
	t.Helper()
	repo := gitRepo(t)
	var root string
	if withModel {
		root = hookEnv(t, fakeAgentModel(t, func(string) float64 { return .9 }), repo)
	} else {
		root = hookEnv(t, nil, repo)
	}
	if modern {
		usePlanningSpec(t, root)
	}
	runUnreviewedHook(t, "user-prompt-submit", map[string]any{"session_id": "audit-a", "cwd": repo, "prompt": "\u5408\u6210\u306e\u624b\u9806\u3092\u78ba\u8a8d\u3057\u3066\u304f\u3060\u3055\u3044\u3002"})
	b := notebook.Book{Root: root, Scope: feedbackScope(repo)}
	p, e := workCall(t, b, "prepare", map[string]any{"session_id": "audit-a", "turn_id": loadSession(root, "audit-a").TurnID, "query": "\u5408\u6210 \u624b\u9806"})
	if e != nil {
		t.Fatal("\u6e96\u5099\u5931\u6557", e)
	}
	return repo, root, b, p
}

func auditAInput(phase string) map[string]any {
	r := map[string]any{"phase": phase, "focus": "\u5408\u6210\u6761\u4ef6\u3092\u4fdd\u3066\u308b\u304b\u3002", "assessment": "retain", "reason": "\u4f9d\u983c\u306e\u6761\u4ef6\u3092\u78ba\u8a8d\u3057\u305f\u3002", "evidence": []string{"current"}, "next_action": "\u6b8b\u308b\u6761\u4ef6\u3092\u78ba\u8a8d\u3059\u308b\u3002"}
	if phase == "finish" {
		r["result"] = "\u5408\u6210\u6761\u4ef6\u3092\u78ba\u8a8d\u3057\u305f\u3002"
	}
	return r
}

func auditARecord(t *testing.T, b notebook.Book, p map[string]any, phase string) map[string]any {
	t.Helper()
	v, e := workCall(t, b, "record", map[string]any{"receipt": p["receipt"], "reasoning": auditAInput(phase)})
	if e != nil || v["complete"] != true {
		t.Fatal(v, e)
	}
	return v
}

func auditAStatus(t *testing.T, v map[string]any) map[string]any {
	t.Helper()
	return loopObject(t, v["reasoning"])
}

func TestAuditReasoningAWikiFailedCorrectionSurvivesUnrelatedNote(t *testing.T) {
	repo, root, b, p := auditALifecycleFixture(t, true, true)
	auditARecord(t, b, p, "start")
	failed, e := workCall(t, b, "record", map[string]any{"receipt": p["receipt"], "notes": []any{map[string]any{"title": "\u5fc5\u8981\u306a\u8a02\u6b63", "lesson": "\u5408\u6210\u6761\u4ef6\u3092\u8a02\u6b63\u3059\u308b\u3002", "kind": "howto", "scene": "\u5408\u6210\u78ba\u8a8d", "applies_to": "\u5408\u6210\u6761\u4ef6", "sources": []string{"missing"}}}})
	if e != nil || failed["complete"] != false || auditAStatus(t, failed)["wiki_pending"] != true {
		t.Fatal("\u6e96\u5099\u72b6\u614b\u3092\u4f5c\u308c\u306a\u3044", failed, e)
	}
	unrelated, e := workCall(t, b, "record", map[string]any{"receipt": p["receipt"], "notes": []any{map[string]any{"target": "new", "title": "\u7121\u95a2\u4fc2\u306a\u88dc\u8db3", "lesson": "\u5408\u6210\u306e\u5225\u6761\u4ef6\u3092\u8a18\u9332\u3059\u308b\u3002", "kind": "fact", "scene": "\u5225\u6761\u4ef6", "applies_to": "\u5225\u306e\u5408\u6210\u6761\u4ef6", "sources": []string{"current"}}}, "reasoning": auditAInput("finish")})
	if e != nil {
		t.Fatal("\u5b9f\u884c\u5931\u6557", e)
	}
	pages, e := b.Live()
	if e != nil {
		t.Fatal(e)
	}
	names := []string{}
	for _, p := range pages {
		names = append(names, p.Title)
	}
	out := runUnreviewedHook(t, "stop", map[string]any{"session_id": "audit-a", "cwd": repo, "last_assistant_message": "\u5408\u6210\u78ba\u8a8d\u3092\u7d42\u4e86\u3057\u307e\u3059\u3002"})
	recs, e := (store.Store{Dir: root}).Records("done-as-asked", 0)
	if e != nil {
		t.Fatal(e)
	}
	t.Logf("\u7121\u95a2\u4fc2\u306a\u4fdd\u5b58\u5f8c complete=%v wiki_pending=%v reasoning_complete=%v \u4fdd\u5b58\u8a18\u4e8b=%v Stop=%q judged=%v \u7d42\u4e86\u5224\u5b9a\u4ef6\u6570=%d", unrelated["complete"], auditAStatus(t, unrelated)["wiki_pending"], auditAStatus(t, unrelated)["complete"], names, out, loadSession(root, "audit-a").Judged, len(recs))
	if auditAStatus(t, unrelated)["wiki_pending"] != true {
		t.Fatal("RH-A01: \u5225\u8a18\u4e8b\u306e\u4fdd\u5b58\u304c\u672a\u4fdd\u5b58\u306e\u8a02\u6b63\u3092\u89e3\u6d88\u3057\u305f")
	}
}

func TestAuditReasoningAFailedObservation(t *testing.T) {
	repo, root, b, p := auditALifecycleFixture(t, true, true)
	auditARecord(t, b, p, "start")
	auditARecord(t, b, p, "finish")
	old := workAfterWrite
	t.Cleanup(func() { workAfterWrite = old })
	workAfterWrite = func(kind string) error {
		if kind == "observation" {
			return errors.New("\u5408\u6210\u306e\u4fdd\u5b58\u76f4\u5f8c\u306e\u4e2d\u65ad")
		}
		return nil
	}
	failed, e := workCall(t, b, "record", map[string]any{"receipt": p["receipt"], "observations": []any{map[string]any{"kind": "test", "title": "\u4fdd\u5b58\u304c\u4e2d\u65ad\u3057\u305f\u5408\u6210\u7d50\u679c", "content": "\u672a\u78ba\u8a8d\u306e\u5225\u6761\u4ef6\u3002"}}})
	workAfterWrite = old
	if e != nil || failed["complete"] != false {
		t.Fatal("\u4e2d\u65ad\u3092\u518d\u73fe\u3067\u304d\u306a\u3044", failed, e)
	}
	newer := auditAInput("finish")
	newer["result"] = "\u5225\u306e\u6761\u4ef6\u306e\u4fdd\u5b58\u72b6\u614b\u3092\u78ba\u8a8d\u3057\u3066\u3044\u306a\u3044\u3002"
	v, e := workCall(t, b, "record", map[string]any{"receipt": p["receipt"], "reasoning": newer})
	if e != nil {
		t.Fatal(e)
	}
	incomplete := 0
	for _, capture := range loadSession(root, "audit-a").Planning.Work.Captures {
		if capture.ID == "" {
			incomplete++
		}
	}
	out := runUnreviewedHook(t, "stop", map[string]any{"session_id": "audit-a", "cwd": repo, "last_assistant_message": "\u5408\u6210\u78ba\u8a8d\u3092\u7d42\u4e86\u3057\u307e\u3059\u3002"})
	returnedID := false
	returnedSource := ""
	for _, raw := range failed["errors"].([]any) {
		item := loopObject(t, raw)
		if item["kind"] == "observation" && item["id"] != "" && item["id"] != nil {
			returnedID = true
			returnedSource, _ = item["id"].(string)
		}
	}
	actualSource := false
	if returnedSource != "" {
		durable, readErr := b.ReadSources([]string{returnedSource})
		if readErr != nil {
			t.Fatal("\u539f\u8cc7\u6599\u306e\u8aad\u8fbc", readErr)
		}
		raw, _ := json.Marshal(durable)
		actualSource = strings.Contains(string(raw), "\u4fdd\u5b58\u304c\u4e2d\u65ad\u3057\u305f\u5408\u6210\u7d50\u679c")
	}
	if !actualSource {
		t.Fatal("\u4fdd\u5b58\u76f4\u5f8c\u306e\u5931\u6557\u3092\u4f5c\u308c\u306a\u3044")
	}
	t.Logf("\u4fdd\u5b58\u5931\u6557\u5fdc\u7b54\u3067\u539f\u8cc7\u6599ID\u306e\u8fd4\u5374=%v \u5b9f\u539f\u8cc7\u6599\u306e\u518d\u8aad\u8fbc=%v", returnedID, actualSource)
	t.Logf("\u89b3\u5bdf\u4fdd\u5b58\u4e2d\u65ad\u5f8c\u306e\u65b0finish complete=%v reasoning_complete=%v ID\u672a\u4fdd\u5b58\u306ecapture\u6570=%d Stop=%q judged=%v", v["complete"], auditAStatus(t, v)["complete"], incomplete, out, loadSession(root, "audit-a").Judged)
	if incomplete > 0 && auditAStatus(t, v)["complete"] == true {
		t.Fatal("RH-A03: \u672a\u5b8c\u4e86\u306e\u89b3\u5bdf\u4fdd\u5b58\u3092\u6b8b\u3057\u3066\u7d42\u4e86\u3057\u305f")
	}
}

func TestAuditReasoningAObservationNeverSaved(t *testing.T) {
	repo, root, b, p := auditALifecycleFixture(t, true, true)
	auditARecord(t, b, p, "start")
	auditARecord(t, b, p, "finish")
	sourceDir := filepath.Join(root, "records", "wiki-sources", store.StoreName(root, b.Scope))
	queue := filepath.Join(sourceDir, ".captured.json")
	original, e := os.ReadFile(queue)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(queue, []byte("{"), 0600); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = os.WriteFile(queue, original, 0600) })
	failed, e := workCall(t, b, "record", map[string]any{"receipt": p["receipt"], "observations": []any{map[string]any{"kind": "test", "title": "\u672a\u4fdd\u5b58\u306e\u5408\u6210\u7d50\u679c", "content": "\u4fdd\u5b58\u3057\u3066\u304a\u304f\u5fc5\u8981\u306e\u3042\u308b\u5408\u6210\u7d50\u679c\u3002"}}})
	if restore := os.WriteFile(queue, original, 0600); restore != nil {
		t.Fatal(restore)
	}
	if e != nil || failed["complete"] != false {
		t.Fatal("\u4fdd\u5b58\u5931\u6557\u3092\u518d\u73fe\u3067\u304d\u306a\u3044", failed, e)
	}
	newer := auditAInput("finish")
	newer["result"] = "\u4f9d\u983c\u3060\u3051\u3092\u53c2\u7167\u3057\u305f\u65b0\u3057\u3044\u7d50\u679c\u3002"
	v, e := workCall(t, b, "record", map[string]any{"receipt": p["receipt"], "reasoning": newer})
	if e != nil {
		t.Fatal(e)
	}
	incomplete := 0
	for _, capture := range loadSession(root, "audit-a").Planning.Work.Captures {
		if capture.ID == "" {
			incomplete++
		}
	}
	entries, e := os.ReadDir(sourceDir)
	if e != nil {
		t.Fatal(e)
	}
	found := false
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".json") && entry.Name() != ".captured.json" {
			raw, e := os.ReadFile(filepath.Join(sourceDir, entry.Name()))
			if e != nil {
				t.Fatal(e)
			}
			if strings.Contains(string(raw), "\u672a\u4fdd\u5b58\u306e\u5408\u6210\u7d50\u679c") {
				found = true
			}
		}
	}
	out := runUnreviewedHook(t, "stop", map[string]any{"session_id": "audit-a", "cwd": repo, "last_assistant_message": "\u5408\u6210\u78ba\u8a8d\u3092\u7d42\u4e86\u3057\u307e\u3059\u3002"})
	recs, e := (store.Store{Dir: root}).Records("done-as-asked", 0)
	if e != nil {
		t.Fatal(e)
	}
	returnedID := false
	for _, raw := range failed["errors"].([]any) {
		item := loopObject(t, raw)
		if item["kind"] == "observation" && item["id"] != "" && item["id"] != nil {
			returnedID = true
		}
	}
	t.Logf("\u4fdd\u5b58\u5931\u6557\u5fdc\u7b54\u3067\u539f\u8cc7\u6599ID\u306e\u8fd4\u5374=%v", returnedID)
	t.Logf("\u89b3\u5bdf\u306e\u5b9f\u539f\u8cc7\u6599\u304c\u4fdd\u5b58\u6e08\u307f=%v ID\u672a\u4fdd\u5b58\u306ecapture\u6570=%d reasoning_complete=%v Stop=%q judged=%v \u7d42\u4e86\u5224\u5b9a\u4ef6\u6570=%d", found, incomplete, auditAStatus(t, v)["complete"], out, loadSession(root, "audit-a").Judged, len(recs))
	if !found && incomplete > 0 && auditAStatus(t, v)["complete"] == true {
		t.Fatal("RH-A03: \u5b9f\u539f\u8cc7\u6599\u3092\u4fdd\u5b58\u3067\u304d\u306a\u3044\u89b3\u5bdf\u3092\u6b8b\u3057\u3066\u7d42\u4e86\u3057\u305f")
	}
}
