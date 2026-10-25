package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/store"
)

// 0.7.4: show the content of `shikii status` (the person's effort, the effect of delivered lessons, guards) on the page too. Adds summary to /v1/status.
func TestStatusEndpointCarriesTheSummaryForThePage(t *testing.T) {
	st, scope, h, _, _ := memoryEffectFixture(t)
	get := func() map[string]any {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/status", nil))
		var out map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("status JSON: %v %s", err, rec.Body.String())
		}
		s, _ := out["summary"].(map[string]any)
		if s == nil {
			t.Fatalf("status needs a summary: %s", rec.Body.String())
		}
		return s
	}
	s := get()
	mem, _ := s["memory"].(map[string]any)
	gu, _ := s["guards"].(map[string]any)
	if mem["delivered"] != 0.0 || gu["total"] != 0.0 || gu["enabled"] != 0.0 || gu["rituals"] == nil { // no person's guards; only the count of ritual checks (built in) (0.8.0)
		t.Fatalf("nothing at first (the count of ritual checks is separate): %v", s)
	}
	writeGuard(t, st.Dir, guard{ID: "changelog-pair", Kind: guardKindPairedFiles, Title: "変更履歴は英語と日本語を揃える", Pairs: [][]string{{"CHANGELOG.md", "CHANGELOG_ja.md"}}})
	memoryEffectPrompt(t, scope, "s1")
	s = get()
	mem, _ = s["memory"].(map[string]any)
	gu, _ = s["guards"].(map[string]any)
	var items []any // only guards a person made (built-in ritual checks are listed with builtin:true)
	for _, it := range gu["items"].([]any) {
		if it.(map[string]any)["builtin"] != true {
			items = append(items, it)
		}
	}
	if mem["delivered"] != 1.0 || mem["pending"] != 1.0 || gu["total"] != 1.0 || gu["enabled"] != 1.0 || len(items) != 1 || items[0].(map[string]any)["id"] != "changelog-pair" {
		t.Fatalf("delivered lessons and guards appear in summary: %v", s)
	}
	person, _ := s["person"].(map[string]any)
	if person == nil || person["records"] == nil {
		t.Fatalf("the person's effort appears too: %v", s)
	}
}

// 0.8.0: decisions a person does not answer (wiki-conflict / wiki-place) are not counted as waiting for confirmation; only the number waiting for an outcome is shown. Hook decisions keep the old behavior, since they have spot checks for the person.
func TestStatusDoesNotCountMachineOnlySpecsAsWaitingForAPerson(t *testing.T) {
	model := fakeAgentModel(t, func(string) float64 { return .1 })
	hookEnv(t, model)
	repo := gitRepo(t)
	initAt(t, repo)
	root := filepath.Join(repo, ".shikii")
	st := store.Store{Dir: root}
	for i := 0; i < 3; i++ {
		rec := store.Record{TraceID: fmt.Sprintf("wc%d", i), SpecID: "wiki-conflict", Type: "noul", Model: "m", Inputs: map[string]string{"lesson": "a", "new_lesson": "b"}, InputsHash: fmt.Sprintf("h%d", i), Raw: map[string]float64{"yes": .2, "no": .8}, Top: "no", Band: "discard", At: time.Now()}
		if err := st.AppendRecord(rec); err != nil {
			t.Fatal(err)
		}
	}
	// "Unsure" in decisions whose truth comes from an outcome waits for the outcome; "unsure" in decisions a person answers waits for the person
	st.AppendRecord(store.Record{TraceID: "pk1", SpecID: "prompt-kind", Type: "choice", Model: "m", Inputs: map[string]string{"request": "x"}, InputsHash: "pk1", Raw: map[string]float64{"edit": .5, "explain": .5}, Top: "edit", Band: "ask", At: time.Now()})
	st.AppendRecord(store.Record{TraceID: "np1", SpecID: "needs-plan", Type: "noul", Model: "m", Inputs: map[string]string{"request": "x", "agreed": "a", "open": "o"}, InputsHash: "np1", Raw: map[string]float64{"yes": .5, "no": .5}, Top: "yes", Band: "ask", At: time.Now()})
	st.AppendRecord(store.Record{TraceID: "np2", SpecID: "needs-plan", Type: "noul", Model: "m", Inputs: map[string]string{"request": "y", "agreed": "a", "open": "o"}, InputsHash: "np2", Raw: map[string]float64{"yes": .9, "no": .1}, Top: "yes", Band: "act", At: time.Now()})
	o := serveOpts{storeDir: root, specsDir: filepath.Join(root, "decisions"), projectScope: repo}
	h := newServer(o)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/status", nil))
	var body struct {
		Specs []map[string]any `json:"specs"`
		Model map[string]any   `json:"model"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("%v: %s", err, rec.Body.String()[:200])
	}
	if body.Model["name"] == nil {
		t.Fatalf("the AI's state: which model is deciding: %v", body.Model)
	}
	got := map[string]map[string]any{}
	for _, sp := range body.Specs {
		got[sp["id"].(string)] = sp
	}
	wc := got["wiki-conflict"]
	if wc == nil || wc["person_answers"] != false || wc["waiting_for_person"] != float64(0) || wc["awaiting_results"] != float64(3) || wc["unlabeled"] != float64(3) || wc["truth_source"] != "results" {
		t.Fatalf("decisions a person does not answer: 0 waiting for a person, 3 waiting for an outcome: %v", wc)
	}
	if pk := got["prompt-kind"]; pk["waiting_for_person"] != float64(0) || pk["awaiting_results"] != float64(1) {
		t.Fatalf("unsure decisions whose truth comes from an outcome wait for the outcome: %v", pk)
	}
	if np := got["needs-plan"]; np["waiting_for_person"] != float64(1) || np["person_answers"] != true || np["awaiting_results"] != float64(0) || np["settled_by_ai"] != float64(1) {
		t.Fatalf("unsure decisions a person answers wait for the person; act outside spot checks is not waiting for an outcome but decided by the AI: %v", np)
	}
	nq := httptest.NewRecorder()
	h.ServeHTTP(nq, httptest.NewRequest(http.MethodGet, "/v1/queue?spec=needs-plan", nil))
	if !strings.Contains(nq.Body.String(), `"np1"`) || !strings.Contains(nq.Body.String(), `"np2"`) {
		t.Fatalf("the queue of questions a person gives truths to also shows act (the person can correct it; told apart by waits_for_person): %s", nq.Body.String()[:300])
	}
	q := httptest.NewRecorder()
	h.ServeHTTP(q, httptest.NewRequest(http.MethodGet, "/v1/queue?spec=prompt-kind", nil))
	if !strings.Contains(strings.ReplaceAll(q.Body.String(), " ", ""), `"waits_for_person":false`) {
		t.Fatalf("queue items also say whether they wait for a person: %s", q.Body.String()[:200])
	}
}

// 0.8.0: for contradiction decisions, once the draft is saved (the insertion place got an outcome truth), the items for pages it did not touch are "undecided"
// and are neither counted as waiting for an outcome nor shown in the queue (even after saving, 61 items looked like they waited for an outcome forever). Before saving, they wait for an outcome.
func TestUndecidedConflictJudgmentsLeaveTheQueueOnceTheDraftIsSaved(t *testing.T) {
	model := fakeAgentModel(t, func(string) float64 { return .1 })
	hookEnv(t, model)
	repo := gitRepo(t)
	initAt(t, repo)
	root := filepath.Join(repo, ".shikii")
	st := store.Store{Dir: root}
	draft := "新しい教訓の文"
	st.AppendRecord(store.Record{TraceID: "wc1", SpecID: "wiki-conflict", Type: "noul", Model: "m", Inputs: map[string]string{"lesson": "既存の記事 A", "new_lesson": draft}, InputsHash: "h1", Raw: map[string]float64{"yes": .2, "no": .8}, Top: "no", Band: "discard", At: time.Now()})
	st.AppendRecord(store.Record{TraceID: "wp1", SpecID: "wiki-place", SpecHash: "sh", Type: "noul", Model: "m", Inputs: map[string]string{"lesson": "既存の記事 A", "new_lesson": draft}, InputsHash: "h2", Raw: map[string]float64{"yes": .2, "no": .8}, Top: "no", Band: "discard", At: time.Now()})
	o := serveOpts{storeDir: root, specsDir: filepath.Join(root, "decisions"), projectScope: repo}
	h := newServer(o)
	status := func() map[string]any {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/status", nil))
		var body struct {
			Specs []map[string]any `json:"specs"`
		}
		json.Unmarshal(rec.Body.Bytes(), &body)
		for _, sp := range body.Specs {
			if sp["id"] == "wiki-conflict" {
				return sp
			}
		}
		return nil
	}
	if sp := status(); sp["awaiting_results"] != float64(1) || sp["undecided"] != float64(0) {
		t.Fatalf("before saving, waiting for an outcome: %v", sp)
	}
	// The draft was saved (the insertion-place decision got an outcome truth)
	labels, err := store.LabelsFor(&store.Record{TraceID: "wp1", SpecID: "wiki-place", SpecHash: "sh", Type: "noul", Model: "m", InputsHash: "h2", Raw: map[string]float64{"yes": .2, "no": .8}, Top: "no"}, "no", placeOtherWeight, "outcome")
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range labels {
		if err := st.AppendLabel(l); err != nil {
			t.Fatal(err)
		}
	}
	if sp := status(); sp["awaiting_results"] != float64(0) || sp["undecided"] != float64(1) || sp["unlabeled"] != float64(0) {
		t.Fatalf("after saving, undecided (settled) and not counted as waiting for an outcome: %v", sp)
	}
	q := httptest.NewRecorder()
	h.ServeHTTP(q, httptest.NewRequest(http.MethodGet, "/v1/queue?spec=wiki-conflict", nil))
	if strings.Contains(q.Body.String(), `"wc1"`) {
		t.Fatalf("undecided records are not shown in the queue: %s", q.Body.String()[:200])
	}
}

// Audit R5-3: spot checks (1 in 20 of act) wait for a person; spot checks of AI-only questions wait for an outcome. status's rituals count and the model's name.
func TestStatusCountsSpotChecksRitualsAndModel(t *testing.T) {
	model := fakeAgentModel(t, func(string) float64 { return .1 })
	hookEnv(t, model)
	t.Setenv("TYPESAFE_API_KEY", "test-key")
	repo := gitRepo(t)
	initAt(t, repo)
	root := filepath.Join(repo, ".shikii")
	st := store.Store{Dir: root}
	var samples []string
	for i := 0; i < 2000 && len(samples) < 2; i++ {
		if id := fmt.Sprintf("s%d", i); store.AuditSample(id) {
			samples = append(samples, id)
		}
	}
	if len(samples) < 2 {
		t.Fatal("no trace that hits a spot check was found")
	}
	sampled, sampledWiki := samples[0], samples[1]
	st.AppendRecord(store.Record{TraceID: sampled, SpecID: "needs-plan", SpecHash: "h", Type: "noul", Model: "m", Inputs: map[string]string{"request": "x", "agreed": "a", "open": "o"}, InputsHash: "a1", Raw: map[string]float64{"yes": .9, "no": .1}, Top: "yes", Band: "act", At: time.Now()})
	st.AppendRecord(store.Record{TraceID: sampledWiki, SpecID: "wiki-conflict", SpecHash: "h", Type: "noul", Model: "m", Inputs: map[string]string{"lesson": "a", "new_lesson": "b"}, InputsHash: "a2", Raw: map[string]float64{"yes": .9, "no": .1}, Top: "yes", Band: "act", At: time.Now()})
	appendGuardRecords(root, []guardRecord{{Session: "s", Turn: "t1", Guard: ritualPlan, Pass: false}, {Session: "s", Turn: "t1", Guard: ritualPlan, Pass: true, Recheck: true}, {Session: "s", Turn: "t2", Guard: ritualReflect, Pass: true}})
	h := newServer(serveOpts{storeDir: root, specsDir: filepath.Join(root, "decisions"), projectScope: repo})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/status", nil))
	var body struct {
		Specs   []map[string]any `json:"specs"`
		Model   map[string]any   `json:"model"`
		Summary struct {
			Guards struct {
				Rituals map[string]map[string]float64 `json:"rituals"`
			} `json:"guards"`
		} `json:"summary"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	got := map[string]map[string]any{}
	for _, sp := range body.Specs {
		got[sp["id"].(string)] = sp
	}
	if np := got["needs-plan"]; np["waiting_for_person"] != float64(1) || np["settled_by_ai"] != float64(0) {
		t.Fatalf("an act that hit a spot check waits for a person: %v", np)
	}
	if wc := got["wiki-conflict"]; wc["waiting_for_person"] != float64(0) || wc["awaiting_results"] != float64(1) {
		t.Fatalf("AI-only questions do not wait for a person even when spot-checked: %v", wc)
	}
	if body.Model["name"] != "TypeSafe Jev" {
		t.Fatalf("with a key, the decision model is Jev: %v", body.Model)
	}
	r := body.Summary.Guards.Rituals
	if r["plan"]["asked"] != 1 || r["plan"]["done"] != 0 || r["plan"]["fixed"] != 1 || r["reflect"]["asked"] != 1 || r["reflect"]["done"] != 1 {
		t.Fatalf("number of rituals done: %v", r)
	}
}
