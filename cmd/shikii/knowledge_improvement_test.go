package main

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/notebook"
	"github.com/hayaenfj/shikii/internal/store"
)

func improvementCall(t *testing.T, b notebook.Book, fields map[string]any) (any, error) {
	t.Helper()
	raw, err := json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	var req notebookRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		t.Fatal(err)
	}
	return runNotebook(b.Root, b.Scope, req)
}

func TestKnowledgePatchPreservesOtherWork(t *testing.T) {
	b, s := loopBook(t)
	p := loopPage(t, b, s, map[string]any{"body": "## First\nKeep this.\n\n## Second\nOld condition.", "rationale": "A supported reason."})
	patch := map[string]any{"id": p.ID, "revision": p.Revision, "sources": []string{s}, "change_reason": "Correct the second condition.", "edits": []any{map[string]any{"field": "body", "before": "Old condition.", "after": "New condition."}}}
	v, err := improvementCall(t, b, map[string]any{"operation": "patch", "patch": patch})
	if err != nil {
		t.Fatal("partial editing must be available", err)
	}
	raw, _ := json.Marshal(v)
	var got notebook.Result
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got.Page == nil || got.Page.Body != "## First\nKeep this.\n\n## Second\nNew condition." || got.Page.Rationale != p.Rationale || !reflect.DeepEqual(got.Page.Sources, p.Sources) || got.Page.Previous != p.Revision {
		t.Fatalf("lost content or evidence: %+v", got.Page)
	}
	if _, err := improvementCall(t, b, map[string]any{"operation": "patch", "patch": patch}); !errors.Is(err, notebook.ErrArticleConflict) {
		t.Fatal("stale patch must not overwrite a later edit", err)
	}
	patch["revision"] = got.Page.Revision
	patch["edits"] = []any{map[string]any{"field": "body", "before": "condition.", "after": "changed"}, map[string]any{"field": "title", "before": "absent", "after": "bad"}}
	if _, err := improvementCall(t, b, map[string]any{"operation": "patch", "patch": patch}); err == nil {
		t.Fatal("a failing edit must reject the entire patch")
	}
	current, err := b.Query("lesson:" + p.ID)
	if err != nil || current.Lessons[0].Revision != got.Page.Revision {
		t.Fatal("a rejected patch changed the article", current, err)
	}
}

func TestKnowledgeDiagnosisKeepsHistoricalReasons(t *testing.T) {
	b, s := loopBook(t)
	p := loopPage(t, b, s, nil)
	session := &hookSession{TurnID: "0000000000000123"}
	if _, err := memoryContext(b.Root, b.Scope, "workflow design", session); err != nil {
		t.Fatal(err)
	}
	session.TurnID = "0000000000000124"
	if _, err := memoryContext(b.Root, b.Scope, "workflow design", session); err != nil {
		t.Fatal(err)
	}
	v, err := improvementCall(t, b, map[string]any{"operation": "diagnose", "id": p.ID})
	if err != nil {
		t.Fatal("diagnosis must explain saved attempts", err)
	}
	raw, _ := json.Marshal(v)
	var report struct {
		Cases []struct {
			Reason string `json:"reason"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(raw, &report); err != nil {
		t.Fatal(err)
	}
	if len(report.Cases) != 2 || report.Cases[0].Reason != "recently_delivered" || report.Cases[1].Reason != "delivered" {
		t.Fatalf("missing actual delivery/cache reasons: %s", raw)
	}
	loopPage(t, b, s, map[string]any{"id": p.ID, "applies_to": "a completely different topic"})
	after, err := improvementCall(t, b, map[string]any{"operation": "diagnose", "id": p.ID})
	if err != nil || !reflect.DeepEqual(loopObject(t, v), loopObject(t, after)) {
		t.Fatal("current edits must not rewrite past reasons", after, err)
	}
}

func TestKnowledgeCompareUsesFrozenCasesWithoutSaving(t *testing.T) {
	b, s := loopBook(t)
	p := loopPage(t, b, s, nil)
	session := &hookSession{TurnID: "0000000000000130"}
	if _, err := memoryContext(b.Root, b.Scope, "quartz zephyr deployment", session); err != nil {
		t.Fatal(err)
	}
	patch := map[string]any{"id": p.ID, "revision": p.Revision, "sources": []string{s}, "change_reason": "Name the actual deployment context.", "edits": []any{map[string]any{"field": "applies_to", "before": p.AppliesTo, "after": "quartz zephyr deployment"}}}
	v, err := improvementCall(t, b, map[string]any{"operation": "compare", "patch": patch})
	if err != nil {
		t.Fatal("compare must preview the same edit against recorded contexts", err)
	}
	raw, _ := json.Marshal(v)
	var report struct {
		Cases []struct {
			Before string `json:"before"`
			After  string `json:"after"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(raw, &report); err != nil {
		t.Fatal(err)
	}
	if len(report.Cases) != 1 || report.Cases[0].Before != "no_match" || report.Cases[0].After != "candidate" {
		t.Fatalf("expected a newly retrievable candidate: %s", raw)
	}
	current, err := b.Query("lesson:" + p.ID)
	if err != nil || current.Lessons[0].Revision != p.Revision {
		t.Fatal("compare saved its draft", current, err)
	}
}

func TestKnowledgeDiagnosticsFailurePreservesDelivery(t *testing.T) {
	b, s := loopBook(t)
	p := loopPage(t, b, s, nil)
	if _, err := memoryContext(b.Root, b.Scope, "workflow design", &hookSession{TurnID: "0000000000000140"}); err != nil {
		t.Fatal(err)
	}
	paths, err := filepath.Glob(filepath.Join(b.Root, "records", "wiki-deliveries", "*", ".deliveries.json"))
	if err != nil || len(paths) != 1 {
		t.Fatal(paths, err)
	}
	if err := os.WriteFile(paths[0], []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	text, err := memoryContext(b.Root, b.Scope, "workflow design", &hookSession{TurnID: "0000000000000141"})
	if err != nil || !strings.Contains(text, p.ID) {
		t.Fatal("diagnostic failure must not remove a valid delivery", text, err)
	}
	if _, err := diagnoseKnowledge(b, p.ID, ""); err == nil {
		t.Fatal("unreadable diagnostics must be explicit")
	}
}

func TestKnowledgeCompareFreezesOtherArticlesAndWritesNothing(t *testing.T) {
	b, s := loopBook(t)
	p := loopPage(t, b, s, nil)
	if _, err := memoryContext(b.Root, b.Scope, "quartz zephyr deployment", &hookSession{TurnID: "0000000000000150"}); err != nil {
		t.Fatal(err)
	}
	patch := notebook.Patch{ID: p.ID, Revision: p.Revision, Sources: []string{s}, ChangeReason: "Name the deployment.", Edits: []notebook.TextEdit{{Field: "applies_to", Before: p.AppliesTo, After: "quartz zephyr deployment"}}}
	first, err := compareKnowledge(b, patch, nil)
	if err != nil {
		t.Fatal(err)
	}
	// Later articles must not affect the earlier vocabulary frequencies or ranks.
	for i := 0; i < 8; i++ {
		loopPage(t, b, s, map[string]any{"title": fmt.Sprintf("Quartz zephyr deployment %d", i), "applies_to": "quartz zephyr deployment"})
	}
	saved := knowledgeTree(t, b.Root)
	second, err := compareKnowledge(b, patch, nil)
	if err != nil || !reflect.DeepEqual(loopObject(t, first), loopObject(t, second)) {
		t.Fatal("later knowledge leaked into a past comparison", first, second, err)
	}
	if !reflect.DeepEqual(saved, knowledgeTree(t, b.Root)) {
		t.Fatal("comparison wrote project records")
	}
	cases, err := b.DeliveryCases()
	if err != nil {
		t.Fatal(err)
	}
	other, source := loopBook(t)
	otherPage := loopPage(t, other, source, nil)
	otherPatch := patch
	otherPatch.ID, otherPatch.Revision, otherPatch.Sources = otherPage.ID, otherPage.Revision, []string{source}
	if _, err := compareKnowledge(other, otherPatch, []string{cases[0].ID}); err == nil {
		t.Fatal("a case crossed projects")
	}
}

func knowledgeTree(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		hash := sha256.Sum256(raw)
		out[path] = fmt.Sprintf("%x", hash)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestKnowledgeToolDiagnosisAndComparison(t *testing.T) {
	st, scope, _, ids := toolLessonFixture(t)
	b := notebook.Book{Root: st.Dir, Scope: scope}
	toolHook(t, scope, "diagnostic", toolPrompt, "Bash", map[string]any{"command": "git commit -m example"})
	toolHook(t, scope, "diagnostic", "", "Bash", map[string]any{"command": "git commit -m example"})
	id := strings.Split(ids["commit"], ":")[1]
	v, err := diagnoseKnowledge(b, id, "")
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(v)
	var out struct {
		Cases []diagnosisCase `json:"cases"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Cases) < 2 || out.Cases[0].Mode != "tool" || out.Cases[0].Reason != "recently_delivered" || out.Cases[1].Reason != "delivered" {
		t.Fatal("tool reasons lost", string(raw))
	}
	p, err := b.Query("lesson:" + id)
	if err != nil {
		t.Fatal(err)
	}
	page := p.Lessons[0]
	patch := notebook.Patch{ID: page.ID, Revision: page.Revision, Sources: []string{page.Sources[0].ID}, ChangeReason: "Name a different command.", Edits: []notebook.TextEdit{{Field: "lesson", Before: page.Lesson, After: "Use go test before a change."}}}
	compared, err := compareKnowledge(b, patch, []string{out.Cases[0].CaseID})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ = json.Marshal(compared)
	if !strings.Contains(string(raw), `"after":"no_match"`) {
		t.Fatal("tool comparison did not use the recorded target", string(raw))
	}
}

func TestKnowledgeOperationsThroughCLIAndMCP(t *testing.T) {
	repo := gitRepo(t)
	root := hookEnv(t, nil, repo)
	t.Chdir(repo)
	b := notebook.Book{Root: root, Scope: feedbackScope(repo)}
	source, err := b.Capture(notebook.Observation{Kind: "test", Title: "Synthetic source", Content: "A checked update.", Reference: "synthetic", Version: "1", ObservedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	p := loopPage(t, b, source.ID, nil)
	patch := notebook.Patch{ID: p.ID, Revision: p.Revision, Sources: []string{source.ID}, ChangeReason: "Precise update.", Edits: []notebook.TextEdit{{Field: "lesson", Before: p.Lesson, After: "Keep the checked workflow compact."}}}
	encoded, _ := json.Marshal(patch)
	got := notebookCommand(t, "patch", string(encoded))
	if got["page"] == nil {
		t.Fatal(got)
	}
	notebookCommand(t, "diagnose", `{}`)
	// MCP uses the same handler, including strict nested fields and conflict status.
	h := newServer(serveOpts{storeDir: root, projectScope: b.Scope})
	body, _ := json.Marshal(map[string]any{"operation": "patch", "patch": patch})
	code, _, err := callTool(h, "shikii_wiki", body)
	if err != nil || code != 409 {
		t.Fatal("MCP must preserve revision conflicts", code, err)
	}
	code, _, err = callTool(h, "shikii_wiki", json.RawMessage(`{"operation":"diagnose"}`))
	if err != nil || code != 200 {
		t.Fatal(code, err)
	}
	code, _, err = callTool(h, "shikii_wiki", json.RawMessage(`{"operation":"patch","patch":{"status":"confirmed"}}`))
	if err != nil || code != 400 {
		t.Fatal("nested unknown fields accepted", code, err)
	}

	// A failure after publishing the article returns its saved revision and warning.
	current := got["page"].(map[string]any)
	patch.Revision = current["revision"].(string)
	patch.Edits = []notebook.TextEdit{{Field: "body", Before: "", After: "Checked body."}}
	indexes, err := filepath.Glob(filepath.Join(root, "wiki", "lessons", "*", "index.md"))
	if err != nil || len(indexes) != 1 {
		t.Fatal(indexes, err)
	}
	if err := os.Remove(indexes[0]); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(indexes[0], 0700); err != nil {
		t.Fatal(err)
	}
	body, _ = json.Marshal(map[string]any{"operation": "patch", "patch": patch})
	code, response, err := callTool(h, "shikii_wiki", body)
	if err != nil || code != 200 || !strings.Contains(string(response), `"warnings"`) || !strings.Contains(string(response), `"Checked body."`) {
		t.Fatal("transport hid a saved article or recovery warning", code, string(response), err)
	}
}

func TestKnowledgeToolDeliveryFailureRemainsVisible(t *testing.T) {
	st, scope, _, ids := toolLessonFixture(t)
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "failure", "cwd": scope, "prompt": toolPrompt})
	snapshot := loadSession(st.Dir, "failure")
	b := notebook.Book{Root: st.Dir, Scope: scope}
	page, err := b.Query("lesson:" + strings.Split(ids["commit"], ":")[1])
	if err != nil {
		t.Fatal(err)
	}
	q := lessonQuote(page.Lessons[0])
	label := "Bash git commit"
	hash := sha256.Sum256([]byte(snapshot.TurnID + "/tool/" + label + "/" + quoteCacheKey(q)))
	id := fmt.Sprintf("%x", hash)[:16]
	if err := st.SaveMemoryDelivery(store.MemoryDelivery{ID: id, Scope: scope, Request: "A different synthetic request.", Quotes: []store.MemoryQuote{q}, Parent: snapshot.TurnID, Tool: label, Phrase: "git commit"}); err != nil {
		t.Fatal(err)
	}
	out, err := hookToolLesson(st.Dir, &hookInput{SessionID: "failure", CWD: scope, ToolName: "Bash", ToolInput: json.RawMessage(`{"command":"git commit -m example"}`)}, snapshot)
	if err == nil || out.Context != "" {
		t.Fatal("a conflicting delivery save was hidden", out, err)
	}
	cases, err := b.DeliveryCases()
	if err != nil || len(cases) == 0 || cases[len(cases)-1].Failure != "delivery_failed" {
		t.Fatal("the diagnostic omitted delivery failure", cases, err)
	}
}
