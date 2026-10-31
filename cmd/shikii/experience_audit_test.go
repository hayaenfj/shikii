package main

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/notebook"
)

func TestExperienceSameMissedEvidenceDifferentLookup(t *testing.T) {
	b, s := loopBook(t)
	p := loopPage(t, b, s, nil)
	_, c, err := b.RecallDetailed("quartz zephyr deployment")
	if err != nil {
		t.Fatal(err)
	}
	c.TurnID = "0000000000000180"
	if err = b.SaveDeliveryCase(c); err != nil {
		t.Fatal(err)
	}
	cases, err := b.DeliveryCases()
	if err != nil {
		t.Fatal(err)
	}
	save := func(query string) {
		v, err := improvementCall(t, b, map[string]any{"operation": "query", "query": query})
		if err != nil {
			t.Fatal(err)
		}
		lookup := v.(notebook.View).Lookup
		if lookup == nil {
			t.Fatal("the article was not returned")
		}
		if err = b.Discover(notebook.Discovery{LookupID: lookup.ID, PageID: p.ID, Revision: p.Revision, CaseID: cases[0].ID, Context: c.Request, Decision: "Used the existing rule", Evidence: "The checked action needs no extra form"}); err != nil {
			t.Fatal(err)
		}
	}
	save("lesson:" + p.ID)
	tasks, err := b.Maintenance()
	if err != nil || len(tasks) != 1 {
		t.Fatal(tasks, err)
	}
	if _, err = b.Maintain(notebook.MaintenanceReview{PageID: p.ID, Revision: p.Revision, Key: tasks[0].Key, Reason: "Checked the same action"}); err != nil {
		t.Fatal(err)
	}
	save("compact workflow")
	tasks, err = b.Maintenance()
	if err != nil || len(tasks) != 0 {
		t.Fatal("changing only the manual query must not reopen reviewed evidence", tasks, err)
	}
}

func TestExperienceLongModelContextDoesNotHideConditions(t *testing.T) {
	var calls atomic.Int64
	model := fakeAgentModel(t, func(state string) float64 { calls.Add(1); return .8 })
	repo := gitRepo(t)
	root := hookEnv(t, model, repo)
	b := notebook.Book{Root: root, Scope: feedbackScope(repo)}
	src, err := b.Capture(notebook.Observation{Kind: "test", Title: "Invoice condition", Content: "Keep ownership checks", Reference: "synthetic", Version: "1", ObservedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	loopPage(t, b, src.ID, map[string]any{"title": "Invoice routing", "applies_to": "invoice routing"})
	before := calls.Load()
	v, err := improvementCall(t, b, map[string]any{"operation": "consult", "request": "invoice routing", "proposal": strings.Repeat("Long approach. ", 80), "conditions": "CURRENT_CONDITION_MARKER"})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(v)
	if calls.Load() != before || !strings.Contains(string(raw), `"model_notice"`) || strings.Contains(string(raw), `"applicability_trace"`) {
		t.Fatal("an incomplete model context must be explicit instead of producing a truncated judgment", string(raw))
	}
	if !strings.Contains(string(raw), "CURRENT_CONDITION_MARKER") {
		t.Fatal("the host agent must still receive the full conditions")
	}
}

func TestExperienceEscapedInputHasTransportParity(t *testing.T) {
	repo := gitRepo(t)
	root := hookEnv(t, nil, repo)
	h := newServer(serveOpts{storeDir: root, projectScope: feedbackScope(repo)})
	for _, op := range []string{"consult", "reuse"} {
		input := map[string]string{"request": strings.Repeat("<", 16000), "proposal": "synthetic plan", "conditions": strings.Repeat("<", 16000)}
		cli, _ := json.Marshal(input)
		input["operation"] = op
		body, _ := json.Marshal(input)
		r := httptest.NewRequest("POST", "/v1/wiki", strings.NewReader(string(body)))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 200 {
			t.Fatal(op, w.Code, w.Body.String())
		}
		code, _, err := callTool(h, "shikii_wiki", body)
		if err != nil || code != 200 {
			t.Fatal(op, code, err)
		}
		previous := stdin
		stdin = strings.NewReader(string(cli))
		_, err = captureErr(func() error { return cmdNotebook([]string{op}) })
		stdin = previous
		if err != nil {
			t.Fatal("valid escaped text must work through CLI too", op, err)
		}
	}
}
