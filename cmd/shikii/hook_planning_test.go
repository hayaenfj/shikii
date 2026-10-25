package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	shikii "github.com/hayaenfj/shikii"
	"github.com/hayaenfj/shikii/internal/spec"
	"github.com/hayaenfj/shikii/internal/store"
)

const planningTestRequest = "Use the agreed CSV columns and keep the existing filters."

func planningFixture(t *testing.T) (string, string, *atomic.Int32) {
	t.Helper()
	var calls atomic.Int32
	model := fakeAgentModel(t, func(string) float64 { calls.Add(1); return .9 })
	repo := gitRepo(t)
	root := hookEnv(t, model, repo)
	usePlanningSpec(t, root)
	return repo, root, &calls
}

func usePlanningSpec(t *testing.T, root string) {
	t.Helper()
	data, err := shikii.AgentSpecsFS.ReadFile("integrations/claude-code/specs/needs-plan.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "decisions/needs-plan.yaml"), data, 0600); err != nil {
		t.Fatal(err)
	}
}

func planningCall(t *testing.T, op string, payload any) (map[string]any, error) {
	t.Helper()
	b, _ := json.Marshal(payload)
	oldIn, oldOut := stdin, stdout
	var out bytes.Buffer
	stdin, stdout = strings.NewReader(string(b)), &out
	defer func() { stdin, stdout = oldIn, oldOut }()
	err := cmdHook([]string{"plan", op})
	var result map[string]any
	if out.Len() > 0 && json.Unmarshal(out.Bytes(), &result) != nil {
		t.Fatalf("invalid JSON: %s", out.String())
	}
	return result, err
}

func planningPayload(root string) map[string]any {
	return map[string]any{
		"session_id": "planning", "turn_id": loadSession(root, "planning").TurnID,
		"agreed": map[string]any{"summary": "CSVの列と既存フィルターの保持が指定されている。", "evidence": []map[string]string{{"source": "current_user", "quote": "agreed CSV columns"}}},
		"open":   map[string]any{"summary": "現在の依頼で未決の選択はない。", "evidence": []map[string]string{{"source": "current_user", "quote": "keep the existing filters"}}},
	}
}

func TestPlanningHookWaitsForGroundedContext(t *testing.T) {
	repo, root, calls := planningFixture(t)
	out := runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": planningTestRequest})
	if calls.Load() != 0 || !strings.Contains(feedbackContext(t, out), "hook plan query") {
		t.Fatalf("context must be prepared before needs-plan is asked: calls=%d out=%s", calls.Load(), out)
	}
	q, err := planningCall(t, "query", map[string]string{"session_id": "planning", "turn_id": loadSession(root, "planning").TurnID})
	if err != nil {
		t.Fatal(err)
	}
	sources, _ := q["source_material"].(map[string]any)
	if sources["current_user"] != planningTestRequest || q["turn_id"] == "" {
		t.Fatalf("query: %v", q)
	}
	if recs, _ := (store.Store{Dir: root}).Records("needs-plan", 0); len(recs) != 0 {
		t.Fatal("must not guess missing context")
	}
}

func TestPlanningSubmitRecordsEvidenceWithoutHumanTruthOrAdvice(t *testing.T) {
	repo, root, calls := planningFixture(t)
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": planningTestRequest})
	payload := planningPayload(root)
	r, err := planningCall(t, "submit", payload)
	if err != nil {
		t.Fatal(err)
	}
	if r["recorded"] != true || r["advice"] != "" || r["shadow"] != true {
		t.Fatalf("uncalibrated result must be shadow only: %v", r)
	}
	recs, _ := (store.Store{Dir: root}).Records("needs-plan", 0)
	if len(recs) != 1 || !strings.Contains(recs[0].Inputs["agreed"], "agreed CSV columns") || !strings.Contains(recs[0].Inputs["open"], "keep the existing filters") {
		t.Fatalf("evidence not recorded: %+v", recs)
	}
	_, labels, _ := (store.Store{Dir: root}).Counts("needs-plan")
	if len(labels) != 0 || len(mustLoad(t, filepath.Join(root, "decisions/needs-plan.yaml")).Calibration) != 0 {
		t.Fatal("AI context is not human truth or calibration")
	}
	r2, err := planningCall(t, "submit", payload)
	if err != nil || r2["trace_id"] != r["trace_id"] || calls.Load() != 1 {
		t.Fatalf("retry duplicated a decision: %v %v calls=%d", r2, err, calls.Load())
	}
}

func TestPlanningRejectsInventedOrAssistantOnlyAgreement(t *testing.T) {
	for _, tc := range []struct{ name, source, quote string }{{"invented", "current_user", "the user approved public sharing"}, {"assistant-only", "previous_assistant", "I propose CSV"}} {
		t.Run(tc.name, func(t *testing.T) {
			repo, root, calls := planningFixture(t)
			runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": "Please compare CSV formats before choosing one."})
			runHook(t, "stop", map[string]any{"session_id": "planning", "cwd": repo, "last_assistant_message": "I propose CSV"})
			calls.Store(0)
			runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": planningTestRequest})
			payload := planningPayload(root)
			payload["agreed"] = map[string]any{"summary": "承認された", "evidence": []map[string]string{{"source": tc.source, "quote": tc.quote}}}
			if _, err := planningCall(t, "submit", payload); err == nil {
				t.Fatal("unsupported agreement accepted")
			}
			if calls.Load() != 0 {
				t.Fatal("invalid evidence reached model")
			}
		})
	}
}

func TestPlanningDoesNotInventJoinedUserQuotesOrReuseOldReplies(t *testing.T) {
	repo, root, _ := planningFixture(t)
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": "Please compare the export formats before making a decision."})
	runHook(t, "stop", map[string]any{"session_id": "planning", "cwd": repo, "last_assistant_message": "The first proposal uses CSV."})
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": "Only adjust the title."})
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": planningTestRequest})
	q, err := planningCall(t, "query", map[string]string{"session_id": "planning", "turn_id": loadSession(root, "planning").TurnID})
	if err != nil {
		t.Fatal(err)
	}
	sources := q["source_material"].(map[string]any)
	if sources["previous_user"] != "Only adjust the title." || sources["previous_assistant"] != nil {
		t.Fatalf("source roles do not match the latest exchange: %v", sources)
	}
}

func TestPlanningRecordSurvivesInterruptedReceiptWrite(t *testing.T) {
	repo, root, _ := planningFixture(t)
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": planningTestRequest})
	payload := planningPayload(root)
	r, err := planningCall(t, "submit", payload)
	if err != nil {
		t.Fatal(err)
	}
	// simulate the state where, after the record was appended, the session's completion mark was not saved.
	file := sessionPath(root, "planning")
	data, _ := os.ReadFile(file)
	var session map[string]any
	if err := json.Unmarshal(data, &session); err != nil {
		t.Fatal(err)
	}
	frame := session["planning"].(map[string]any)
	delete(frame, "result")
	delete(frame, "digest")
	data, _ = json.Marshal(session)
	if err := os.WriteFile(file, data, 0600); err != nil {
		t.Fatal(err)
	}
	retry, err := planningCall(t, "submit", payload)
	if err != nil {
		t.Fatal(err)
	}
	records, _ := (store.Store{Dir: root}).Records("needs-plan", 0)
	if len(records) != 1 || retry["trace_id"] != r["trace_id"] {
		t.Fatalf("retry must keep the first record: n=%d %v %v", len(records), r, retry)
	}
}

func TestPlanningDropsResultWhenNextRequestArrives(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	model := fakeAgentModel(t, func(string) float64 { close(entered); <-release; return .9 })
	repo := gitRepo(t)
	root := hookEnv(t, model, repo)
	usePlanningSpec(t, root)
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": planningTestRequest})
	payload := planningPayload(root)
	done := make(chan error, 1)
	go func() { _, err := planningCall(t, "submit", payload); done <- err }()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("model not reached")
	}
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": "Instead of exporting, inspect the existing parser and report."})
	close(release)
	if err := <-done; err == nil {
		t.Fatal("old result returned after request changed")
	}
	if recs, _ := (store.Store{Dir: root}).Records("needs-plan", 0); len(recs) != 0 {
		t.Fatal("old context was recorded against the new request")
	}
}

func TestPlanningRejectsUnsafeInputAndSessionLinks(t *testing.T) {
	repo, root, calls := planningFixture(t)
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": planningTestRequest})
	for _, bad := range []map[string]any{{"unrecognized": true}, {"unknown": true, "summary": "合意済み"}, {"summary": strings.Repeat("x", 17000)}} {
		payload := planningPayload(root)
		payload["agreed"] = bad
		if _, err := planningCall(t, "submit", payload); err == nil {
			t.Fatal("invalid input accepted")
		}
	}
	file := sessionPath(root, "planning")
	if err := os.Rename(file, file+".private-backup"); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(file+".private-backup", file); err != nil {
		t.Fatal(err)
	}
	if _, err := planningCall(t, "query", map[string]string{"session_id": "planning", "turn_id": "1234567890abcdef"}); err == nil {
		t.Fatal("session link accepted")
	}
	if calls.Load() != 0 {
		t.Fatal("unsafe input reached model")
	}
}

func TestPlanningRejectsOldTurnAndOtherProject(t *testing.T) {
	repo, root, calls := planningFixture(t)
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": planningTestRequest})
	payload := planningPayload(root)
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": "Please stop the previous work and examine the new request."})
	if _, err := planningCall(t, "submit", payload); err == nil {
		t.Fatal("old turn accepted")
	}
	other := gitRepo(t)
	initTestProject(t, other)
	t.Chdir(other)
	if _, err := planningCall(t, "query", payload); err == nil {
		t.Fatal("another project was readable")
	}
	if calls.Load() != 0 {
		t.Fatal("invalid turn reached model")
	}
}

func TestPlanningUnknownDoesNotInventAnAnswer(t *testing.T) {
	repo, root, calls := planningFixture(t)
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": planningTestRequest})
	payload := planningPayload(root)
	payload["agreed"], payload["open"] = map[string]any{"unknown": true}, map[string]any{"unknown": true}
	r, err := planningCall(t, "submit", payload)
	if err != nil || r["recorded"] != false || r["advice"] != "" || calls.Load() != 0 {
		t.Fatalf("unknown must remain unjudged: %v %v calls=%d", r, err, calls.Load())
	}
}

func TestPlanningCapturesOnlyCurrentSessionEvidence(t *testing.T) {
	repo, root, _ := planningFixture(t)
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": "Please compare two CSV export formats before implementing."})
	runHook(t, "stop", map[string]any{"session_id": "planning", "cwd": repo, "last_assistant_message": "I propose CSV with name and status. Shall we use that?"})
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": planningTestRequest})
	q, err := planningCall(t, "query", map[string]string{"session_id": "planning", "turn_id": loadSession(root, "planning").TurnID})
	if err != nil {
		t.Fatal(err)
	}
	sources := q["source_material"].(map[string]any)
	if !strings.Contains(sources["previous_user"].(string), "compare two CSV") || !strings.Contains(sources["previous_assistant"].(string), "I propose CSV") {
		t.Fatalf("conversation source missing: %v", q)
	}
	before, _ := os.ReadFile(sessionPath(root, "planning"))
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": "<task-notification>completed</task-notification>"})
	q2, err := planningCall(t, "query", map[string]string{"session_id": "planning", "turn_id": q["turn_id"].(string)})
	if err != nil || q2["turn_id"] != q["turn_id"] || len(before) == 0 {
		t.Fatalf("notification replaced human context: %v %v", q2, err)
	}
}

func TestPlanningDoesNotReplayTruthsMissingTheNewInputs(t *testing.T) {
	repo, root, calls := planningFixture(t)
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": planningTestRequest})
	st := store.Store{Dir: root}
	inputs := map[string]string{"request": "Please improve the export workflow."}
	row := store.Label{TraceID: store.NewTraceID(), SpecID: "needs-plan", SpecHash: "legacy", Type: "noul", Model: "kev-4b", Inputs: inputs, InputsHash: store.HashInputs(inputs), Raw: map[string]float64{"yes": .9, "no": .1}, Truth: "yes", Weight: 1, Source: "ui", At: time.Now()}
	if err := st.AppendLabel(row); err != nil {
		t.Fatal(err)
	}
	var report bytes.Buffer
	err := runCalibrate(filepath.Join(root, "decisions/needs-plan.yaml"), calibrateOpts{output: &report, ef: endpointFlags{local: hookLocalURL, model: "kev-4b"}, storeDir: root, labeled: "none", write: true})
	if err == nil || !strings.Contains(err.Error(), "no labels") || calls.Load() != 0 {
		t.Fatalf("old truth must not be reinterpreted with invented context: %v calls=%d\n%s", err, calls.Load(), report.String())
	}
	_, labels, _ := st.Counts("needs-plan")
	if len(labels) != 1 || labels[0].SpecHash != "legacy" {
		t.Fatal("old truth changed or became a new truth")
	}
}

func TestPlanningKeepsOldRecordsOutOfTheNewAnswerQueue(t *testing.T) {
	_, root, _ := planningFixture(t)
	sp := mustLoad(t, filepath.Join(root, "decisions/needs-plan.yaml"))
	st := store.Store{Dir: root}
	var old store.Record
	for i := 0; i < 3; i++ {
		inputs := map[string]string{"request": fmt.Sprint(i)}
		hash := "legacy"
		if i == 2 {
			inputs["agreed"], inputs["open"], hash = "agreed scope", "unselected format", sp.Hash()
		}
		r := store.Record{TraceID: store.NewTraceID(), SpecID: "needs-plan", SpecHash: hash, Type: "noul", Inputs: inputs, InputsHash: store.HashInputs(inputs), Model: "m", Raw: map[string]float64{"yes": .9, "no": .1}, Top: "yes", P: .9, Band: "ask", At: time.Now(), Display: &store.RecordDisplay{Lang: "ja", Question: "以前の質問"}}
		if err := st.AppendRecord(r); err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			old = r
		}
	}
	labels, err := store.LabelsFor(&old, "no", 1, "ui")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AppendLabels(labels); err != nil {
		t.Fatal(err)
	}
	s := httptest.NewServer(newServer(serveOpts{specsDir: filepath.Join(root, "decisions"), storeDir: root}))
	defer s.Close()
	var queue, answered struct {
		Items []map[string]any `json:"items"`
	}
	getJSON(t, s.URL+"/v1/queue?spec=needs-plan", &queue)
	if len(queue.Items) != 1 {
		t.Fatalf("old pending records mixed into the new question: %v", queue.Items)
	}
	getJSON(t, s.URL+"/v1/answered?spec=needs-plan", &answered)
	if len(answered.Items) != 1 || answered.Items[0]["legacy_planning"] != true || answered.Items[0]["record_question"] != "以前の質問" {
		t.Fatalf("old answer must remain clearly identified: %v", answered.Items)
	}
	var status struct {
		Specs []map[string]any `json:"specs"`
	}
	getJSON(t, s.URL+"/v1/status", &status)
	var current map[string]any
	for _, item := range status.Specs {
		if item["id"] == "needs-plan" {
			current = item
		}
	}
	if current["unlabeled"] != float64(1) || current["legacy_planning_records"] != float64(2) {
		t.Fatalf("status mixed old and new records: %v", status)
	}
	if records, _ := st.Records("needs-plan", 0); len(records) != 3 {
		t.Fatal("old history was removed")
	}
}

// For a request that a calibrated prompt-kind confidently answered "does not change files", the agent is not asked for the pre-start organizing.
// Without a line, and for replies or choices on a proposal (decide), it is asked as before.
func TestPlanningIsSkippedOnlyForConfidentNonEditRequests(t *testing.T) {
	top := "explain"
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Questions map[string]struct {
				Type string `json:"type"`
			} `json:"questions"`
		}
		body, _ := readAll(r)
		_ = json.Unmarshal(body, &req)
		ans := map[string]any{}
		for k, q := range req.Questions {
			if q.Type == "noul" {
				ans[k] = map[string]any{"type": "noul", "noul": 0.2}
				continue
			}
			probs := map[string]float64{"edit": 0.02, "explain": 0.02, "investigate": 0.02, "run": 0.02, "decide": 0.02}
			probs[top] = 0.92
			ans[k] = map[string]any{"type": "choice", "choice": top, "probabilities": probs, "confidence": 0.92}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"model": "kev-4b", "answers": ans})
	}))
	t.Cleanup(model.Close)
	repo := gitRepo(t)
	agent := hookEnv(t, model, repo)
	usePlanningSpec(t, agent)
	ask := func(session string) string {
		return runHook(t, "user-prompt-submit", map[string]any{"session_id": session, "cwd": repo, "prompt": "what does the retry loop in the uploader do?"})
	}
	if out := ask("no-line"); !strings.Contains(out, "hook plan query") {
		t.Fatalf("without a line the planning context must still be asked for: %s", out)
	}
	path := filepath.Join(agent, "decisions", "prompt-kind.yaml")
	sp := mustLoad(t, path)
	sp.Calibration = []spec.Calibration{{Model: "kev-4b", SpecHash: sp.Hash(), Method: "identity", N: 50}}
	if err := sp.Save(); err != nil {
		t.Fatal(err)
	}
	if out := ask("explain"); strings.Contains(out, "hook plan query") || loadSession(agent, "explain").Planning != nil {
		t.Fatalf("a confident explain must skip the planning context: %s %+v", out, loadSession(agent, "explain").Planning)
	}
	top = "decide"
	if out := ask("decide"); !strings.Contains(out, "hook plan query") {
		t.Fatalf("a reply to a proposal must keep the planning context: %s", out)
	}
	top = "edit"
	if out := ask("edit"); !strings.Contains(out, "hook plan query") {
		t.Fatalf("an edit must keep the planning context: %s", out)
	}
}
