package main

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/notebook"
	"github.com/hayaenfj/shikii/internal/store"
)

func TestExperienceConsultFindsReasonsAndLinkedExceptions(t *testing.T) {
	b, s := loopBook(t)
	criterion := loopPage(t, b, s, map[string]any{"title": "Minimize manual work", "applies_to": "always", "rationale": "Extra forms were rejected because people already explain their intent in conversation."})
	exception := loopPage(t, b, s, map[string]any{"title": "External publication", "kind": "rule", "applies_to": "publishing externally", "lesson": "Obtain current authorization before publishing."})
	p := loopPage(t, b, s, map[string]any{"title": "Invoice routing", "applies_to": "invoice routing", "body": "Reuse verified routing rules; do not add a form.", "related": []string{criterion.ID, exception.ID}})
	v, err := improvementCall(t, b, map[string]any{"operation": "consult", "request": "invoice routing", "proposal": "Add an invoice routing form"})
	if err != nil {
		t.Fatal("consult must check the proposed approach against project evidence", err)
	}
	raw, _ := json.Marshal(v)
	for _, want := range []string{p.ID, criterion.ID, exception.ID, criterion.Rationale, exception.Lesson, "sources", "next_action"} {
		if !strings.Contains(string(raw), want) {
			t.Fatalf("missing reasoning or exception %q: %s", want, raw)
		}
	}
	if _, err := improvementCall(t, b, map[string]any{"operation": "consult", "request": "invoice routing"}); err == nil {
		t.Fatal("an absent proposal is not an approach to check")
	}
	if _, err := improvementCall(t, b, map[string]any{"operation": "consult", "request": strings.Repeat("x", 17000), "proposal": "x"}); err == nil {
		t.Fatal("bound consultation input before retrieval or model calls")
	}
}

func TestExperienceTransportAndSchemas(t *testing.T) {
	repo := gitRepo(t)
	root := hookEnv(t, nil, repo)
	b := notebook.Book{Root: root, Scope: feedbackScope(repo)}
	s, err := b.Capture(notebook.Observation{Kind: "test", Title: "Procedure check", Content: "Synthetic invoice checks passed.", Reference: "synthetic", Version: "1", ObservedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	p := loopPage(t, b, s.ID, map[string]any{"title": "Invoice routing", "applies_to": "invoice routing", "kind": "howto", "body": "Verify totals, then route."})
	h := newServer(serveOpts{storeDir: root, projectScope: b.Scope})
	for _, op := range []string{"consult", "reuse"} {
		input := `{"request":"invoice routing","proposal":"Verify first","conditions":"tenant B"}`
		got := notebookCommand(t, op, input)
		if got["next_action"] == nil {
			t.Fatal(got)
		}
		payload := `{"operation":"` + op + `","request":"invoice routing","proposal":"Verify first","conditions":"tenant B"}`
		code, raw, err := callTool(h, "shikii_wiki", json.RawMessage(payload))
		if err != nil || code != 200 || !strings.Contains(string(raw), p.ID) {
			t.Fatal(code, string(raw), err)
		}
		r := httptest.NewRequest("POST", "/v1/wiki", strings.NewReader(payload))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 200 || !strings.Contains(w.Body.String(), p.ID) {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	for _, tool := range mcpTools {
		if tool["name"] != "shikii_wiki" {
			continue
		}
		raw, _ := json.Marshal(tool)
		for _, text := range []string{`"consult"`, `"reuse"`, `"request"`, `"proposal"`, `"conditions"`} {
			if !strings.Contains(string(raw), text) {
				t.Fatal("MCP must advertise usable operations and their inputs", text)
			}
		}
	}
	raw, _ := json.Marshal(discoverySchema)
	if !strings.Contains(string(raw), `"case_id"`) {
		t.Fatal("the discovery schema must offer diagnostic linkage")
	}
}

func TestExperienceConsultUsesProjectModelWithoutTruth(t *testing.T) {
	model := fakeAgentModel(t, func(state string) float64 { return .8 })
	repo := gitRepo(t)
	root := hookEnv(t, model, repo)
	b := notebook.Book{Root: root, Scope: feedbackScope(repo)}
	s, err := b.Capture(notebook.Observation{Kind: "test", Title: "Policy source", Content: "No extra forms.", Reference: "synthetic", Version: "1", ObservedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	loopPage(t, b, s.ID, map[string]any{"applies_to": "invoice routing", "title": "Invoice routing policy"})
	v, err := improvementCall(t, b, map[string]any{"operation": "consult", "request": "invoice routing", "proposal": "Add a form"})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(v)
	if !strings.Contains(string(raw), "applicability_trace") {
		t.Fatal("consultation must use the project's existing model", string(raw))
	}
	labels, err := (store.Store{Dir: root}).LabeledTraces(lessonAppliesSpec)
	if err != nil || len(labels) != 0 {
		t.Fatal("an applicability prediction is not a truth", labels, err)
	}
}

func TestExperienceMissedEvidenceBoundaries(t *testing.T) {
	for _, mode := range []string{"incomplete", "foreign", "old-revision", "delivered", "recently_delivered", "missing"} {
		t.Run(mode, func(t *testing.T) {
			b, s := loopBook(t)
			p := loopPage(t, b, s, nil)
			tasks, _ := b.Maintenance()
			if _, err := b.Maintain(notebook.MaintenanceReview{PageID: p.ID, Revision: p.Revision, Key: tasks[0].Key, Reason: "Initial review"}); err != nil {
				t.Fatal(err)
			}
			_, c, err := b.RecallDetailed("quartz zephyr deployment")
			if err != nil {
				t.Fatal(err)
			}
			if mode == "incomplete" {
				c.Complete = false
			}
			if mode == "delivered" || mode == "recently_delivered" {
				c.Entries[0].Reason = mode
			}
			if err = b.SaveDeliveryCase(c); err != nil {
				t.Fatal(err)
			}
			cases, _ := b.DeliveryCases()
			caseID := cases[0].ID
			if mode == "foreign" {
				other, _ := loopBook(t)
				if err = other.SaveDeliveryCase(c); err != nil {
					t.Fatal(err)
				}
				foreign, _ := other.DeliveryCases()
				caseID = foreign[0].ID
			}
			if mode == "missing" {
				caseID = "ffffffffffffffff"
			}
			if mode == "old-revision" {
				p = loopPage(t, b, s, map[string]any{"id": p.ID, "lesson": "Use existing ownership rules."})
			}
			v, err := improvementCall(t, b, map[string]any{"operation": "query", "query": "lesson:" + p.ID})
			if err != nil {
				t.Fatal(err)
			}
			lookup := v.(notebook.View).Lookup
			d := notebook.Discovery{LookupID: lookup.ID, PageID: p.ID, Revision: p.Revision, Context: "quartz zephyr deployment", Decision: "Used an existing rule", Evidence: "No additional form", CaseID: caseID}
			err = b.Discover(d)
			if mode == "delivered" || mode == "recently_delivered" {
				if err != nil {
					t.Fatal(err)
				}
				tasks, err = b.Maintenance()
				if err != nil || len(tasks) != 0 {
					t.Fatal("an intentional prior delivery is not missed evidence", tasks, err)
				}
			} else if err == nil {
				t.Fatal("unsupported diagnostic linkage accepted", mode)
			}
		})
	}
}

func TestExperienceCurrentPremisesAndDeletedKnowledge(t *testing.T) {
	b, s := loopBook(t)
	path := filepath.Join(b.Scope, "policy.txt")
	if err := os.WriteFile(path, []byte("before"), 0600); err != nil {
		t.Fatal(err)
	}
	p := loopPage(t, b, s, map[string]any{"applies_to": "invoice routing", "title": "Invoice routing policy", "watch": []string{"policy.txt"}})
	if err := os.WriteFile(path, []byte("after"), 0600); err != nil {
		t.Fatal(err)
	}
	v, err := improvementCall(t, b, map[string]any{"operation": "consult", "request": "invoice routing", "proposal": "Use the earlier policy"})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(v)
	if !strings.Contains(string(raw), `"recheck"`) {
		t.Fatal("current premise changes must reach the agent", string(raw))
	}
	if _, err = b.Manage(notebook.Change{Action: "delete", ID: p.ID, Revision: p.Revision}); err != nil {
		t.Fatal(err)
	}
	v, err = improvementCall(t, b, map[string]any{"operation": "consult", "request": "invoice routing", "proposal": "Use the earlier policy"})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ = json.Marshal(v)
	if strings.Contains(string(raw), p.ID) || !strings.Contains(string(raw), "unknown") {
		t.Fatal("deleted knowledge must not supply current guidance", string(raw))
	}
}

func TestExperienceReuseMarksUnverifiedReports(t *testing.T) {
	b, _ := loopBook(t)
	s, err := b.Capture(notebook.Observation{Kind: "agent", Title: "Earlier report", Content: "Try routing after ownership checks.", Reference: "synthetic", Version: "1", ObservedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	loopPage(t, b, s.ID, map[string]any{"title": "Invoice routing procedure", "kind": "howto", "applies_to": "invoice routing", "body": "Check ownership before routing."})
	v, err := improvementCall(t, b, map[string]any{"operation": "reuse", "request": "invoice routing", "conditions": "tenant B"})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(v)
	if !strings.Contains(string(raw), `"unknown"`) || !strings.Contains(string(raw), "No test or release snapshot") {
		t.Fatal("a reported howto must not become a verified success", string(raw))
	}
}

func TestExperienceEvidenceBudgetsAndIntegrity(t *testing.T) {
	b, s := loopBook(t)
	body := strings.Repeat("Background information. ", 350) + "\n\nInvoice routing exception: verify ownership first."
	for _, name := range []string{"Invoice routing alpha", "Invoice routing beta", "Invoice routing gamma"} {
		loopPage(t, b, s, map[string]any{"title": name, "kind": "howto", "applies_to": "invoice routing", "body": body})
	}
	v, err := improvementCall(t, b, map[string]any{"operation": "reuse", "request": "invoice routing", "conditions": "tenant B"})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(v)
	if !strings.Contains(string(raw), `"body_omitted":true`) {
		t.Fatal("a bounded context must explicitly name omitted bodies", string(raw))
	}
	if !strings.Contains(string(raw), `"body_excerpts"`) {
		t.Fatal("long articles need exact useful excerpts instead of another mandatory read", string(raw))
	}
	object := loopObject(t, v)
	for _, item := range object["checks"].([]any) {
		check := item.(map[string]any)
		for _, rawExcerpt := range check["body_excerpts"].([]any) {
			e := rawExcerpt.(map[string]any)
			start, end := int(e["start"].(float64)), int(e["end"].(float64))
			if start < 0 || end > len(body) || body[start:end] != e["text"] {
				t.Fatal("excerpts must quote exact byte ranges", e)
			}
		}
	}
	if !strings.Contains(string(raw), "Invoice routing exception: verify ownership first.") {
		t.Fatal("the relevant condition must be in the focused context")
	}
	if _, err = improvementCall(t, b, map[string]any{"operation": "reuse", "request": "invoice routing"}); err == nil {
		t.Fatal("reuse needs current conditions")
	}
	paths, err := filepath.Glob(filepath.Join(b.Root, "records", "wiki-sources", "*", s+".json"))
	if err != nil || len(paths) != 1 {
		t.Fatal(paths, err)
	}
	if err = os.WriteFile(paths[0], []byte("tampered"), 0600); err != nil {
		t.Fatal(err)
	}
	v, err = improvementCall(t, b, map[string]any{"operation": "reuse", "request": "invoice routing", "conditions": "tenant B"})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ = json.Marshal(v)
	if strings.Contains(string(raw), "Check ownership") || strings.Contains(string(raw), `"steps"`) || !strings.Contains(string(raw), `"unknown"`) {
		t.Fatal("tampered evidence cannot supply a procedure", string(raw))
	}
}

func TestExperienceMissedDiscoveryReopensMaintenanceOnce(t *testing.T) {
	b, s := loopBook(t)
	p := loopPage(t, b, s, nil)
	tasks, _ := b.Maintenance()
	if len(tasks) != 1 {
		t.Fatal(tasks)
	}
	if _, err := b.Maintain(notebook.MaintenanceReview{PageID: p.ID, Revision: p.Revision, Key: tasks[0].Key, Reason: "Initial review."}); err != nil {
		t.Fatal(err)
	}
	_, c, err := b.RecallDetailed("quartz zephyr deployment")
	if err != nil {
		t.Fatal(err)
	}
	c.TurnID = "0000000000000180"
	if err := b.SaveDeliveryCase(c); err != nil {
		t.Fatal(err)
	}
	cases, _ := b.DeliveryCases()
	v, err := improvementCall(t, b, map[string]any{"operation": "query", "query": "lesson:" + p.ID})
	if err != nil {
		t.Fatal(err)
	}
	lookup := loopObject(t, v)["lookup"].(map[string]any)["id"]
	body, _ := json.Marshal(map[string]any{"discoveries": []any{map[string]any{"lookup_id": lookup, "page_id": p.ID, "revision": p.Revision, "context": c.Request, "decision": "Used the existing rule instead of adding forms.", "evidence": "The proposal has no extra form.", "case_id": cases[0].ID}}})
	var req reflectRequest
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatal(err)
	}
	req.Operation = "submit"
	result, err := runReflect(b.Root, b.Scope, req)
	if err != nil || !loopObject(t, result)["complete"].(bool) {
		t.Fatal(result, err)
	}
	tasks, err = b.Maintenance()
	raw, _ := json.Marshal(tasks)
	if err != nil || len(tasks) != 1 || !strings.Contains(string(raw), "missed-delivery") || !strings.Contains(string(raw), cases[0].ID) || !strings.Contains(string(raw), "no_match") {
		t.Fatal("an evidenced missed delivery must return with its historical reason", string(raw), err)
	}
	before := tasks[0].Key
	if _, err := b.Maintain(notebook.MaintenanceReview{PageID: p.ID, Revision: p.Revision, Key: before, Reason: "Reviewed the missed delivery and applicability."}); err != nil {
		t.Fatal(err)
	}
	if _, err = runReflect(b.Root, b.Scope, req); err != nil {
		t.Fatal(err)
	}
	tasks, err = b.Maintenance()
	if err != nil || len(tasks) != 0 {
		t.Fatal("the same evidence must not return after review or retry", tasks, err)
	}
	// The same query and revision produce the same lookup id in a later task.
	// A new recorded case must still be accepted as distinct evidence.
	c.TurnID = "0000000000000181"
	if err = b.SaveDeliveryCase(c); err != nil {
		t.Fatal(err)
	}
	newCases, err := b.DeliveryCases()
	if err != nil {
		t.Fatal(err)
	}
	d := req.Discoveries[0]
	d.CaseID, d.Evidence = newCases[len(newCases)-1].ID, "A second checked action also avoided an extra form."
	if err = b.Discover(d); err != nil {
		t.Fatal(err)
	}
	tasks, err = b.Maintenance()
	if err != nil || len(tasks) != 1 || tasks[0].Key == before {
		t.Fatal("new evidence must reopen review", tasks, err)
	}
	if _, err = b.Maintain(notebook.MaintenanceReview{PageID: p.ID, Revision: p.Revision, Key: before, Reason: "Stale review"}); err == nil {
		t.Fatal("new evidence must invalidate the earlier review key")
	}
}

func TestExperienceReuseKeepsEvidenceAndCurrentConditions(t *testing.T) {
	b, _ := loopBook(t)
	if err := os.MkdirAll(filepath.Join(b.Root, "handoff", "invoice"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(b.Root, "handoff", "invoice", "checks.md"), []byte("Checked invoice totals. Avoid routing before verifying ownership."), 0600); err != nil {
		t.Fatal(err)
	}
	s, err := b.Capture(notebook.Observation{Kind: "test", Title: "Invoice verification", Content: "Invoice checks passed in the sandbox.", Reference: ".shikii/handoff/invoice/checks.md", Version: "1", ObservedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	p := loopPage(t, b, s.ID, map[string]any{"title": "Invoice routing procedure", "kind": "howto", "applies_to": "invoice routing", "body": "Check ownership.\nVerify totals.\nRoute the invoice.", "rationale": "Routing before ownership was rejected."})
	v, err := improvementCall(t, b, map[string]any{"operation": "reuse", "request": "invoice routing", "conditions": "New tenant; the routing version is 2."})
	if err != nil {
		t.Fatal("reuse must assemble a source-backed procedure for current conditions", err)
	}
	raw, _ := json.Marshal(v)
	for _, want := range []string{p.ID, p.Revision, "Check ownership.", s.Content, "checks.md", "New tenant", "next_action"} {
		if !strings.Contains(string(raw), want) {
			t.Fatalf("lost procedure conditions or evidence %q: %s", want, raw)
		}
	}
	// Provenance accumulates; lexicographic source ids must not hide the latest
	// validation behind older reports when only three excerpts are returned.
	older := []string{}
	for _, version := range []string{"old-1", "old-2", "old-3", "old-4"} {
		o, err := b.Capture(notebook.Observation{Kind: "test", Title: "Older invoice check", Content: "Earlier routing checks passed.", Reference: "synthetic", Version: version, ObservedAt: time.Unix(1, 0)})
		if err != nil {
			t.Fatal(err)
		}
		older = append(older, o.ID)
	}
	p = loopPage(t, b, s.ID, map[string]any{"id": p.ID, "title": p.Title, "kind": "howto", "applies_to": "invoice routing", "sources": append(older, s.ID)})
	ordered := p
	ordered.Sources = nil
	for _, id := range append(older, s.ID) {
		for _, ref := range p.Sources {
			if ref.ID == id {
				ordered.Sources = append(ordered.Sources, ref)
			}
		}
	}
	evidence, _, err := b.ProcedureEvidence(ordered, "invoice routing")
	if err != nil || len(evidence) != 3 || evidence[0].SourceID != s.ID {
		t.Fatal("the latest validation must not be buried by old source order", evidence, err)
	}
	if err = os.WriteFile(filepath.Join(b.Root, "handoff", "invoice", "checks.md"), []byte(strings.Repeat("Long current record. ", 150)), 0600); err != nil {
		t.Fatal(err)
	}
	v, err = improvementCall(t, b, map[string]any{"operation": "reuse", "request": "invoice routing", "conditions": "tenant B"})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ = json.Marshal(v)
	if !strings.Contains(string(raw), `"record_cut":true`) {
		t.Fatal("a shortened current record must say it is incomplete", string(raw))
	}
	outside := filepath.Join(t.TempDir(), "outside.md")
	if err = os.WriteFile(outside, []byte("PRIVATE_OUTSIDE_MARKER"), 0600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(b.Root, "handoff", "invoice", "checks.md")
	if err = os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err = os.Symlink(outside, path); err != nil {
		t.Fatal(err)
	}
	v, err = improvementCall(t, b, map[string]any{"operation": "reuse", "request": "invoice routing", "conditions": "version 2"})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ = json.Marshal(v)
	if strings.Contains(string(raw), "PRIVATE_OUTSIDE_MARKER") || !strings.Contains(string(raw), "unavailable") {
		t.Fatal("work record links must not be followed", string(raw))
	}
}
