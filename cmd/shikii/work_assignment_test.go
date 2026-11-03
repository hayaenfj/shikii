package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestAssignmentAppliesSourceBackedProcedureAndProbe(t *testing.T) {
	b, s := loopBook(t)
	p := loopPage(t, b, s, map[string]any{"title": "Quartz procedure", "kind": "howto", "body": "Check only the small input."})
	a := assignmentInput("fix-procedure", "case-1", "reviewer", "review")
	v, err := workCall(t, b, "prepare", map[string]any{"query": "lesson:" + p.ID, "assignment": a})
	if err != nil {
		t.Fatal(err)
	}
	note := map[string]any{"target": "knowledge1", "sources": []string{"new1"}, "change_reason": "A second synthetic observation exposed a size condition.", "edits": []any{map[string]any{"field": "body", "before": "Check only the small input.", "after": "Compare the small and large inputs under the same owner."}}, "learning": map[string]any{"probe": map[string]any{"question": "Does input size distinguish the results?", "method": "Compare two bounded synthetic inputs.", "reconsider_when": "The owner condition differs.", "results": []any{map[string]any{"conditions": "same owner, two synthetic sizes", "conclusion": "The large input exposed an error.", "assessment": "challenges", "sources": []string{"new1"}}}}, "practice": map[string]any{"steps": []any{map[string]any{"condition": "Before exporting", "action": "Compare small and large synthetic inputs", "avoid": "Assuming one input covers all sizes", "check": "Both retain the owner condition", "sources": []string{"new1"}}}}}}
	r, err := workCall(t, b, "record", map[string]any{"assignment": a, "receipt": v["receipt"], "reviewed": []string{"knowledge1"}, "event": map[string]any{"id": "procedure-result", "state": "completed", "summary": "The synthetic comparison exposed a size condition."}, "notes": []any{note}})
	if err != nil || r["complete"] != true {
		t.Fatal(r, err)
	}
	live, err := b.Query("lesson:" + p.ID)
	if err != nil {
		t.Fatal(err)
	}
	q := live.Lessons[0]
	if !strings.Contains(q.Body, "same owner") || q.Learning == nil || q.Learning.Practice == nil || len(q.Learning.Probe.Results) != 1 || len(q.Sources) != 2 {
		t.Fatal(q)
	}
}

func TestAssignmentScopePreservesOriginalReportBoundary(t *testing.T) {
	b, _ := loopBook(t)
	a := assignmentInput("research-private", "case-1", "researcher", "research")
	a["evidence_scope"] = "plan-a"
	v, err := workCall(t, b, "prepare", map[string]any{"query": "quartz", "assignment": a})
	if err != nil {
		t.Fatal(err)
	}
	_, err = workCall(t, b, "record", map[string]any{"assignment": a, "receipt": v["receipt"], "event": map[string]any{"id": "raw-report", "state": "completed", "summary": "private-original-synthetic-value", "unknowns": []string{"private-unknown-synthetic-value"}}})
	if err != nil {
		t.Fatal(err)
	}
	a["id"], a["role"], a["evidence_scope"] = "primary-1", "primary", "primary"
	v, err = workCall(t, b, "prepare", map[string]any{"query": "quartz", "assignment": a})
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(v["teamwork"])
	if strings.Contains(string(data), "private-original") || strings.Contains(string(data), "private-unknown") {
		t.Fatal("a different scope received an original report", string(data))
	}
	if !strings.Contains(string(data), "research-private") {
		t.Fatal("omitted originals still need recovery metadata")
	}
}

func TestAssignmentReportsInProgressAndJapaneseResearch(t *testing.T) {
	b, _ := loopBook(t)
	a := assignmentInput("research-active", "first-case", "researcher", "research")
	a["purpose"] = "\u6240\u6709\u6a29\u3092\u7dad\u6301\u3059\u308b\u8abf\u67fb"
	a["question"] = "\u6240\u6709\u6a29\u306e\u78ba\u8a8d"
	if _, err := workCall(t, b, "prepare", map[string]any{"query": "\u6240\u6709\u6a29", "assignment": a}); err != nil {
		t.Fatal(err)
	}
	a["id"], a["group"], a["purpose"], a["question"] = "second-job", "another-case", "\u6240\u6709\u6a29\u306e\u5883\u754c\u3092\u8abf\u3079\u308b", "\u5927\u304d\u3044\u5165\u529b\u306e\u6240\u6709\u6a29\u3092\u8abf\u3079\u308b"
	v, err := workCall(t, b, "prepare", map[string]any{"query": "\u6240\u6709\u6a29", "assignment": a})
	if err != nil {
		t.Fatal(err)
	}
	coverage := v["teamwork"].(map[string]any)["coverage"].([]any)
	if len(coverage) != 1 || coverage[0].(map[string]any)["state"] != "active" {
		t.Fatal(coverage)
	}
}

func TestAssignmentChangedRetryCannotSmuggleANote(t *testing.T) {
	b, _ := loopBook(t)
	a := assignmentInput("job", "case", "worker", "work")
	v, err := workCall(t, b, "prepare", map[string]any{"query": "quartz", "assignment": a})
	if err != nil {
		t.Fatal(err)
	}
	req := map[string]any{"assignment": a, "receipt": v["receipt"], "event": map[string]any{"id": "result", "state": "failed", "summary": "A bounded failure."}}
	if _, err = workCall(t, b, "record", req); err != nil {
		t.Fatal(err)
	}
	req["notes"] = []any{map[string]any{"title": "An unrelated new note", "lesson": "Do not lose a changed retry.", "applies_to": "quartz", "kind": "fact", "scene": "checking", "sources": []string{"new1"}}}
	if _, err = workCall(t, b, "record", req); err == nil {
		t.Fatal("cached success silently ignored different recording content")
	}
}

func TestAssignmentRejectsUnsuppliedKnowledgeAndOversizedInput(t *testing.T) {
	b, _ := loopBook(t)
	a := assignmentInput("job", "case", "worker", "work")
	v, err := workCall(t, b, "prepare", map[string]any{"query": "quartz", "assignment": a})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = workCall(t, b, "record", map[string]any{"assignment": a, "receipt": v["receipt"], "reviewed": []string{"knowledge999"}, "event": map[string]any{"id": "result", "state": "completed", "summary": "Uninspected."}}); err == nil {
		t.Fatal("unsupplied knowledge must not be acknowledged")
	}
	a["purpose"] = strings.Repeat("x", 8001)
	a["id"] = "oversized-job"
	if _, err = workCall(t, b, "prepare", map[string]any{"query": "quartz", "assignment": a}); err == nil {
		t.Fatal("oversized context must be refused")
	}
}

func TestAssignmentExtendsTheSameReceiptWithExactKnowledge(t *testing.T) {
	b, s := loopBook(t)
	p := loopPage(t, b, s, map[string]any{"title": "Quartz exact procedure", "kind": "howto"})
	a := assignmentInput("job", "case", "worker", "work")
	v, err := workCall(t, b, "prepare", map[string]any{"query": "no-match-topic", "assignment": a})
	if err != nil {
		t.Fatal(err)
	}
	extended, err := workCall(t, b, "prepare", map[string]any{"assignment": a, "receipt": v["receipt"], "query": "lesson:" + p.ID + "@" + p.Revision})
	if err != nil {
		t.Fatal(err)
	}
	if extended["receipt"] != v["receipt"] || extended["teamwork"] == nil {
		t.Fatal(extended)
	}
	entries := extended["knowledge"].([]any)
	ref := entries[0].(map[string]any)["ref"]
	r, err := workCall(t, b, "record", map[string]any{"assignment": a, "receipt": v["receipt"], "reviewed": []any{ref}})
	if err != nil || r["complete"] != true {
		t.Fatal(r, err)
	}
}

func TestAssignmentPartialRecordingCanBeCorrectedWithoutChangingTheEvent(t *testing.T) {
	b, s := loopBook(t)
	p := loopPage(t, b, s, map[string]any{"title": "Quartz practice", "kind": "howto", "body": "Original condition."})
	a := assignmentInput("job", "case", "worker", "work")
	v, err := workCall(t, b, "prepare", map[string]any{"query": "lesson:" + p.ID, "assignment": a})
	if err != nil {
		t.Fatal(err)
	}
	edit := map[string]any{"field": "body", "before": "Absent text.", "after": "Corrected condition."}
	req := map[string]any{"assignment": a, "receipt": v["receipt"], "event": map[string]any{"id": "result", "state": "completed", "summary": "A synthetic condition was checked."}, "notes": []any{map[string]any{"target": "knowledge1", "sources": []string{"new1"}, "change_reason": "Correct the observed condition.", "edits": []any{edit}}}}
	r, err := workCall(t, b, "record", req)
	if err != nil || r["complete"] != false {
		t.Fatal(r, err)
	}
	edit["before"] = "Original condition."
	r, err = workCall(t, b, "record", req)
	if err != nil || r["complete"] != true {
		t.Fatal("a partial result must permit correction", r, err)
	}
}

func TestAssignmentPartialEventKeepsItsOriginalReceipt(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		t.Run(fmt.Sprintf("legacy-%t", legacy), func(t *testing.T) {
			b, s := loopBook(t)
			page := loopPage(t, b, s, map[string]any{"title": "Quartz receipt ownership", "kind": "howto", "body": "Original condition."})
			a := assignmentInput("job", "case", "worker", "work")
			first, err := workCall(t, b, "prepare", map[string]any{"query": "lesson:" + page.ID, "assignment": a})
			if err != nil {
				t.Fatal(err)
			}
			edit := map[string]any{"field": "body", "before": "Absent text.", "after": "Corrected condition."}
			event := map[string]any{"id": "result", "state": "completed", "summary": "A bounded condition was checked."}
			request := map[string]any{"assignment": a, "receipt": first["receipt"], "event": event, "notes": []any{map[string]any{"target": "knowledge1", "sources": []string{"new1"}, "change_reason": "Correct the observed condition.", "edits": []any{edit}}}}
			partial, err := workCall(t, b, "record", request)
			if err != nil || partial["complete"] != false {
				t.Fatal("expected an incomplete note", err)
			}
			if legacy {
				dir, err := assignmentDir(b.Root, b.Scope)
				if err != nil {
					t.Fatal(err)
				}
				r, err := readAssignment(dir, assignmentName("job"))
				if err != nil {
					t.Fatal(err)
				}
				data, _ := json.Marshal(r)
				var old map[string]any
				if err := json.Unmarshal(data, &old); err != nil {
					t.Fatal(err)
				}
				delete(old["events"].(map[string]any)["result"].(map[string]any), "receipt_hash")
				data, _ = json.Marshal(old)
				if err := json.Unmarshal(data, &r); err != nil {
					t.Fatal(err)
				}
				if err := saveAssignment(dir, r); err != nil {
					t.Fatal(err)
				}
				dir.Close()
			}
			second, err := workCall(t, b, "prepare", map[string]any{"query": "lesson:" + page.ID, "assignment": a})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := workCall(t, b, "record", map[string]any{"assignment": a, "receipt": second["receipt"], "event": event}); err == nil {
				t.Fatal("another receipt completed the first receipt's pending event")
			}
			if legacy {
				if r, err := workCall(t, b, "record", request); err != nil || r["complete"] != false {
					t.Fatal("the earlier unchanged input must recover its receipt", err)
				}
			}
			edit["before"] = "Original condition."
			r, err := workCall(t, b, "record", request)
			if err != nil || r["complete"] != true {
				t.Fatal("the original receipt must permit correction", err)
			}
			again, err := workCall(t, b, "record", request)
			if err != nil || workHash(r) != workHash(again) {
				t.Fatal("an unchanged completed retry must return its saved result", err)
			}
			live, err := b.Query("lesson:" + page.ID)
			if err != nil || live.Lessons[0].Body != "Corrected condition." || len(live.Lessons[0].Sources) != 2 {
				t.Fatal("the correction must keep its original evidence", err)
			}
		})
	}
}

func TestAssignmentRejectedInputDoesNotConsumeTheEvent(t *testing.T) {
	b, _ := loopBook(t)
	a := assignmentInput("job", "case", "worker", "work")
	v, err := workCall(t, b, "prepare", map[string]any{"query": "quartz", "assignment": a})
	if err != nil {
		t.Fatal(err)
	}
	req := map[string]any{"assignment": a, "receipt": v["receipt"], "query": "invalid replacement", "event": map[string]any{"id": "result", "state": "completed", "summary": "A bounded observation."}}
	if _, err = workCall(t, b, "record", req); err == nil {
		t.Fatal("replacement query must be refused")
	}
	delete(req, "query")
	r, err := workCall(t, b, "record", req)
	if err != nil || r["complete"] != true {
		t.Fatal("invalid input consumed the event", r, err)
	}
}

func TestAssignmentPendingCaptureBlocksAnInterleavedReceipt(t *testing.T) {
	b, _ := loopBook(t)
	a := assignmentInput("job", "case", "worker", "work")
	v, err := workCall(t, b, "prepare", map[string]any{"query": "quartz", "assignment": a})
	if err != nil {
		t.Fatal(err)
	}
	v2, err := workCall(t, b, "prepare", map[string]any{"query": "quartz", "assignment": a})
	if err != nil {
		t.Fatal(err)
	}
	old := workAfterWrite
	t.Cleanup(func() { workAfterWrite = old })
	workAfterWrite = func(kind string) error { return errors.New("synthetic checkpoint interruption") }
	req := map[string]any{"assignment": a, "receipt": v["receipt"], "event": map[string]any{"id": "observation", "state": "observed", "summary": "A bounded observation."}}
	r, err := workCall(t, b, "record", req)
	if err != nil || r["complete"] != false {
		t.Fatal(r, err)
	}
	workAfterWrite = old
	if _, err = workCall(t, b, "record", map[string]any{"assignment": a, "receipt": v2["receipt"], "event": req["event"]}); err == nil {
		t.Fatal("an interrupted event cannot move to another receipt")
	}
	if _, err = workCall(t, b, "record", map[string]any{"assignment": a, "receipt": v2["receipt"], "event": map[string]any{"id": "finish", "state": "completed", "summary": "A later result."}}); err == nil {
		t.Fatal("an interrupted observation was bypassed by another receipt")
	}
	r, err = workCall(t, b, "record", req)
	if err != nil || r["complete"] != true {
		t.Fatal(r, err)
	}
	r, err = workCall(t, b, "record", map[string]any{"assignment": a, "receipt": v2["receipt"], "event": map[string]any{"id": "finish", "state": "completed", "summary": "A later result."}})
	if err != nil || r["complete"] != true {
		t.Fatal(r, err)
	}
}

func TestAssignmentCoverageFitsItsBudgetAndKeepsRecentWork(t *testing.T) {
	b, _ := loopBook(t)
	a := assignmentInput("job", "case", "researcher", "research")
	a["conditions"], a["question"] = strings.Repeat("c", 2000), strings.Repeat("q", 1500)
	for i := 0; i < 10; i++ {
		a["id"] = fmt.Sprintf("prior-%d", i)
		if _, err := workCall(t, b, "prepare", map[string]any{"query": "quartz", "assignment": a}); err != nil {
			t.Fatal("bounded history must remain usable", err)
		}
	}
	a["id"] = "current"
	v, err := workCall(t, b, "prepare", map[string]any{"query": "quartz", "assignment": a})
	if err != nil {
		t.Fatal(err)
	}
	team := v["teamwork"].(map[string]any)
	coverage := team["coverage"].([]any)
	if len(coverage) == 0 || coverage[0].(map[string]any)["id"] != "prior-9" || team["omitted"].(float64) < 1 || workContextSize(team) > 24<<10 {
		t.Fatal("recent coverage and explicit omission must fit the budget", team)
	}
}

func assignmentInput(id, group, role, stage string) map[string]any {
	return map[string]any{"id": id, "group": group, "role": role, "stage": stage, "purpose": "Quartz invoice export", "question": "Does quartz preserve ownership?", "conditions": "synthetic local input", "commit": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "code_state": "provisional"}
}

func TestAssignmentContextAndOrdinaryWorkRemainSeparate(t *testing.T) {
	b, s := loopBook(t)
	p := loopPage(t, b, s, map[string]any{"title": "Quartz ownership", "body": "Keep the owner condition and its source."})
	ordinary, err := workCall(t, b, "prepare", map[string]any{"query": "lesson:" + p.ID, "request": "Export"})
	if err != nil || ordinary["teamwork"] != nil {
		t.Fatal(ordinary, err)
	}
	v, err := workCall(t, b, "prepare", map[string]any{"query": "lesson:" + p.ID, "assignment": assignmentInput("research-1", "case-1", "specialist", "research")})
	if err != nil {
		t.Fatal("assignment preparation must work without hooks", err)
	}
	team, ok := v["teamwork"].(map[string]any)
	if !ok || team["assignment"].(map[string]any)["role"] != "specialist" {
		t.Fatal("the role and evidence must reach external workers", v)
	}
}

func TestAssignmentCarriesInspectedChangesAcrossSessions(t *testing.T) {
	b, s := loopBook(t)
	p := loopPage(t, b, s, map[string]any{"title": "Quartz ownership", "body": "Initial ownership condition."})
	a := assignmentInput("worker-1", "case-1", "worker", "work")
	v, err := workCall(t, b, "prepare", map[string]any{"query": "lesson:" + p.ID, "assignment": a})
	if err != nil {
		t.Fatal(err)
	}
	r, err := workCall(t, b, "record", map[string]any{"assignment": a, "receipt": v["receipt"], "reviewed": []string{"knowledge1"}, "event": map[string]any{"id": "result-1", "state": "completed", "summary": "Inspected ownership."}})
	if err != nil || r["complete"] != true {
		t.Fatal(r, err)
	}
	q := loopPage(t, b, s, map[string]any{"id": p.ID, "body": "The owner must remain active."})
	a["id"] = "worker-2"
	v, err = workCall(t, b, "prepare", map[string]any{"query": "lesson:" + p.ID, "assignment": a})
	if err != nil {
		t.Fatal(err)
	}
	team, ok := v["teamwork"].(map[string]any)
	if !ok {
		t.Fatal("cross-session context is missing", v)
	}
	updates := team["changes"].(map[string]any)["updates"].([]any)
	if len(updates) != 1 || updates[0].(map[string]any)["after"].(map[string]any)["revision"] != q.Revision {
		t.Fatal(updates)
	}
}

func TestAssignmentReturnsResearchAndFailureEvidence(t *testing.T) {
	b, _ := loopBook(t)
	a := assignmentInput("research-1", "case-1", "researcher", "research")
	v, err := workCall(t, b, "prepare", map[string]any{"query": "quartz", "assignment": a})
	if err != nil {
		t.Fatal(err)
	}
	e := map[string]any{"id": "failure-1", "state": "failed", "summary": "Large quartz input failed.", "unknowns": []string{"Is the size the distinguishing condition?"}, "contradictions": []string{"Small input passed; large input failed."}, "method": "Compare the same small and large synthetic inputs."}
	r, err := workCall(t, b, "record", map[string]any{"assignment": a, "receipt": v["receipt"], "event": e})
	if err != nil || r["complete"] != true {
		t.Fatal(r, err)
	}
	a["id"], a["group"] = "research-2", "case-2"
	v, err = workCall(t, b, "prepare", map[string]any{"query": "quartz", "assignment": a})
	if err != nil {
		t.Fatal(err)
	}
	team, ok := v["teamwork"].(map[string]any)
	if !ok {
		t.Fatal("prior conditional observations are missing", v)
	}
	coverage := team["coverage"].([]any)
	if len(coverage) != 1 {
		t.Fatal("a new case must find related research", team)
	}
	prior := coverage[0].(map[string]any)
	if prior["source"] == "" || prior["conditions"] != "synthetic local input" || prior["code_state"] != "provisional" {
		t.Fatal(prior)
	}
	if len(team["questions"].([]any)) == 0 || len(team["procedure_reviews"].([]any)) == 0 {
		t.Fatal("questions and failure review must reach the next agent", team)
	}
}

func TestAssignmentRefusesChangedRetryAndForeignReceipt(t *testing.T) {
	b, _ := loopBook(t)
	a := assignmentInput("work-1", "case-1", "worker", "work")
	v, err := workCall(t, b, "prepare", map[string]any{"query": "quartz", "assignment": a})
	if err != nil {
		t.Fatal(err)
	}
	e := map[string]any{"id": "event-1", "state": "failed", "summary": "Observed a failure."}
	args := map[string]any{"assignment": a, "receipt": v["receipt"], "event": e}
	r, err := workCall(t, b, "record", args)
	if err != nil || r["complete"] != true {
		t.Fatal(r, err)
	}
	r2, err := workCall(t, b, "record", args)
	if err != nil || workHash(r) != workHash(r2) {
		t.Fatal("identical retries must recover the same evidence", r2, err)
	}
	e["summary"] = "Changed failure."
	if _, err = workCall(t, b, "record", args); err == nil {
		t.Fatal("a changed retry must be refused")
	}
	e["summary"] = "Observed a failure."
	a["id"] = "work-2"
	if _, err = workCall(t, b, "record", args); err == nil {
		t.Fatal("a receipt must be bound to its assignment")
	}
	other, _ := loopBook(t)
	a["id"] = "work-1"
	if _, err = workCall(t, other, "record", args); err == nil {
		t.Fatal("a receipt must stay in its project")
	}
}

func TestAssignmentKeepsProvisionalAndRevertedEvidence(t *testing.T) {
	b, _ := loopBook(t)
	a := assignmentInput("work-1", "case-1", "worker", "work")
	v, err := workCall(t, b, "prepare", map[string]any{"query": "quartz", "assignment": a})
	if err != nil {
		t.Fatal(err)
	}
	for _, state := range []string{"completed", "integrated", "reverted"} {
		r, e := workCall(t, b, "record", map[string]any{"assignment": a, "receipt": v["receipt"], "event": map[string]any{"id": state, "state": state, "summary": "Quartz observation " + state}})
		if e != nil || r["complete"] != true {
			t.Fatal(r, e)
		}
	}
	a["id"], a["commit"] = "work-2", strings.Repeat("b", 40)
	v, err = workCall(t, b, "prepare", map[string]any{"query": "quartz", "assignment": a})
	if err != nil {
		t.Fatal(err)
	}
	team, ok := v["teamwork"].(map[string]any)
	if !ok || len(team["coverage"].([]any)) == 0 {
		t.Fatal("reverted observations must be retained with their conditions", v)
	}
	prior := team["coverage"].([]any)[0].(map[string]any)
	if prior["state"] != "reverted" || prior["applicability"] != "recheck" {
		t.Fatal(prior)
	}
}
