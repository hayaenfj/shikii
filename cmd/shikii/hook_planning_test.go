package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hayaenfj/shikii"
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
	if personTruths(labels) != 0 || len(mustLoad(t, filepath.Join(root, "decisions/needs-plan.yaml")).Calibration) != 0 {
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

// The agent's organizing becomes the needs-plan decision's input (sent to the decision model and kept in records). For an English user it starts with a Japanese label.
func TestAuditEnglishPlanningInputHasNoJapanese(t *testing.T) {
	out, err := renderPlanningFact(&planningFact{Summary: "Write scripts/check-totals.sh and run it", Evidence: []planningQuote{{Source: "current_user", Quote: "Write a script"}}},
		map[string]string{"current_user": "Write a script at scripts/check-totals.sh"}, true)
	if err != nil {
		t.Fatal(err)
	}
	if auditJapanese.MatchString(out) {
		t.Fatalf("the decision input for an English user contains Japanese: %q", out)
	}
}

// Audit 0.9.2 (translation): what moving the texts to English broke or left untested.

// T2: the preface Go puts in front of the pre-work organizing is the one the page removes (web/src/components/ItemCard.svelte).
func TestAuditTranslationPlanningPrefaceMatchesThePage(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "web", "src", "components", "ItemCard.svelte"))
	if err != nil {
		t.Fatal(err)
	}
	var line string
	for _, l := range strings.Split(string(b), "\n") {
		if strings.Contains(l, "planningPrefixes =") {
			line = l
		}
	}
	if line == "" {
		t.Fatal("ItemCard.svelte no longer has planningPrefixes")
	}
	if !strings.Contains(line, `"`+planningPreface+`"`) && !strings.Contains(line, `'`+planningPreface+`'`) {
		t.Fatalf("the page removes a different preface than Go writes.\nGo:   %q\npage: %s", planningPreface, line)
	}
	got, err := renderPlanningFact(&planningFact{Summary: "s", Evidence: []planningQuote{{Source: "current_user", Quote: "q"}}}, map[string]string{"current_user": "q"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(got, planningPreface+"s") {
		t.Fatalf("renderPlanningFact starts with planningPreface: %q", got)
	}
}

// Each notice keeps the instructions an agent needs to act on it. Where each row came from:
//   - T4: habitsNotice keeps handoff_write (the MCP way to write work records); the planning notice keeps lesson:<id> (how to cite a lesson).
//   - 0.9.6 audit, low 2: restates is the <id> inside history:<id>:user (restateDone compares the id exactly, so the whole history key
//     silently does nothing), and maintenance sends the latest revision after reading the sources and existing pages.
//   - 0.9.7 audit G-3: the key query returns is lesson:<id>:<revision> (submit refuses lesson:<id>), in the full, short and no-query notices.
//   - 0.9.7 final audit F2: the short form also says where the restates id comes from (history:<id>:user).
//   - re3-3 (F4): the short forms point to the details as the full text does (the skill's references/...); a bare path reads as one in the project.
//   - Low 1: Codex often passed the JSON as an argument first and was refused; the notice says it goes on standard input.
//   - continues_previous is explained where the agent is asked to organize, and where the Stop asks again (planningShape).
//   - 0.9.7 second real-agent check: Claude Code submitted before reading what query returned; both forms say to read the query result first.
func TestNoticesKeepTheirInstructions(t *testing.T) {
	full, short := planningNotice("s", "0123456789abcdef"), planningNoticeShort("s", "0123456789abcdef")
	for _, c := range []struct {
		name, text string
		want       []string
	}{
		{"planning notice (full)", full, []string{"evidence source to lesson:<id>", "lesson:<id>:<revision>", "the <id> in history:<id>:user",
			"the skill's references/planning.md", "standard input", "continues_previous to true", "false when it is a new request",
			"omit it when unsure", "after reading what query returned"}},
		{"planning notice (short)", short, []string{"lesson:<id>:<revision>", "history:<id>:user", "the skill's references/planning.md",
			"after reading what query returned"}},
		{"planning notice (no query)", planningNoQueryNotice, []string{"lesson:<id>:<revision>"}},
		{"planningShape", planningShape, []string{"the <id> in history:<id>:user", "continues_previous is true, false or left out"}},
		{"habitsNotice", habitsNotice, []string{"handoff_write in shikii_wiki", "shikii handoff write <path>"}},
		{"reflection notice (full)", reflectAsk("shikii"), []string{"the latest page_id", "Read the sources and existing pages"}},
		{"reflection notice (short)", reflectAskShort("X", nil), []string{"the skill's references/wiki.md"}},
	} {
		for _, w := range c.want {
			if !strings.Contains(c.text, w) {
				t.Errorf("the %s lacks %q: %q", c.name, w, c.text)
			}
		}
	}
}

// a restatement of the same request labels the previous request as not done, whatever the agent says about grouping
func TestAudit3AgentCannotEraseRestatement(t *testing.T) {
	repo, root, _ := planningFixture(t)
	done := continuesTurn(t, repo, root, "Please use the agreed CSV columns and keep the existing filters.")
	continuesSubmit(t, root, true)
	runHook(t, "stop", map[string]any{"session_id": "planning", "cwd": repo, "last_assistant_message": "Done again."})
	if l, ok := doneLabel(t, root, done); !ok || l.Truth != "no" {
		t.Fatalf("a restatement must label the previous request as not done even when the agent says it continues: %+v %v", l, ok)
	}
}

// query reads; it does not take organized content such as continues_previous (test hole: mutation M8)
func TestAudit3QueryRefusesContinuesPrevious(t *testing.T) {
	repo, root, _ := planningFixture(t)
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": continuesFirst})
	if _, err := planningCall(t, "query", map[string]any{"session_id": "planning", "turn_id": loadSession(root, "planning").TurnID, "continues_previous": true}); err == nil {
		t.Fatal("query must refuse continues_previous")
	}
}

// 0.9.7 final audit (D1 to D2, F1 to F3). The checks are written independently in the tests; the functions are only what is tested.

// F2 control: an organizing sent from the short form alone is accepted (query, agreed, open and continues_previous added to its JSON).
func TestAuditFinalF2ShortPlanningNoticeAloneSubmits(t *testing.T) {
	repo, root, _ := planningFixture(t)
	turn := func(prompt string) string {
		t.Helper()
		return feedbackContext(t, runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": prompt}))
	}
	turn(planningTestRequest)
	if _, err := planningCall(t, "submit", planningPayload(root)); err != nil {
		t.Fatal(err)
	}
	short := turn("Now add a tax rate to the agreed CSV columns and keep the existing filters.")
	if !strings.Contains(short, "as earlier in this session") {
		t.Fatalf("precondition: the second notice is the short form: %q", short)
	}
	var ids map[string]any
	if err := json.Unmarshal([]byte(regexp.MustCompile(`\{"session_id":"[^"]*","turn_id":"[0-9a-f]{16}"\}`).FindString(short)), &ids); err != nil {
		t.Fatalf("the JSON at the end of the short form: %v", err)
	}
	ids["query"] = "CSV columns filters"
	q, err := planningCall(t, "query", ids)
	if err != nil {
		t.Fatal(err)
	}
	src := q["source_material"].(map[string]any)["current_user"].(string)
	ids["agreed"] = map[string]any{"summary": "\u7a0e\u7387\u3092 CSV \u306e\u5217\u306b\u8db3\u3059\u3002", "evidence": []map[string]string{{"source": "current_user", "quote": src[:20]}}}
	ids["open"] = map[string]any{"unknown": true}
	ids["continues_previous"] = false
	if _, err := planningCall(t, "submit", ids); err != nil {
		t.Fatalf("F2: an organizing sent from the short form alone was refused: %v", err)
	}
}

// RED tests from the 0.9.3 behaviour check with real agents (ba89f21). Each fails on ba89f21.

// 1. The agent's continues_previous changes the previous request's outcome truth: attaching a short request to the one before
// makes the previous request text long, so the person's restatement of the short request is no longer similar enough and the
// "no" is lost (Codex attached every short request and the restatement; Claude attached "write it in the README too").
func TestGroupingDoesNotEraseRestatement(t *testing.T) {
	for _, continues := range []bool{false, true} {
		repo, root, _ := planningFixture(t)
		continuesTurn(t, repo, root, "README\u306b\u3082\u66f8\u3044\u3066")
		continuesSubmit(t, root, continues)
		runHook(t, "stop", map[string]any{"session_id": "planning", "cwd": repo, "last_assistant_message": "Wrote the README."})
		done := ""
		for _, r := range loadSession(root, "planning").Reviews {
			if r.Spec == "done-as-asked" {
				done = r.Trace
			}
		}
		if done == "" {
			t.Fatalf("continues=%v: the README turn must have a done-as-asked decision", continues)
		}
		runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": "README\u306b\u3082\u66f8\u3044\u3066\u304f\u3060\u3055\u3044"})
		continuesSubmit(t, root, false)
		if l, ok := doneLabel(t, root, done); !ok || l.Truth != "no" || l.Source != "outcome" {
			t.Errorf("continues_previous=%v: the person restated the README request, so its outcome must be no whatever the grouping: %+v %v", continues, l, ok)
		}
	}
}

// 2. The organizing notice tells the agent to attach "a short follow-up on the same work". Codex then attached
// "also check the totals", "write it in the README too" and the restatement of request 1, and the whole session became one request.
// A short request that asks for new work must be its own request (e3ac97e).
func TestNoticeKeepsShortRequestsWithContentApart(t *testing.T) {
	n := planningNotice("s", "t")
	if strings.Contains(n, "short follow-up on the same work") {
		t.Fatalf("the notice invites attaching short requests with content: %s", n)
	}
}

// a short request with content (the list treats it as new) that the agent says continues the previous one is attached to it,
// and the previous request's outcome is not settled from it (not at plan submit, not at the turn's Stop)
func TestPlanContinuesMergesShortRequest(t *testing.T) {
	for _, reply := range []string{"main取り込んで", "それでいこう"} { // "merge main", "let's go with that" (not in the list)
		t.Run(reply, func(t *testing.T) {
			repo, root, _ := planningFixture(t)
			done := continuesTurn(t, repo, root, reply)
			if s := loadSession(root, "planning"); s.Request != reply {
				t.Fatalf("provisional decision: the list treats %q as new, got %q", reply, s.Request)
			}
			continuesSubmit(t, root, true)
			s := loadSession(root, "planning")
			if s.Request != continuesFirst+" / "+reply || s.RequestText != continuesFirst+" / "+reply {
				t.Fatalf("the agent said it continues: must be attached to the previous request, got %q / %q", s.Request, s.RequestText)
			}
			if l, ok := doneLabel(t, root, done); ok {
				t.Fatalf("a continuation must not settle the previous request's outcome: %+v", l)
			}
			runHook(t, "stop", map[string]any{"session_id": "planning", "cwd": repo, "last_assistant_message": "Merged."})
			if s := loadSession(root, "planning"); !s.Judged || s.Completed == nil || s.Completed.Request != continuesFirst+" / "+reply {
				t.Fatalf("the Stop must judge the attached request (listing kept for it): %+v", s)
			}
			if l, ok := doneLabel(t, root, done); ok {
				t.Fatalf("the Stop must not settle the previous outcome from a continuation: %+v", l)
			}
		})
	}
}

// a list acknowledgement that the agent says is a new request becomes its own request, and the previous outcome is settled from it
func TestPlanNewSplitsAcknowledgement(t *testing.T) {
	repo, root, _ := planningFixture(t)
	done := continuesTurn(t, repo, root, "ok")
	if s := loadSession(root, "planning"); s.Request != continuesFirst+" / ok" {
		t.Fatalf("provisional decision: the list attaches ok, got %q", s.Request)
	}
	continuesSubmit(t, root, false)
	s := loadSession(root, "planning")
	if s.Request != "ok" || s.RequestText != "ok" {
		t.Fatalf("the agent said it is new: must stand alone, got %q / %q", s.Request, s.RequestText)
	}
	if l, ok := doneLabel(t, root, done); ok {
		t.Fatalf("the grouping never decides a truth: an acknowledgement does not settle the previous outcome (audit 3): %+v", l)
	}
	runHook(t, "stop", map[string]any{"session_id": "planning", "cwd": repo, "last_assistant_message": "Done."})
	if s := loadSession(root, "planning"); !s.Judged || s.Completed == nil || s.Completed.Request != "ok" {
		t.Fatalf("the Stop must judge the request standing alone: %+v", s)
	}
}

// 0.9.3 re-check: the minimal organizing (agreed and open unknown, continues_previous set) answered "recorded": false … skipped,
// which reads as a failure though the grouping was applied. The receipt says what happened to the grouping.
func TestPlanSubmitSaysHowTheMessageWasGrouped(t *testing.T) {
	for _, c := range []struct {
		reply     string
		continues bool
		want      string
	}{{"\u305d\u308c\u3067\u3044\u3053\u3046", true, "attached"}, {"ok", false, "own"}} {
		t.Run(c.want, func(t *testing.T) {
			repo, root, _ := planningFixture(t)
			continuesTurn(t, repo, root, c.reply)
			out, err := planningCall(t, "submit", map[string]any{
				"session_id": "planning", "turn_id": loadSession(root, "planning").TurnID,
				"agreed": map[string]any{"unknown": true}, "open": map[string]any{"unknown": true}, "continues_previous": c.continues,
			})
			if err != nil {
				t.Fatal(err)
			}
			if g, _ := out["grouping"].(string); !strings.Contains(g, c.want) {
				t.Fatalf("the receipt says how the message was grouped (%q): %v", c.want, out)
			}
		})
	}
}

func TestStopDoesNotAskContinuesPreviousOtherwise(t *testing.T) {
	cases := map[string]struct {
		reply  string
		submit bool
		bare   bool // organized, but continues_previous left out (unsure): not asked again
	}{
		"organizing without continues_previous": {reply: "OKです", bare: true},
		"listed acknowledgement":                {reply: "ok"},
		"long message":                          {reply: "次の版のリリースノートを書いて、0.9.2 からの変更をすべて並べ、英語と日本語の両方で用意してください。"},
		"organizing came":                       {reply: "OKです", submit: true},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			repo, root, _ := planningFixture(t)
			continuesTurn(t, repo, root, c.reply)
			if c.submit {
				continuesSubmit(t, root, true)
			}
			if c.bare {
				if _, err := planningCall(t, "submit", map[string]any{"session_id": "planning", "turn_id": loadSession(root, "planning").TurnID, "agreed": map[string]any{"unknown": true}, "open": map[string]any{"unknown": true}}); err != nil {
					t.Fatal(err)
				}
			}
			out := runHook(t, "stop", map[string]any{"session_id": "planning", "cwd": repo, "last_assistant_message": "Done."})
			if _, block := stopReason(t, out); block {
				t.Fatalf("the Stop must not ask for continues_previous here: %s", out)
			}
			if s := loadSession(root, "planning"); !s.Judged {
				t.Fatalf("the Stop judges at once: %+v", s)
			}
		})
	}
}

// 0.9.7: the pre-start organizing notice (about 1,700 characters) was added in full to every request. The full text now comes on the
// session's first request and every noticeFullEvery requests (so it survives context compaction); in between a short form names the same
// commands and this turn's IDs. A request whose organizing was not submitted does not bring the full text back: the short form alone is
// enough to submit, and the Stop check repeats the steps. (Tracking misses across requests was tried first; every audit found another way
// it went wrong, and the structure was simplified instead, 2026-10-27.)
func TestPlanningNoticeIsShortAfterTheFirstRequest(t *testing.T) {
	repo, root, _ := planningFixture(t)
	const full = "putting topic words in query"
	turn := func(prompt string) string {
		t.Helper()
		return feedbackContext(t, runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": prompt}))
	}
	submit := func() {
		t.Helper()
		if _, err := planningCall(t, "submit", planningPayload(root)); err != nil {
			t.Fatal(err)
		}
	}
	first := turn(planningTestRequest)
	if !strings.Contains(first, full) {
		t.Fatalf("the session's first request gets the full text: %q", first)
	}
	submit()
	second := turn("Now add a tax rate to the agreed CSV columns and keep the existing filters.")
	id := loadSession(root, "planning").TurnID
	if strings.Contains(second, full) || !strings.Contains(second, "hook plan query") || !strings.Contains(second, "hook plan submit") || !strings.Contains(second, id) {
		t.Fatalf("after an organized request the short form names the commands and this turn's id %s: %q", id, second)
	}
	// under two fifths: the short form keeps the essential rules (re-audit S1, audit E). Measured with the binary's path taken out: the full
	// text names it twice and the short form once, so a long install path (a coverage build in a deep folder) broke the ratio, not the text.
	command := regexp.MustCompile(`Pipe the JSON below to (.+?) hook plan query`).FindStringSubmatch(second)
	if command == nil {
		t.Fatalf("the short form names the binary: %q", second)
	}
	short, long := strings.ReplaceAll(second, command[1], "shikii"), strings.ReplaceAll(first, command[1], "shikii")
	if len(short)*5 >= len(long)*2 {
		t.Fatalf("the short form is short: %d of %d characters", len(short), len(long))
	}
	// the second request's organizing is not submitted: the next request still gets the short form, which is enough to submit
	if third := turn("Then sort the agreed CSV columns by date and keep the existing filters."); strings.Contains(third, full) || !strings.Contains(third, "hook plan submit") {
		t.Fatalf("after a missed organizing, the short form: %q", third)
	}
	submit()
	fulls := 0
	for i := 0; i < noticeFullEvery; i++ {
		if strings.Contains(turn("Request "+strings.Repeat("x", i+1)+": use the agreed CSV columns and keep the existing filters."), full) {
			fulls++
		}
		submit()
	}
	if fulls != 1 {
		t.Fatalf("the full text comes back once every %d requests: %d times", noticeFullEvery, fulls)
	}
}

// Properties F1 to F3 (F2 as decided on 2026-10-27: no miss is tracked, and the short form alone is enough) over random sequences of requests where the agent sometimes submits its organizing and sometimes does not.
func TestPlanningNoticeFormOverRandomSessions(t *testing.T) {
	repo, root, _ := planningFixture(t)
	r := rand.New(rand.NewSource(97))
	const full, short = "putting topic words in query", "as earlier in this session"
	missedBefore, shortsInARow := false, 0
	var history []string
	for i := 0; i < 40; i++ {
		prompt := fmt.Sprintf("Request %d: use the agreed CSV columns and keep the existing filters.", i)
		notice := feedbackContext(t, runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": prompt}))
		isFull, isShort := strings.Contains(notice, full), strings.Contains(notice, short)
		if isFull == isShort {
			t.Fatalf("request %d: one form of the organizing notice: %q", i, notice)
		}
		switch {
		case i == 0 && !isFull:
			t.Fatalf("F1: the session's first notice is the full text\nhistory: %v", history)
		case missedBefore && !strings.Contains(notice, "hook plan submit"):
			t.Fatalf("F2: after a missed organizing the notice alone is enough to submit\nhistory: %v", history)
		}
		if isShort {
			shortsInARow++
		} else {
			shortsInARow = 0
		}
		if shortsInARow >= noticeFullEvery {
			t.Fatalf("F3: %d short notices in a row\nhistory: %v", shortsInARow, history)
		}
		submitted := r.Intn(4) != 0
		if submitted {
			if _, err := planningCall(t, "submit", planningPayload(root)); err != nil {
				t.Fatal(err)
			}
		}
		missedBefore = !submitted
		form := "short"
		if isFull {
			form = "full"
		}
		history = append(history, fmt.Sprintf("%s/submitted=%v", form, submitted))
	}
}

// 0.9.3 re-check (5ca36c5): the real sequence of the behaviour check has five turns between request 1 and its restatement
// ("continue", "OK", "let's go with that", "also check the totals", "write it in the README too"). restates must still reach request 1.
func TestRestatesReachesRequestSixTurnsBack(t *testing.T) {
	repo, root, _ := planningFixture(t)
	session := "planning"
	turn, done := "", ""
	var group []string // done-as-asked of request 1 and of the acknowledgements attached to it
	prompts := []string{restateFirst, "\u7d9a\u3051\u3066", "OK\u3067\u3059", "\u305d\u308c\u3067\u3044\u3053\u3046", restateSecond, restateThird}
	for i, prompt := range prompts {
		runHook(t, "user-prompt-submit", map[string]any{"session_id": session, "cwd": repo, "prompt": prompt})
		if i > 0 {
			payload := map[string]any{"session_id": session, "turn_id": loadSession(root, session).TurnID, "agreed": map[string]any{"unknown": true}, "open": map[string]any{"unknown": true}, "continues_previous": i <= 3}
			if _, err := planningCall(t, "submit", payload); err != nil {
				t.Fatalf("plan submit: %v", err)
			}
		}
		runHook(t, "stop", map[string]any{"session_id": session, "cwd": repo, "last_assistant_message": "Done."})
		if i <= 3 {
			s := loadSession(root, session)
			if i == 0 {
				turn = s.TurnID
			}
			for _, r := range s.Reviews {
				if r.Spec == "done-as-asked" {
					if i == 0 {
						done = r.Trace
					}
					group = append(group, r.Trace)
				}
			}
		}
	}
	if turn == "" || done == "" {
		t.Fatal("request 1 must have a turn and a done-as-asked decision")
	}
	runHook(t, "user-prompt-submit", map[string]any{"session_id": session, "cwd": repo, "prompt": restateAgain})
	out, err := planningCall(t, "query", map[string]any{"session_id": session, "turn_id": loadSession(root, session).TurnID})
	if err != nil {
		t.Fatalf("plan query: %v", err)
	}
	raw, _ := json.Marshal(out)
	if !strings.Contains(string(raw), "history:"+turn+":user") {
		t.Errorf("plan query must still list request 1 (history:%s:user) so the agent can name it in restates; got %.600s", turn, raw)
	}
	restateSubmit(t, root, session, turn)
	found := false
	for _, tr := range group {
		if l, ok := doneLabel(t, root, tr); ok && l.Truth == "no" && l.Source == "outcome" {
			found = true
		}
	}
	if !found {
		t.Fatalf("a restatement of request 1 five turns later, named by restates, must give request 1 (or the acknowledgements joined to it) \"no\": traces %v", group)
	}
}

// 0.9.1: rules, tests and procedures (commands or scripts) the agent newly made or changed itself are also written to the wiki on the spot (adding to or fixing
// an existing page if there is one). The ritual used to write only the person's corrections, so even the way to run tests (scripts/go-test.sh) never reached the wiki (the person: "if you
// make something new as a rule or a test, always write it in the wiki"). So that it reaches agents that do not read the skill (Codex without hooks, OpenCode),
// it goes into every entry point (the ritual one-liner, the reflection text, the AGENTS.md section, the MCP guidance, the skill)
func TestNewRulesTestsAndProceduresGoToTheWiki(t *testing.T) {
	ja := "新しく作った・変えた決まり・テスト・手順"
	en := "rule, test or procedure"
	reflectText := reflectAsk("shikii")
	skill, _ := os.ReadFile("../../skills/shikii/SKILL.md")
	wikiRef, _ := os.ReadFile("../../skills/shikii/references/wiki.md")
	// The pre-work sorting guidance arrives with every request (the ritual one-liner is only once per session, and Claude Code in a project without work rules did not use it when it finished making something; measured in 0.9.1)
	for name, text := range map[string]string{"ritual one-liner (habitsNotice)": habitsNotice, "reflection text": reflectText, "pre-work sorting guidance (every time)": planningNotice("s", "t")} {
		if !strings.Contains(text, ja) && !strings.Contains(text, en) { // Japanese before the English translation, English after
			t.Errorf("%s lacks the rule to also write %q (or %q) to the wiki", name, en, ja)
		}
	}
	for name, text := range map[string]string{"MCP guidance": mcpInstructions, "SKILL.md": string(skill), "references/wiki.md": string(wikiRef)} {
		if !strings.Contains(text, en) {
			t.Errorf("%s lacks the rule to also write %q to the wiki", name, en)
		}
	}
}

// 0.11: the skill asks the agent to search the wiki again by the new topic when the work moves to another area mid-task, not only
// before work (the person agreed to add it; lessons delivered before work were chosen by the first request's words).
func TestSkillAsksToSearchTheWikiAgainWhenTheWorkMoves(t *testing.T) {
	skill, _ := os.ReadFile("../../skills/shikii/SKILL.md")
	planning, _ := os.ReadFile("../../skills/shikii/references/planning.md")
	for name, text := range map[string]string{"SKILL.md": string(skill), "references/planning.md": string(planning)} {
		if !strings.Contains(text, "moves to another area") {
			t.Errorf("%s asks to search the wiki again by the new topic when the work moves to another area", name)
		}
	}
}


// pagePeek reads the page's /v1/status entry and /v1/queue items for one question, through the handler (no listening port).
func pagePeek(t *testing.T, root, repo, specID string) (map[string]any, []map[string]any) {
	t.Helper()
	h := newServer(serveOpts{storeDir: root, specsDir: filepath.Join(root, "decisions"), projectScope: feedbackScope(repo)})
	var status struct {
		Specs []map[string]any `json:"specs"`
	}
	var queue struct {
		Items []map[string]any `json:"items"`
	}
	for _, c := range []struct {
		path string
		into any
	}{{"/v1/status", &status}, {"/v1/queue?spec=" + specID, &queue}} {
		r := httptest.NewRecorder()
		h.ServeHTTP(r, httptest.NewRequest("GET", c.path, nil))
		if r.Code != 200 || json.Unmarshal(r.Body.Bytes(), c.into) != nil {
			t.Fatalf("%s: %d %s", c.path, r.Code, r.Body.String())
		}
	}
	for _, s := range status.Specs {
		if s["id"] == specID {
			return s, queue.Items
		}
	}
	t.Fatalf("no status for %s", specID)
	return nil, nil
}

// planningWith submits the organizing for one request with the decision model answering p (the probability of yes) and the agent's open
// either filled (the agent's answer is "yes, ask first") or unknown (its answer is "no").
func planningWith(t *testing.T, p float64, openFilled bool) (repo, root, trace string) {
	t.Helper()
	model := fakeAgentModel(t, func(string) float64 { return p })
	repo = gitRepo(t)
	root = hookEnv(t, model, repo)
	usePlanningSpec(t, root)
	runHook(t, "user-prompt-submit", map[string]any{"session_id": "planning", "cwd": repo, "prompt": planningTestRequest})
	payload := planningPayload(root)
	if !openFilled {
		payload["open"] = map[string]any{"unknown": true}
	}
	r, err := planningCall(t, "submit", payload)
	if err != nil || r["recorded"] != true {
		t.Fatalf("submit: %v %v", r, err)
	}
	return repo, root, r["trace_id"].(string)
}

// 0.11 (5-1, 5-3): when the decision model and the agent give the same needs-plan answer, the person is not asked, even in the ask band.
// The agreement is kept as a machine truth (source agreed) that never counts as the person's truth.
func TestPlanningAgreementIsSettledWithoutThePerson(t *testing.T) {
	repo, root, trace := planningWith(t, .6, true) // ask band; the model says yes, and the agent left something open (yes)
	_, labels, _ := (store.Store{Dir: root}).Counts("needs-plan")
	if len(labels) != 1 || labels[0].Source != "agreed" || labels[0].Truth != "yes" || labels[0].TraceID != trace || store.HumanSource(labels[0].Source) {
		t.Fatalf("an agreement is kept as an agreed machine truth: %+v", labels)
	}
	if asked := loadSession(root, "planning").Asked; len(asked) != 0 {
		t.Fatalf("an agreement does not wait for the person: %+v", asked)
	}
	status, items := pagePeek(t, root, repo, "needs-plan")
	if status["waiting_for_person"] != float64(0) || status["labels"] != float64(0) {
		t.Fatalf("nothing waits and no truth of the person is counted: %v", status)
	}
	for _, it := range items {
		if it["trace_id"] == trace && it["waits_for_person"] == true {
			t.Fatalf("the agreed decision does not wait in the queue: %v", it)
		}
	}
}

// 0.11 (5-2): only a disagreement waits for the person, with both answers on the card. A confident "no" of the model (discard band) the agent
// disagrees with waits too, and an ask-band "yes" the agent answers "no" to.
func TestPlanningDisagreementWaitsForThePerson(t *testing.T) {
	for _, c := range []struct {
		p          float64
		openFilled bool
		model      string
		agent      string
	}{{.6, false, "yes", "no"}, {.2, true, "no", "yes"}} {
		repo, root, trace := planningWith(t, c.p, c.openFilled)
		_, labels, _ := (store.Store{Dir: root}).Counts("needs-plan")
		if len(labels) != 0 {
			t.Fatalf("p=%.1f: a disagreement writes no truth: %+v", c.p, labels)
		}
		if asked := loadSession(root, "planning").Asked; len(asked) != 1 || asked[0].Trace != trace {
			t.Fatalf("p=%.1f: a disagreement waits for the person: %+v", c.p, asked)
		}
		status, items := pagePeek(t, root, repo, "needs-plan")
		if status["waiting_for_person"] != float64(1) {
			t.Fatalf("p=%.1f: the status counts the disagreement: %v", c.p, status)
		}
		if len(items) != 1 || items[0]["trace_id"] != trace || items[0]["waits_for_person"] != true || items[0]["review_reason"] != "disagree" {
			t.Fatalf("p=%.1f: the queue shows the disagreement as waiting: %v", c.p, items)
		}
		pair, _ := items[0]["pair"].(map[string]any)
		conf := c.p
		if c.model == "no" {
			conf = 1 - c.p
		}
		if pair["model"] != c.model || pair["agent"] != c.agent || fmt.Sprintf("%.2f", pair["model_confidence"]) != fmt.Sprintf("%.2f", conf) {
			t.Fatalf("p=%.1f: the card carries both answers: %v", c.p, pair)
		}
		if c.openFilled && !strings.Contains(fmt.Sprint(pair["agent_reason"]), "keep the existing filters") {
			t.Fatalf("p=%.1f: the agent's reason is its organizing of what is open: %v", c.p, pair)
		}
	}
}
