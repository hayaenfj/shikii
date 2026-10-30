package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/notebook"
	"github.com/hayaenfj/shikii/internal/store"
)

func loopBook(t *testing.T) (notebook.Book, string) {
	t.Helper()
	repo := t.TempDir()
	b := notebook.Book{Root: filepath.Join(repo, ".shikii"), Scope: repo}
	s, err := b.Capture(notebook.Observation{Kind: "test", Title: "Synthetic evidence", Content: "Choose a compact workflow; extra forms cost time.", Reference: "synthetic", Version: "1", ObservedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	return b, s.ID
}

func loopPage(t *testing.T, b notebook.Book, source string, fields map[string]any) notebook.Page {
	t.Helper()
	d := map[string]any{"sources": []string{source}, "title": "Compact workflow", "lesson": "Keep the workflow compact.", "applies_to": "workflow design", "kind": "criteria", "scene": "designing"}
	for k, v := range fields {
		d[k] = v
	}
	raw, _ := json.Marshal(d)
	var draft notebook.Draft
	if err := json.Unmarshal(raw, &draft); err != nil {
		t.Fatal(err)
	}
	r, err := b.Ingest(draft)
	if err != nil {
		t.Fatal(err)
	}
	return *r.Page
}

func loopObject(t *testing.T, v any) map[string]any {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err = json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestManualDiscoveryFeedsRecall(t *testing.T) {
	b, s := loopBook(t)
	p := loopPage(t, b, s, nil)
	const context = "quartz zephyr deployment"
	before, err := b.Recall(context)
	if err != nil || len(before) != 0 {
		t.Fatal(before, err)
	}
	v, err := runNotebook(b.Root, b.Scope, notebookRequest{Operation: "query", Query: "lesson:" + p.ID})
	if err != nil {
		t.Fatal(err)
	}
	lookup, ok := loopObject(t, v)["lookup"].(map[string]any)
	if !ok {
		t.Fatal("a manual lookup needs a receipt for the articles actually returned")
	}
	readOnly, _ := b.Recall(context)
	if len(readOnly) != 0 {
		t.Fatal("reading is not an evaluation")
	}
	body, _ := json.Marshal(map[string]any{"operation": "submit", "discoveries": []any{map[string]any{"lookup_id": lookup["id"], "page_id": p.ID, "revision": p.Revision, "context": context, "decision": "Used the compact workflow instead of adding forms.", "evidence": "The resulting proposal needs no new input fields."}}})
	req, err := decodeReflect(strings.NewReader(string(body)))
	if err != nil {
		t.Fatal(err)
	}
	result, err := runReflect(b.Root, b.Scope, req)
	if err != nil || loopObject(t, result)["complete"] != true {
		t.Fatal(result, err)
	}
	result, err = runReflect(b.Root, b.Scope, req)
	if err != nil || loopObject(t, result)["complete"] != true {
		t.Fatal("a discovery retry must be idempotent", result, err)
	}
	bad := req
	bad.Discoveries = append([]notebook.Discovery{}, req.Discoveries...)
	bad.Discoveries[0].LookupID = "ffffffffffffffff"
	result, err = runReflect(b.Root, b.Scope, bad)
	if err != nil || loopObject(t, result)["complete"] != false {
		t.Fatal("an invented lookup cannot teach retrieval", result, err)
	}
	other := notebook.Book{Root: t.TempDir(), Scope: t.TempDir()}
	result, err = runReflect(other.Root, other.Scope, req)
	if err != nil || loopObject(t, result)["complete"] != false {
		t.Fatal("a lookup cannot cross projects", result, err)
	}
	report, err := (store.Store{Dir: b.Root}).MemoryReport(b.Scope)
	if err != nil || report.Stats.Helpful != 0 {
		t.Fatal("manual discoveries are separate from delivery evaluations", report, err)
	}
	after, err := b.Recall(context)
	if err != nil || len(after) != 1 || after[0].ID != p.ID {
		t.Fatal("an evidenced discovery must reach the next related request", after, err)
	}
	if lessonQuote(after[0]).DiscoveredFor == "" {
		t.Fatal("deliver the context that made the manual discovery useful")
	}
	// A new revision is not endorsed by the previous revision's discovery.
	loopPage(t, b, s, map[string]any{"id": p.ID, "lesson": "A different workflow."})
	after, err = b.Recall(context)
	if err != nil || len(after) != 0 {
		t.Fatal("do not carry usefulness across revisions", after, err)
	}
	result, err = runReflect(b.Root, b.Scope, req)
	if err != nil || loopObject(t, result)["complete"] != false {
		t.Fatal("an old revision cannot be endorsed again", result, err)
	}
}

func TestWikiRationaleReachesMemory(t *testing.T) {
	b, s := loopBook(t)
	const why = "For small teams, choose local records. Reject a separate approval form because it adds work. Outcome: no extra form."
	p := loopPage(t, b, s, map[string]any{"rationale": why})
	if loopObject(t, p)["rationale"] != why {
		t.Fatal("retain source-backed choice and rejected alternative")
	}
	q := lessonQuote(p)
	if loopObject(t, q)["rationale"] != why {
		t.Fatal("deliver the reasoning, not only the rule")
	}
	sources := map[string]string{}
	addPlanningLesson(sources, p)
	if !strings.Contains(sources["lesson:"+p.ID+":"+p.Revision], why) {
		t.Fatal("planning must carry the choice and reasons")
	}
	p = loopPage(t, b, s, map[string]any{"id": p.ID, "lesson": "Keep records compact."})
	if loopObject(t, p)["rationale"] != why {
		t.Fatal("ordinary edits must preserve the rationale")
	}
	empty := ""
	p, err := b.Manage(notebook.Change{ID: p.ID, Revision: p.Revision, Action: "edit", Title: p.Title, Lesson: p.Lesson, AppliesTo: p.AppliesTo, Kind: p.Kind, Scene: p.Scene, Rationale: &empty})
	if err != nil || p.Rationale != "" {
		t.Fatal("the person can clear an unsupported rationale", p, err)
	}
}

func TestPremiseChangeReopensReview(t *testing.T) {
	b, s := loopBook(t)
	path := filepath.Join(b.Scope, "behavior.txt")
	if err := os.WriteFile(path, []byte("before"), 0600); err != nil {
		t.Fatal(err)
	}
	p := loopPage(t, b, s, map[string]any{"watch": []string{"behavior.txt"}})
	tasks, err := b.Maintenance()
	if err != nil || len(tasks) != 1 {
		t.Fatal(tasks, err)
	}
	old := tasks[0]
	_, err = b.Maintain(notebook.MaintenanceReview{PageID: p.ID, Revision: p.Revision, Key: old.Key, Reason: "Checked against the file."})
	if err != nil {
		t.Fatal(err)
	}
	beforeRecall, _ := b.Recall("workflow design")
	oldCache := memoryCacheKey(beforeRecall[0])
	if err := os.WriteFile(path, []byte("after"), 0600); err != nil {
		t.Fatal(err)
	}
	tasks, err = b.Maintenance()
	if err != nil || len(tasks) != 1 {
		t.Fatal("a changed premise must reopen review", tasks, err)
	}
	if tasks[0].Key == old.Key || !strings.Contains(strings.Join(tasks[0].Reasons, ","), "premise-changed") {
		t.Fatal(tasks)
	}
	if _, err = b.Maintain(notebook.MaintenanceReview{PageID: p.ID, Revision: p.Revision, Key: old.Key, Reason: "Stale check"}); err == nil {
		t.Fatal("reject a check from before the file changed")
	}
	v, err := b.Recall("workflow design")
	if err != nil || len(v) != 1 {
		t.Fatal(v, err)
	}
	if loopObject(t, lessonQuote(v[0]))["recheck"] == nil {
		t.Fatal("the next agent must see the changed premise")
	}
	if memoryCacheKey(v[0]) == oldCache {
		t.Fatal("a changed premise must not be hidden by the one-hour delivery cache")
	}
	if err = os.Remove(path); err != nil {
		t.Fatal(err)
	}
	v, _ = b.Recall("workflow design")
	if v[0].Recheck[0].State != "missing" {
		t.Fatal(v)
	}
	outside := filepath.Join(t.TempDir(), "outside.txt")
	if err = os.WriteFile(outside, []byte("synthetic"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.Symlink(outside, path); err != nil {
		t.Fatal(err)
	}
	v, _ = b.Recall("workflow design")
	if v[0].Recheck[0].State != "unreadable" || v[0].Recheck[0].Current != "" {
		t.Fatal("never hash a symlink target", v)
	}
	for _, watch := range [][]string{{"../outside.txt"}, {outside}, {"behavior.txt"}, {".private"}, {"a", "b", "c", "d", "e"}} {
		d := notebook.Draft{ID: p.ID, Title: p.Title, Lesson: p.Lesson, AppliesTo: p.AppliesTo, Sources: []string{s}, Watch: &watch}
		if _, err = b.Ingest(d); err == nil {
			t.Fatal("unsafe watch accepted", watch)
		}
	}
}

func TestPlanningBundlesReflection(t *testing.T) {
	repo, root, _ := planningFixture(t)
	st := store.Store{Dir: root}
	const source = "0000000000000999"
	if err := st.AppendFeedback(feedbackScope(repo), store.Feedback{ID: source, Request: "Thanks", Summary: "Acknowledged", Reply: "Continue"}); err != nil {
		t.Fatal(err)
	}
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": planningTestRequest})
	turn := loadSession(root, "planning").TurnID
	v, err := planningCall(t, "query", map[string]any{"session_id": "planning", "turn_id": turn, "query": "workflow"})
	if err != nil {
		t.Fatal(err)
	}
	if v["reflection"] == nil {
		t.Fatal("planning must return pending reflection in the same read")
	}
	payload := map[string]any{"session_id": "planning", "turn_id": turn, "query": "workflow", "agreed": map[string]bool{"unknown": true}, "open": map[string]bool{"unknown": true}, "reflection": map[string]any{"wiki": []any{map[string]any{"sources": []string{source}}}}}
	v, err = planningCall(t, "submit", payload)
	if err != nil {
		t.Fatal(err)
	}
	r, ok := v["reflection"].(map[string]any)
	if !ok || r["complete"] != true {
		t.Fatal(v)
	}
	// A failed item is visible and can be repaired without changing the planning receipt.
	payload["reflection"] = map[string]any{"wiki": []any{map[string]any{"sources": []string{"ffffffffffffffff"}}}}
	v, err = planningCall(t, "submit", payload)
	if err != nil || v["reflection"].(map[string]any)["complete"] != false {
		t.Fatal("partial failure must be visible", v, err)
	}
	payload["reflection"] = map[string]any{"wiki": []any{map[string]any{"sources": []string{source}}}}
	v, err = planningCall(t, "submit", payload)
	if err != nil || v["reflection"].(map[string]any)["complete"] != true {
		t.Fatal("retry the failed reflection without replanning", v, err)
	}
	view, err := reflectQuery(root, feedbackScope(repo))
	if err != nil {
		t.Fatal(err)
	}
	for _, pending := range view.Sources {
		if pending.Source.ID == source {
			t.Fatal("the combined save did not settle the source")
		}
	}
}
