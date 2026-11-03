package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/hayaenfj/shikii/internal/notebook"
	"github.com/hayaenfj/shikii/internal/store"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestCollectiveAuditCollectivePremiseInspectionUsesSuppliedState(t *testing.T) {
	b, source := loopBook(t)
	path := filepath.Join(b.Scope, "behavior.txt")
	if err := os.WriteFile(path, []byte("before"), 0600); err != nil {
		t.Fatal(err)
	}
	p := loopPage(t, b, source, map[string]any{"watch": []string{"behavior.txt"}})
	first, err := workCall(t, b, "prepare", map[string]any{"request": "Check workflow design", "query": "lesson:" + p.ID})
	if err != nil {
		t.Fatal(err)
	}
	token, _ := workDecode(first["receipt"].(string))
	dir, err := openWorkSessions(b.Root)
	if err != nil {
		t.Fatal(err)
	}
	defer dir.Close()
	w, name, err := loadWorkState(dir, b.Scope, token)
	if err != nil {
		t.Fatal(err)
	}
	supplied := w.Collective.Baseline[p.ID]
	if err = os.WriteFile(path, []byte("after"), 0600); err != nil {
		t.Fatal(err)
	}
	recorded, err := workCall(t, b, "record", map[string]any{"receipt": first["receipt"], "reviewed": []string{"knowledge1"}})
	if err != nil || recorded["complete"] != true {
		t.Fatal(recorded, err)
	}
	s, err := readPlanningSession(dir, name)
	if err != nil {
		t.Fatal(err)
	}
	confirmed := s.KnowledgeSeen[0]
	t.Logf("supplied premise=%s; confirmed premise=%s; same article revision=%v", supplied.Premise, confirmed.Premise, supplied.Revision == confirmed.Revision)
	next, err := b.Collective([]notebook.CollectiveBaseline{confirmed}, []notebook.ArticleReference{{ID: p.ID, Revision: p.Revision}})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("next-turn update count=%d", len(next.Updates))
	if confirmed.Premise != supplied.Premise || len(next.Updates) != 1 {
		t.Fatal("inspection acknowledged a premise state never supplied to the agent")
	}
}

func TestCollectiveAuditCollectiveReportContrastCannotReferenceAbsentConnections(t *testing.T) {
	b, source := loopBook(t)
	probe := map[string]any{"question": "Does Quartz work?", "method": "Check a synthetic input.", "reconsider_when": "The input changes."}
	p := loopPage(t, b, source, map[string]any{"title": "Quartz workflow", "kind": "howto", "learning": map[string]any{"probe": probe}})
	for i := 0; i < 2; i++ {
		prepared, err := workCall(t, b, "prepare", map[string]any{"query": "lesson:" + p.ID, "request": "Check Quartz"})
		if err != nil {
			t.Fatal(err)
		}
		condition := strings.Repeat("c", 980) + fmt.Sprint(i)
		result := strings.Repeat("r", 980) + fmt.Sprint(i)
		recorded, err := workCall(t, b, "record", map[string]any{"receipt": prepared["receipt"], "observations": []any{map[string]any{"kind": "test", "title": fmt.Sprint(i), "content": "Synthetic input was checked."}}, "outcomes": []any{map[string]any{"ref": "knowledge1", "conditions": condition, "action": strings.Repeat("a", 980), "result": result, "sources": []string{"new1"}}}})
		if err != nil || recorded["complete"] != true {
			t.Fatal(recorded, err)
		}
	}
	view, err := b.Collective(nil, []notebook.ArticleReference{{ID: p.ID, Revision: p.Revision}})
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Contrasts) != 1 || !view.Contrasts[0].Omitted {
		t.Fatal("fixture must omit the large report contrast", view)
	}
	view.ReuseLearning(map[string]string{p.ID + "@" + p.Revision: "knowledge1.learning_context.learning"})
	c := view.Contrasts[0]
	t.Logf("contrast omitted=%v reports=%d sides=%d evidence=%q retained report queries=%d", c.Omitted, len(c.Reports), len(c.Sides), c.Evidence, len(c.ReportQueries))
	if !c.Omitted || strings.HasSuffix(c.Evidence, ".connections") {
		t.Fatal("an outcome contrast points to absent connections while claiming no omission")
	}
}

func TestCollectiveAuditCollectiveExplicitRetiredArticleKeepsSessionBaseline(t *testing.T) {
	b, source := loopBook(t)
	p := loopPage(t, b, source, map[string]any{"title": "Quartz invoice procedure"})
	base := b.CollectiveBaseline(notebook.ArticleReference{ID: p.ID, Revision: p.Revision})
	if _, err := b.Manage(notebook.Change{ID: p.ID, Revision: p.Revision, Action: "delete"}); err != nil {
		t.Fatal(err)
	}
	w := &workState{Query: "lesson:" + p.ID, Articles: map[string]store.KnowledgeReference{}}
	view, err := preparedCollective(b, w, []notebook.CollectiveBaseline{base}, []any{})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("explicit query for the known deleted article: updates=%d known baseline retained=%d", len(view.Updates), len(w.Collective.Baseline))
	if len(view.Updates) != 1 || view.Updates[0].Status != "unavailable" {
		t.Fatal("explicitly requested known article disappeared from cross-turn change delivery")
	}
}

func TestCollectiveAuditDocsExtendedConsultEvidenceOnce(t *testing.T) {
	b, source := loopBook(t)
	loopPage(t, b, source, map[string]any{"title": "Quartz deployment", "lesson": "Check Quartz deployment conditions.", "applies_to": "Quartz deployment", "learning": map[string]any{"probe": map[string]any{"question": "QUARTZ-PROBE-AUDIT-SYNTHETIC", "method": "Check a synthetic deployment.", "reconsider_when": "The conditions change."}}})
	first, e := workCall(t, b, "prepare", map[string]any{"request": "Check Quartz deployment", "query": "unrelated"})
	if e != nil {
		t.Fatal(e)
	}
	next, e := workCall(t, b, "prepare", map[string]any{"receipt": first["receipt"], "proposal": "Check Quartz deployment"})
	if e != nil {
		t.Fatal(e)
	}
	raw, _ := json.Marshal(next)
	n := strings.Count(string(raw), "QUARTZ-PROBE-AUDIT-SYNTHETIC")
	t.Logf("question occurrences: %d", n)
	if n != 1 {
		t.Fatalf("the same probe is supplied %d times", n)
	}
}

func TestCollectiveAuditReconcileKeepsPeerRolesWhenEditorRolesWereUntouched(t *testing.T) {
	b, source := loopBook(t)
	base := loopPage(t, b, source, map[string]any{"body": "First paragraph.\n\nLast paragraph."})
	current := loopPage(t, b, source, map[string]any{"id": base.ID, "passages": []notebook.KnowledgePassage{{Role: "current", Quote: "First paragraph.", Conditions: "Synthetic check", Reason: "Peer classified the existing text."}}})
	empty := []notebook.KnowledgePassage{}
	plan, err := b.ReconcileChange(notebook.Change{ID: base.ID, Revision: base.Revision, Action: "reconcile", Title: base.Title, Lesson: base.Lesson, AppliesTo: base.AppliesTo, Kind: base.Kind, Scene: base.Scene, Body: "First paragraph.\n\nMy last paragraph.", Passages: &empty})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("status=%s conflicts=%v current=%s", plan.Status, plan.Conflicts, current.Revision)
	if plan.Status != "ready" || plan.Result == nil || len(plan.Result.Passages) != 1 || plan.Result.Passages[0].Quote != "First paragraph." {
		t.Fatal("untouched editor roles must preserve the peer's addition without a conflict")
	}
}

func TestCollectiveAuditReconcileRejectsNonregularHistoricalRevision(t *testing.T) {
	b, source := loopBook(t)
	base := loopPage(t, b, source, map[string]any{"body": "First paragraph.\n\nLast paragraph."})
	current := loopPage(t, b, source, map[string]any{"id": base.ID, "body": "Peer first paragraph.\n\nLast paragraph."})
	paths, err := filepath.Glob(filepath.Join(b.Root, "wiki", "lessons", "*", "revisions", base.ID+"-"+base.Revision+".md"))
	if err != nil || len(paths) != 1 {
		t.Fatal(paths, err)
	}
	data, err := os.ReadFile(paths[0])
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(paths[0]); err != nil {
		t.Fatal(err)
	}
	if err = syscall.Mkfifo(paths[0], 0600); err != nil {
		t.Fatal(err)
	}
	writer := make(chan error, 1)
	go func() {
		f, e := os.OpenFile(paths[0], os.O_WRONLY, 0600)
		if e != nil {
			writer <- e
			return
		}
		_, e = f.Write(data)
		f.Close()
		writer <- e
	}()
	plan, err := b.Reconcile(notebook.Patch{ID: base.ID, Revision: base.Revision, Sources: []string{source}, ChangeReason: "Synthetic FIFO boundary check", Edits: []notebook.TextEdit{{Field: "body", Before: "Last paragraph.", After: "My last paragraph."}}})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("status=%s current=%s base_file_is_fifo=true", plan.Status, current.Revision)
	cleanup, cleanupErr := os.OpenFile(paths[0], os.O_RDWR|syscall.O_NONBLOCK, 0600)
	if cleanupErr != nil {
		t.Fatal(cleanupErr)
	}
	defer cleanup.Close()
	if e := <-writer; e != nil && !errors.Is(e, syscall.EPIPE) {
		t.Fatal(e)
	}
	if plan.Status != "unavailable" || plan.Patch != nil {
		t.Fatal("a nonregular historical revision must be refused before reading")
	}
}

func TestCollectiveAuditHelpMakesMergeDiscoverable(t *testing.T) {
	if !strings.Contains(usageText(), "reconcile") {
		t.Fatal("the common CLI help must expose the available merge operation")
	}
}
